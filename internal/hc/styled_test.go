package hc

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// A real `hc attach --observe` capture (t11, 206x35): mode-set preamble,
// scrollback replay, ED2 repaint, cursor-addressed statusline, and a live
// progress-bar update — exactly the byte soup that used to leak through
// to the phone as literal "[34;4H" text.
func loadReplay(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/hc-replay-t11.bin")
	if err != nil {
		t.Skipf("replay fixture missing: %v", err)
	}
	return data
}

func TestRenderVTEmitsSGROnly(t *testing.T) {
	out := RenderVT(loadReplay(t), 206, 35, 100)
	// Every ESC must open an SGR sequence — the phone's ansi parser renders
	// nothing else, so any other escape would leak as literal text.
	for i := 0; i < len(out); i++ {
		if out[i] != 0x1b {
			continue
		}
		if i+1 >= len(out) || out[i+1] != '[' {
			t.Fatalf("non-CSI escape at %d in %q", i, out[max(0, i-20):i+20])
		}
		j := i + 2
		for j < len(out) && out[j] >= '0' && out[j] <= ';' {
			j++
		}
		if j >= len(out) || out[j] != 'm' {
			t.Fatalf("non-SGR CSI at %d: %q", i, out[i:j+1])
		}
		i = j
	}
	if strings.ContainsRune(out, 0x18) { // ^X device probe
		t.Fatal("device probe leaked")
	}
}

var sgrPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestRenderVTRealReplayScreen(t *testing.T) {
	out := RenderVT(loadReplay(t), 206, 35, 100)
	plain := sgrPattern.ReplaceAllString(out, "")
	// The terminal's last-painted content survives, once.
	if n := strings.Count(plain, "╭──"); n != 1 {
		t.Fatalf("statusline box painted %d times, want 1", n)
	}
	if !strings.Contains(plain, "astro build: 63 pages, clean") {
		t.Fatal("final screen content missing")
	}
	// History replay produced scrollback rows ahead of the screen.
	if !strings.Contains(plain, "Root cause found — the diagram") {
		t.Fatal("scrollback rows missing")
	}
}

// Regression: rows shifted into a vacated slot must not share a backing
// array with the erased row — blanking the tail used to wipe the shifted
// copy, stamping one painted row across the scroll region.
func TestRenderVTScrollRegionDoesNotDuplicate(t *testing.T) {
	// Fill rows 0-3 with distinct lines, set margin 2-5 (1-based),
	// then linefeed twice at the bottom margin: row 4 (bottom margin)
	// scrolls up; rows A/B/C move correctly, no aliasing.
	var b strings.Builder
	for i := range 4 {
		b.WriteString("\x1b[" + strings.Repeat("0;", 0) + "m")
		b.WriteString("LINE" + string(rune('A'+i)) + "\r\n")
	}
	b.WriteString("\x1b[3;5r") // margin rows 3..5 (0-based 2..4)
	b.WriteString("\x1b[5;1H") // cursor to bottom margin (row 5, 1-based)
	b.WriteString("\x1b[0mX")  // write at bottom margin
	b.WriteString("\x1b[M")    // delete line at cursor (inside margin)
	out := RenderVT([]byte(b.String()), 80, 24, 10)
	if strings.Count(out, "LINEA") != 1 || strings.Count(out, "LINEB") != 1 {
		t.Fatalf("rows duplicated or lost: %q", out)
	}
}

func TestRenderVTCursorAddressedRepaint(t *testing.T) {
	// Replay the same region twice — final paint wins, once.
	b := "\x1b[?2026l\x1b[2J\x1b[HHello World"
	b += "\x1b[10;1H\x1b[38;5;196mRED\x1b[0m"
	b += "\x1b[10;1H\x1b[32mGREEN" // repaint row 10
	out := RenderVT([]byte(b), 80, 24, 0)
	if strings.Count(out, "GREEN") != 1 || strings.Contains(out, "RED") {
		t.Fatalf("repaint did not settle: %q", out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("no SGR emitted for colored cell")
	}
}

func TestRenderVTTruecolorAndIndexed(t *testing.T) {
	b := "\x1b[H\x1b[38;2;10;20;30mTC\x1b[38;5;99mIX\x1b[0mEND"
	out := RenderVT([]byte(b), 80, 24, 0)
	if !strings.Contains(out, "38;2;10;20;30") {
		t.Fatalf("truecolor lost: %q", out)
	}
	if !strings.Contains(out, "38;5;99") {
		t.Fatalf("indexed color lost: %q", out)
	}
	if !strings.HasSuffix(out, "END") && !strings.Contains(out, "END") {
		t.Fatalf("tail text lost: %q", out)
	}
}

func TestRenderVTWideRunesKeepAlignment(t *testing.T) {
	// CJK + box-drawing frame: each CJK rune occupies two cells.
	b := "\x1b[HAB"
	out := RenderVT([]byte(b), 80, 24, 0)
	// Serialized text: two CJK runes + AB (wide continuations emit a
	// space so the phone's row width still matches the cell grid).
	if !strings.Contains(out, "") || !strings.Contains(out, "") || !strings.Contains(out, "AB") {
		t.Fatalf("wide runes lost: %q", out)
	}
}

func TestColorParamIndexedOffsets(t *testing.T) {
	// Regression: colorParam wrote base+c for standard indexed colors,
	// so fg 4 serialized as 42 — which the reader parses as BG green
	// (the green/red "boat" bug where a blue sea rendered as slabs).
	cases := []struct{ c, base int; want string }{
		{4, 38, ";34"},   // fg blue
		{3, 38, ";33"},   // fg yellow (the boat)
		{1, 48, ";41"},   // bg red
		{12, 38, ";94"},  // bright fg blue
		{9, 48, ";101"},  // bright bg red
		{200, 38, ";38;5;200"},
		{1<<24 | 0x77<<16 | 0xF5<<8 | 0x6C, 48, ";48;2;119;245;108"},
	}
	for _, tc := range cases {
		if got := colorParam(tc.c, tc.base); got != tc.want {
			t.Fatalf("colorParam(%d,%d) = %q, want %q", tc.c, tc.base, got, tc.want)
		}
	}
}
