package hc

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// styled.go renders an `hc attach --observe` VT replay into the final
// screen as SGR-annotated text. The phone's ansi format is a rendered
// screen — the legacy lane's `pane read --format ansi` emits exactly that
// — while attach replays the full byte stream (cursor addressing, mode
// sets, OSC-8 links, device probes, scroll regions). Forwarded raw, every
// non-SGR sequence shows up on the phone as literal "[34;4H" garbage.
//
// The renderer is deliberately small: a cell grid, scroll margins, the
// erase/edit ops real full-screen apps use, and canonical SGR output. It
// is not a terminal emulator — it only needs to be right for replayed
// repaints, which always drive by cursor addressing after a clear.

const (
	styledDefaultCols = 220
	styledDefaultRows = 50
)

// vtAttr is the per-cell graphic state, serialized back to canonical SGR
// params so the phone sees clean "38;5;196m"-style sequences only.
type vtAttr struct {
	bold, faint, italic, underline, strike, inverse bool
	fg, bg                                          int // -1 default; 0-255 indexed; 1<<24|r<<16|g<<8|b truecolor
}

func (a vtAttr) sgr() string {
	if a == (vtAttr{fg: -1, bg: -1}) {
		return "0"
	}
	var p strings.Builder
	if a.bold {
		p.WriteString(";1")
	}
	if a.faint {
		p.WriteString(";2")
	}
	if a.italic {
		p.WriteString(";3")
	}
	if a.underline {
		p.WriteString(";4")
	}
	if a.strike {
		p.WriteString(";9")
	}
	if a.inverse {
		p.WriteString(";7")
	}
	if a.fg >= 0 {
		p.WriteString(colorParam(a.fg, 38))
	}
	if a.bg >= 0 {
		p.WriteString(colorParam(a.bg, 48))
	}
	params := p.String()
	if params == "" {
		return "0"
	}
	return "0" + params
}

func colorParam(c, base int) string {
	if c >= 1<<24 {
		v := c - (1 << 24)
		return fmt.Sprintf(";%d;2;%d;%d;%d", base, v>>16&255, v>>8&255, v&255)
	}
	if c < 8 {
		return fmt.Sprintf(";%d", base+c)
	}
	if c < 16 {
		return fmt.Sprintf(";%d", base+60+c-8)
	}
	return fmt.Sprintf(";%d;5;%d", base, c)
}

type vtCell struct {
	ch   rune // 0 = blank
	attr vtAttr
	wide bool // continuation cell of a double-width rune
}

type vtEmu struct {
	cols, rows           int
	grid                 [][]vtCell
	row, col             int
	attr                 vtAttr
	marginTop, marginBot int
	scrollback           [][]vtCell
	scrollCap            int
	wrapPend             bool
	savedRow, savedCol   int
	savedAttr            vtAttr
	savedWrap            bool
	inSync               bool
	syncBuf              []byte
	pending              []byte
}

// RenderVT replays a collected attach stream and returns the rendered
// screen (plus scrollback that moved off the top) serialized with SGR
// sequences only — the exact format the phone's ansi frames carry.
// cols/rows are the terminal's live dimensions (from `hc get`); when zero
// the renderer falls back to the DEC window-size report inside the replay,
// then to a wide default.
func RenderVT(data []byte, cols, rows, scrollbackLines int) string {
	if cols <= 0 || rows <= 0 {
		if c, r := styledReportedSize(data); c > 0 && r > 0 {
			cols, rows = c, r
		}
	}
	if cols <= 0 {
		cols = styledDefaultCols
	}
	if rows <= 0 {
		rows = styledDefaultRows
	}
	e := &vtEmu{cols: cols, rows: rows, marginBot: rows - 1, scrollCap: scrollbackLines}
	e.attr.fg, e.attr.bg = -1, -1
	e.grid = make([][]vtCell, rows)
	for i := range e.grid {
		e.grid[i] = make([]vtCell, cols)
	}
	e.feed(data)
	e.flushSync() // a one-shot capture that ends inside a bracket still renders
	return e.serialize()
}

// styledReportedSize extracts the terminal size the core reports inside
// the replay (`ESC[8;rows;cols t`, DEC window-manipulation report).
func styledReportedSize(data []byte) (cols, rows int) {
	marker := []byte("\x1b[8;")
	for i := bytes0Index(data, marker); i >= 0; {
		j := i + len(marker)
		r, c, k := 0, 0, j
		for k < len(data) && data[k] >= '0' && data[k] <= '9' {
			r = r*10 + int(data[k]-'0')
			k++
		}
		if k >= len(data) || data[k] != ';' {
			break
		}
		k++
		for k < len(data) && data[k] >= '0' && data[k] <= '9' {
			c = c*10 + int(data[k]-'0')
			k++
		}
		if k < len(data) && data[k] == 't' && r > 0 && c > 0 {
			return c, r
		}
		i = bytes0Index(data[j:], marker)
		if i >= 0 {
			i += j
		}
	}
	return 0, 0
}

func bytes0Index(data, sep []byte) int {
	for i := range len(data) - len(sep) + 1 {
		if bytes.Equal(data[i:i+len(sep)], sep) {
			return i
		}
	}
	return -1
}

func (e *vtEmu) blank() vtCell { return vtCell{attr: e.attr} }

// Synced-output (DEC ?2026) handling: full-screen apps wrap every repaint
// in CSI ?2026l … CSI ?2026h so the display never shows a torn frame. The
// relay snapshots the grid between watch ticks, so torn states are its
// ONLY problem — the phone never sees the stream. Buffer everything
// inside the bracket and apply atomically at end-sync; serialize then
// always sees a coherent repaint.
var (
	syncBegin = []byte("\x1b[?2026l")
	syncEnd   = []byte("\x1b[?2026h")
)

const syncBufferMax = 256 << 10

// feed ingests one chunk of the attach stream. Bytes arriving while a
// sync bracket is open accumulate in syncBuf; sequences split across
// chunks park in pending until the rest arrives.
func (e *vtEmu) feed(data []byte) {
	for len(data) > 0 {
		if e.inSync {
			if i := bytes0Index(data, syncEnd); i >= 0 {
				e.syncBuf = append(e.syncBuf, data[:i]...)
				data = data[i+len(syncEnd):]
				e.inSync = false
				buf := e.syncBuf
				e.syncBuf = nil
				e.feedRaw(buf)
			} else {
				e.syncBuf = append(e.syncBuf, data...)
				if len(e.syncBuf) > syncBufferMax {
					// Unbalanced sync open (or a flood inside the
					// bracket) — keeping it longer only widens the
					// tear. Apply what we have and stay out of sync.
					buf := e.syncBuf
					e.syncBuf = nil
					e.inSync = false
					e.feedRaw(buf)
				}
				return
			}
			continue
		}
		i := bytes0Index(data, syncBegin)
		if i < 0 {
			e.feedRaw(data)
			return
		}
		if i > 0 {
			e.feedRaw(data[:i])
		}
		data = data[i:]
		e.inSync = true
		e.syncBuf = append(e.syncBuf[:0], data[:0]...)
		data = data[len(syncBegin):]
	}
}

// flushSync applies any buffered sync region early — used by the one-shot
// renderer at end of capture. The observer leaves the bracket open and
// keeps buffering instead.
func (e *vtEmu) flushSync() {
	if !e.inSync || len(e.syncBuf) == 0 {
		return
	}
	buf := e.syncBuf
	e.syncBuf = nil
	e.inSync = false
	e.feedRaw(buf)
}

// feedRaw parses and applies one chunk. Incomplete sequences at the tail
// park in e.pending and prepend to the next feed — the attach channel
// slices at arbitrary byte boundaries.
func (e *vtEmu) feedRaw(data []byte) {
	if len(e.pending) > 0 {
		data = append(e.pending, data...)
		e.pending = nil
	}
	for i := 0; i < len(data); {
		b := data[i]
		switch {
		case b == 0x1b || b == 0x9b:
			next := e.escape(data, i)
			if next > len(data) {
				e.pending = append(e.pending[:0], data[i:]...)
				return
			}
			i = next
		case b < 0x20:
			i = e.control(b, i)
		case b == 0x7f:
			i++
		default:
			r, n := utf8.DecodeRune(data[i:])
			if r == utf8.RuneError && n <= 1 {
				i++
				continue
			}
			if i+n > len(data) {
				e.pending = append(e.pending[:0], data[i:]...)
				return
			}
			e.putRune(r)
			i += n
		}
	}
}

func (e *vtEmu) control(b byte, i int) int {
	switch b {
	case '\r':
		e.col = 0
	case '\n', 0x0b, 0x0c:
		e.lineFeed()
	case '\b':
		if e.col > 0 {
			e.col--
		}
	case '\t':
		e.col = min(e.cols-1, (e.col/8+1)*8)
	}
	e.wrapPend = false
	return i + 1
}

func (e *vtEmu) escape(data []byte, i int) int {
	if i+1 >= len(data) {
		return len(data) + 1 // lone ESC at chunk end — park it
	}
	switch data[i+1] {
	case '[':
		return e.csi(data, i+2)
	case ']': // OSC … BEL or ST
		for j := i + 2; j < len(data); j++ {
			if data[j] == 0x07 {
				return j + 1
			}
			if data[j] == 0x1b && j+1 < len(data) && data[j+1] == '\\' {
				return j + 2
			}
		}
		return len(data) + 1
	case 'P', 'X', '^', '_': // DCS/SOS/PM/APC … ST
		for j := i + 2; j+1 < len(data); j++ {
			if data[j] == 0x1b && data[j+1] == '\\' {
				return j + 2
			}
		}
		return len(data) + 1
	case '(', ')': // charset designation, one final byte
		if i+3 > len(data) {
			return len(data) + 1
		}
		return i + 3
	case '#', '%': // DEC test / charset family
		if i+3 > len(data) {
			return len(data) + 1
		}
		return i + 3
	case '7':
		e.savedRow, e.savedCol, e.savedAttr, e.savedWrap = e.row, e.col, e.attr, e.wrapPend
	case '8':
		e.row, e.col, e.attr, e.wrapPend = e.savedRow, e.savedCol, e.savedAttr, e.savedWrap
	case 'c':
		e.reset()
	case 'D':
		e.lineFeed()
	case 'E':
		e.col = 0
		e.lineFeed()
	case 'M':
		e.reverseIndex()
	}
	return i + 2
}

func (e *vtEmu) reset() {
	for r := range e.grid {
		for c := range e.grid[r] {
			e.grid[r][c] = vtCell{}
		}
	}
	e.row, e.col = 0, 0
	e.attr = vtAttr{fg: -1, bg: -1}
	e.marginTop, e.marginBot = 0, e.rows-1
	e.wrapPend = false
}

// csi parses params + final byte starting at i (just past "ESC[" / 0x9b).
func (e *vtEmu) csi(data []byte, i int) int {
	start := i
	priv := byte(0)
	if i < len(data) && (data[i] == '?' || data[i] == '>' || data[i] == '!' || data[i] == ' ') {
		priv = data[i]
		i++
	}
	pStart := i
	for i < len(data) && data[i] >= 0x30 && data[i] <= 0x3f {
		i++
	}
	params := string(data[pStart:i])
	for i < len(data) && data[i] >= 0x20 && data[i] <= 0x2f {
		i++
	}
	if i >= len(data) {
		// Truncated at the chunk boundary — park everything from the
		// sequence start; the rest arrives with the next chunk.
		return len(data) + 1
	}
	if data[i] < 0x40 || data[i] > 0x7e {
		// Aborted sequence — consume what we scanned.
		return max(start+1, i)
	}
	final := data[i]
	i++
	if priv == '?' || priv == '>' {
		// Mode sets, XTMODKEYS, probes — none affect the rendered grid.
		return i
	}
	nums := parseNums(params)
	at := func(idx, def int) int {
		if idx < len(nums) && nums[idx] > 0 {
			return nums[idx]
		}
		return def
	}
	switch final {
	case 'm':
		e.applySGR(nums)
	case 'H', 'f':
		e.row = clamp(at(0, 1)-1, 0, e.rows-1)
		e.col = clamp(at(1, 1)-1, 0, e.cols-1)
		e.wrapPend = false
	case 'A':
		e.row = max(e.marginTop, e.row-at(0, 1))
		e.wrapPend = false
	case 'B':
		e.row = min(e.marginBot, e.row+at(0, 1))
		e.wrapPend = false
	case 'C':
		e.col = min(e.cols-1, e.col+at(0, 1))
		e.wrapPend = false
	case 'D':
		e.col = max(0, e.col-at(0, 1))
		e.wrapPend = false
	case 'E':
		e.row = min(e.marginBot, e.row+at(0, 1))
		e.col = 0
		e.wrapPend = false
	case 'F':
		e.row = max(e.marginTop, e.row-at(0, 1))
		e.col = 0
		e.wrapPend = false
	case 'G', '`':
		e.col = clamp(at(0, 1)-1, 0, e.cols-1)
		e.wrapPend = false
	case 'd':
		e.row = clamp(at(0, 1)-1, 0, e.rows-1)
		e.wrapPend = false
	case 'J':
		e.eraseDisplay(at(0, 0))
	case 'K':
		e.eraseLine(at(0, 0))
	case 'L':
		e.insertLines(at(0, 1))
	case 'M':
		e.deleteLines(at(0, 1))
	case 'P':
		e.deleteChars(at(0, 1))
	case 'X':
		e.eraseChars(at(0, 1))
	case '@':
		e.insertChars(at(0, 1))
	case 'S':
		e.scrollUp(at(0, 1))
	case 'T':
		e.scrollDown(at(0, 1))
	case 'r':
		top, bot := at(0, 1)-1, e.rows-1
		if len(nums) > 1 {
			bot = at(1, e.rows) - 1
		}
		if top >= 0 && bot < e.rows && top < bot {
			e.marginTop, e.marginBot = top, bot
		}
		e.row, e.col = 0, 0
		e.wrapPend = false
	case 's':
		e.savedRow, e.savedCol = e.row, e.col
	case 'u':
		e.row, e.col = e.savedRow, e.savedCol
		e.wrapPend = false
	case 'Z': // back-tab
		e.col = max(0, (e.col-1)/8*8)
		e.wrapPend = false
	}
	return i
}

func parseNums(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		// ITU sub-parameters are colon-separated ("4:3" = curly
		// underline, ":none:242" = truecolor colorspace); take the
		// leading member, which is the value the emulated grid uses.
		if j := strings.IndexByte(p, ':'); j >= 0 {
			p = p[:j]
		}
		n := 0
		for _, b := range p {
			if b >= '0' && b <= '9' {
				n = n*10 + int(b-'0')
			}
		}
		out[i] = n
	}
	return out
}

func (e *vtEmu) applySGR(nums []int) {
	if len(nums) == 0 {
		e.attr = vtAttr{fg: -1, bg: -1}
		return
	}
	// Indexed loop: extColor advances i past the color operand group.
	for i := 0; i < len(nums); i++ {
		n := nums[i]
		switch {
		case n == 0:
			e.attr = vtAttr{fg: -1, bg: -1}
		case n == 1:
			e.attr.bold = true
		case n == 2:
			e.attr.faint = true
		case n == 3:
			e.attr.italic = true
		case n == 4:
			e.attr.underline = true
		case n == 7:
			e.attr.inverse = true
		case n == 9:
			e.attr.strike = true
		case n == 22:
			e.attr.bold, e.attr.faint = false, false
		case n == 23:
			e.attr.italic = false
		case n == 24:
			e.attr.underline = false
		case n == 27:
			e.attr.inverse = false
		case n == 29:
			e.attr.strike = false
		case n >= 30 && n <= 37:
			e.attr.fg = n - 30
		case n == 38:
			i = e.extColor(nums, i, true)
		case n == 39:
			e.attr.fg = -1
		case n >= 40 && n <= 47:
			e.attr.bg = n - 40
		case n == 48:
			i = e.extColor(nums, i, false)
		case n == 49:
			e.attr.bg = -1
		case n >= 90 && n <= 97:
			e.attr.fg = n - 90 + 8
		case n >= 100 && n <= 107:
			e.attr.bg = n - 100 + 8
		}
	}
}

func (e *vtEmu) extColor(nums []int, i int, fg bool) int {
	if i+1 >= len(nums) {
		return i
	}
	set := func(c int) {
		if fg {
			e.attr.fg = c
		} else {
			e.attr.bg = c
		}
	}
	switch nums[i+1] {
	case 5:
		if i+2 < len(nums) {
			set(clamp(nums[i+2], 0, 255))
			return i + 2
		}
	case 2:
		if i+4 < len(nums) {
			r, g, b := clamp(nums[i+2], 0, 255), clamp(nums[i+3], 0, 255), clamp(nums[i+4], 0, 255)
			set(1<<24 | r<<16 | g<<8 | b)
			return i + 4
		}
	}
	return i + 1
}

func (e *vtEmu) putRune(r rune) {
	if r < 0x20 && r != '\t' {
		return
	}
	if e.wrapPend {
		e.col = 0
		e.lineFeed()
	}
	w := runeWidth(r)
	if w == 0 {
		return
	}
	if e.col+w > e.cols {
		e.col = 0
		e.lineFeed()
	}
	e.grid[e.row][e.col] = vtCell{ch: r, attr: e.attr}
	if w == 2 && e.col+1 < e.cols {
		e.grid[e.row][e.col+1] = vtCell{attr: e.attr, wide: true}
	}
	if e.col+1 >= e.cols {
		e.wrapPend = true
	} else {
		e.col += w
		e.wrapPend = false
	}
}

func runeWidth(r rune) int {
	if r >= 0x1100 && (r <= 0x115f ||
		(r >= 0x2e80 && r <= 0x9fff) ||
		(r >= 0xa000 && r <= 0xa4cf) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0x1f300 && r <= 0x1faff)) {
		return 2
	}
	return 1
}

// lineFeed honors scroll margins: at the bottom margin the region scrolls
// and the vacated top row joins scrollback; inside it just moves down.
func (e *vtEmu) lineFeed() {
	e.wrapPend = false
	if e.row == e.marginBot {
		e.scrollUp(1)
		return
	}
	if e.row < e.rows-1 {
		e.row++
	}
}

func (e *vtEmu) reverseIndex() {
	if e.row == e.marginTop {
		e.scrollDown(1)
		return
	}
	if e.row > 0 {
		e.row--
	}
}

// freshRow returns a blank row sized to the grid. Row-shifting ops
// (scroll/IL/DL) move slice headers around, so a vacated slot must get a
// NEW backing array — clearing the displaced header in place would wipe
// the copy that just moved into another slot and corrupt the screen.
func (e *vtEmu) freshRow() []vtCell {
	row := make([]vtCell, e.cols)
	for c := range row {
		row[c] = e.blank()
	}
	return row
}

func (e *vtEmu) scrollUp(n int) {
	for range n {
		if e.marginTop == 0 && len(e.scrollback) < e.scrollCap {
			cp := make([]vtCell, e.cols)
			copy(cp, e.grid[e.marginTop])
			e.scrollback = append(e.scrollback, cp)
		}
		copy(e.grid[e.marginTop:e.marginBot], e.grid[e.marginTop+1:e.marginBot+1])
		e.grid[e.marginBot] = e.freshRow()
	}
}

func (e *vtEmu) scrollDown(n int) {
	for range n {
		copy(e.grid[e.marginTop+1:e.marginBot+1], e.grid[e.marginTop:e.marginBot])
		e.grid[e.marginTop] = e.freshRow()
	}
}

func (e *vtEmu) insertLines(n int) {
	if e.row < e.marginTop || e.row > e.marginBot {
		return
	}
	n = min(n, e.marginBot-e.row+1)
	copy(e.grid[e.row+n:e.marginBot+1], e.grid[e.row:e.marginBot-n+1])
	for r := e.row; r < e.row+n; r++ {
		e.grid[r] = e.freshRow()
	}
}

func (e *vtEmu) deleteLines(n int) {
	if e.row < e.marginTop || e.row > e.marginBot {
		return
	}
	n = min(n, e.marginBot-e.row+1)
	copy(e.grid[e.row:e.marginBot-n+1], e.grid[e.row+n:e.marginBot+1])
	for r := e.marginBot - n + 1; r <= e.marginBot; r++ {
		e.grid[r] = e.freshRow()
	}
}

func (e *vtEmu) deleteChars(n int) {
	n = min(n, e.cols-e.col)
	row := e.grid[e.row]
	copy(row[e.col:], row[e.col+n:])
	for c := e.cols - n; c < e.cols; c++ {
		row[c] = e.blank()
	}
}

func (e *vtEmu) insertChars(n int) {
	n = min(n, e.cols-e.col)
	row := e.grid[e.row]
	copy(row[e.col+n:], row[e.col:])
	for c := e.col; c < e.col+n; c++ {
		row[c] = e.blank()
	}
}

func (e *vtEmu) eraseChars(n int) {
	n = min(n, e.cols-e.col)
	row := e.grid[e.row]
	for c := e.col; c < e.col+n; c++ {
		row[c] = e.blank()
	}
}

func (e *vtEmu) eraseDisplay(mode int) {
	switch mode {
	case 0:
		for c := e.col; c < e.cols; c++ {
			e.grid[e.row][c] = e.blank()
		}
		for r := e.row + 1; r < e.rows; r++ {
			for c := range e.grid[r] {
				e.grid[r][c] = e.blank()
			}
		}
	case 1:
		for r := 0; r < e.row; r++ {
			for c := range e.grid[r] {
				e.grid[r][c] = e.blank()
			}
		}
		for c := 0; c <= e.col; c++ {
			e.grid[e.row][c] = e.blank()
		}
	case 2:
		for r := range e.grid {
			for c := range e.grid[r] {
				e.grid[r][c] = e.blank()
			}
		}
	default: // 3 — xterm clears the scrollback too
		for r := range e.grid {
			for c := range e.grid[r] {
				e.grid[r][c] = e.blank()
			}
		}
		e.scrollback = nil
	}
}

func (e *vtEmu) eraseLine(mode int) {
	row := e.grid[e.row]
	switch mode {
	case 0:
		for c := e.col; c < e.cols; c++ {
			row[c] = e.blank()
		}
	case 1:
		for c := 0; c <= e.col; c++ {
			row[c] = e.blank()
		}
	default:
		for c := range row {
			row[c] = e.blank()
		}
	}
}

// serialize emits every non-empty row (scrollback first) with canonical
// SGR transitions — the only escape family the phone parser understands.
func (e *vtEmu) serialize() string {
	var out strings.Builder
	var cur vtAttr
	cur.fg, cur.bg = -1, -1
	curSet := false
	writeRow := func(row []vtCell) {
		last := len(row)
		for last > 0 && (row[last-1].ch == 0 || row[last-1].ch == ' ') && !row[last-1].wide {
			last--
		}
		for c := range last {
			cell := row[c]
			if !curSet || cell.attr != cur {
				cur = cell.attr
				curSet = true
				out.WriteString("\x1b[")
				out.WriteString(cur.sgr())
				out.WriteByte('m')
			}
			if cell.wide || cell.ch == 0 {
				out.WriteByte(' ')
			} else {
				out.WriteRune(cell.ch)
			}
		}
	}
	lines := 0
	total := len(e.scrollback) + e.rows
	emit := func(row []vtCell) {
		lines++
		if lines > 1 {
			out.WriteByte('\n')
		}
		writeRow(row)
	}
	// Serialize scrollback + screen, skipping leading blank rows only —
	// the phone splices rows by content, so stable row order matters.
	all := make([][]vtCell, 0, total)
	all = append(all, e.scrollback...)
	all = append(all, e.grid...)
	start := 0
	for start < len(all) && rowEmpty(all[start]) {
		start++
	}
	for r := start; r < len(all); r++ {
		emit(all[r])
	}
	return out.String()
}

func rowEmpty(row []vtCell) bool {
	for _, c := range row {
		if c.ch != 0 && c.ch != ' ' && !c.wide {
			return false
		}
	}
	return true
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
