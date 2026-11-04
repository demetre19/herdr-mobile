package hc

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// observer.go — persistent `hc attach --observe` sessions.
//
// A collect-per-poll attach is wrong for live terminals: the replay
// finishes, then the attach keeps streaming forever, so quiet-detection
// never fires on a busy pane and every read burns the outer deadline.
// The stream IS the primitive the desktop app uses — hold ONE observer
// attach per terminal, feed its bytes into the VT emulator continuously,
// and serialize the current grid whenever a pane read/watch lands. Reads
// become instant and the feed is genuinely live.
//
// Remote `machine/addr` terminals are excluded by the caller: federated
// attach pays seconds per frame and the plain read stays the fast path.

const (
	// observerIdleTTL bounds an attach's lifetime after its last read —
	// `hc attach` costs a core while held, so an unattended observer must
	// die fast (the user's ask: ~20–30s). Watched panes touch the observer
	// every frame, so the TTL only fires after the last watcher leaves.
	observerIdleTTL    = 30 * time.Second
	observerRespawnMin = 5 * time.Second
	observerReadChunk  = 32 << 10
	// observerQuiet marks a coherent frame: repaint bursts (bracketed or
	// not) land as back-to-back chunks, so a feed silence this long means
	// the app finished repainting. Snapshots taken mid-burst serialize a
	// torn grid — half old half new — which the phone renders as a jump.
	observerQuiet = 40 * time.Millisecond
	// observerQuietMax bounds how long a read waits for quiet before
	// serving the last coherent frame anyway (or a torn one on the very
	// first paint).
	observerQuietMax = 150 * time.Millisecond
	// observerMaxConcurrent bounds live attaches: `hc attach` on a busy
	// pane costs ~1 core while held, so N watched panes must never burn N
	// cores. Evicting the least-recently-touched observer when a new pane
	// is requested keeps worst-case cost at observerMaxConcurrent cores —
	// the evicted pane's next read respawns on demand.
	observerMaxConcurrent = 3
)

// evictOverflowObservers kills the oldest live observers until at most
// observerMaxConcurrent remain (excluding `keep`, the pane about to be
// served). Callers hold observersMu; lastTouch marks recency.
func (c *Client) evictOverflowObservers(keep string) {
	live := 0
	for addr, ob := range c.observers {
		if addr != keep && ob.alive() {
			live++
		}
	}
	for live >= observerMaxConcurrent {
		var oldestAddr string
		var oldest time.Time
		for addr, ob := range c.observers {
			if addr == keep || !ob.alive() {
				continue
			}
			if oldest.IsZero() || ob.lastTouch.Before(oldest) {
				oldest, oldestAddr = ob.lastTouch, addr
			}
		}
		if oldestAddr == "" {
			return
		}
		c.observers[oldestAddr].kill()
		delete(c.observers, oldestAddr)
		live--
	}
}

// Close kills every live observer attach — server shutdown calls this so
// no `hc attach` child outlives the relay burning a core.
func (c *Client) Close() {
	c.observersMu.Lock()
	defer c.observersMu.Unlock()
	for addr, ob := range c.observers {
		ob.kill()
		delete(c.observers, addr)
	}
}

// AttachObserver owns one `hc attach --observe` child plus the emulator
// it feeds.
type AttachObserver struct {
	addr       string
	cols, rows int
	scrollCap  int

	mu         sync.Mutex
	emu        *vtEmu
	lastSpawn  time.Time
	started    bool
	fed        chan struct{} // closed on first bytes
	spawnErr   error
	cancel     context.CancelFunc
	lastTouch  time.Time
	done       chan struct{}
	lastFeed   time.Time // last pump chunk fed into emu
	lastSnapAt time.Time // when snap was serialized
	snap       string    // coherent cached frame (serialized post-quiet)
}

func (c *Client) observerFor(addr string, cols, rows, scrollCap int) *AttachObserver {
	c.observersMu.Lock()
	defer c.observersMu.Unlock()
	if c.observers == nil {
		c.observers = make(map[string]*AttachObserver)
	}
	ob := c.observers[addr]
	if ob != nil && (!ob.alive() || !ob.dimsMatch(cols, rows)) {
		ob.kill()
		delete(c.observers, addr)
		ob = nil
	}
	if ob == nil {
		// A new live attach costs up to a core on a busy pane — evict the
		// least-recently-served observer first so watched-pane count can
		// never exceed observerMaxConcurrent.
		c.evictOverflowObservers(addr)
		ob = &AttachObserver{addr: addr, cols: cols, rows: rows,
			scrollCap: scrollCap, lastTouch: time.Now()}
		c.observers[addr] = ob
	}
	if scrollCap > ob.scrollCap {
		ob.scrollCap = scrollCap
		ob.mu.Lock()
		if ob.emu != nil {
			ob.emu.scrollCap = scrollCap
		}
		ob.mu.Unlock()
	}
	return ob
}

func (o *AttachObserver) dimsMatch(cols, rows int) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return (cols <= 0 || o.cols == cols) && (rows <= 0 || o.rows == rows)
}

// dead reports whether the attach child has exited: `done` is created at
// spawn and CLOSED by pump/markDone when the stream ends, so a closed
// channel means dead — non-nil alone only means the child was spawned.
// Misreading non-nil as dead respawned the attach on every Observe tick.
func (o *AttachObserver) dead() bool {
	select {
	case <-o.done:
		return true
	default:
		return false
	}
}

func (o *AttachObserver) alive() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return !o.dead()
}

// Observe returns the terminal's current screen rendered as SGR-annotated
// text — the same contract as `pane read --format ansi`. It spawns (or
// lazily respawns) the observer attach, waits for the first replay paint
// within ctx, then snapshots. Subsequent calls serialize instantly.
func (c *Client) Observe(ctx context.Context, addr string, cols, rows, scrollCap int) (string, error) {
	if scrollCap < 0 {
		scrollCap = 0
	}
	ob := c.observerFor(addr, cols, rows, scrollCap)
	ob.touch()

	needSpawn := false
	ob.mu.Lock()
	if !ob.started {
		ob.started = true
		needSpawn = true
	} else if ob.dead() && time.Since(ob.lastSpawn) >= observerRespawnMin {
		needSpawn = true
	}
	ob.mu.Unlock()
	if needSpawn {
		ob.spawn(c)
	}

	// First paint: wait for the replay's first bytes (bounded by ctx);
	// a core that rejects the attach surfaces via spawnErr instead.
	ob.mu.Lock()
	fed, serr, done := ob.fed, ob.spawnErr, ob.done
	ob.mu.Unlock()
	if serr != nil {
		return "", serr
	}
	if fed != nil {
		select {
		case <-fed:
		case <-done:
			// done racing a just-fed repaint is not an error — serve
			// the painted grid; only a paint-less death fails.
			select {
			case <-fed:
			default:
				return "", fmt.Errorf("hc attach %s: ended before paint", addr)
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	// A child that died after painting leaves a stale grid — the
	// respawn guard above keeps hot respawns bounded; here the caller
	// is better off with the plain-read fallback than frozen output,
	// so report the dead child instead of serving it.
	ob.mu.Lock()
	dead := ob.dead()
	ob.mu.Unlock()
	if dead && !needSpawn {
		return "", fmt.Errorf("hc attach %s: stream ended", addr)
	}
	return ob.snapshot(), nil
}

func (o *AttachObserver) touch() {
	o.mu.Lock()
	o.lastTouch = time.Now()
	o.mu.Unlock()
}

// snapshot returns the terminal's coherent frame. While the attach stream
// is mid-burst it waits for the repaint to settle (observerQuiet of feed
// silence), then serializes once and caches — reads between quiet windows
// serve the cached coherent frame instead of a torn mid-repaint grid.
func (o *AttachObserver) snapshot() string {
	deadline := time.Now().Add(observerQuietMax)
	for {
		o.mu.Lock()
		quiet := o.emu != nil && time.Since(o.lastFeed) >= observerQuiet
		if quiet && o.snap != "" && !o.lastSnapAt.Before(o.lastFeed) {
			s := o.snap
			o.mu.Unlock()
			return s
		}
		if quiet {
			// Quiet = repaint done: drain any held sync region so the
			// serialized frame includes it — an app that opens ?2026 and
			// never closes must not starve the grid to blank.
			o.emu.flushSync()
			s := o.emu.serialize()
			o.snap, o.lastSnapAt = s, time.Now()
			o.mu.Unlock()
			return s
		}
		// Not quiet yet. If we already have a coherent frame and the wait
		// budget is spent, serve it rather than block the read.
		if o.snap != "" && time.Now().After(deadline) {
			s := o.snap
			o.mu.Unlock()
			return s
		}
		o.mu.Unlock()
		if time.Now().After(deadline) {
			// First paint still mid-burst and no cache: drain any held
			// sync region and serialize whatever exists — a torn frame
			// beats an empty one; the next quiet window restores
			// coherence.
			o.mu.Lock()
			o.emu.flushSync()
			s := o.emu.serialize()
			o.mu.Unlock()
			return s
		}
		time.Sleep(8 * time.Millisecond)
	}
}

func (o *AttachObserver) kill() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cancel != nil {
		o.cancel()
	}
}

func (o *AttachObserver) markDone() {
	o.mu.Lock()
	if o.done != nil {
		select {
		case <-o.done:
		default:
			close(o.done)
		}
	}
	o.mu.Unlock()
}

// newEmu builds the observer's emulator at the pane's real dims.
// Defaults fill UNKNOWN dims only — never raise a real smaller pane:
// an inflated grid pads the serialized frame with blank rows under the
// app's live edge and the phone's bottom pin lands on dead space (the
// "screen starts high" bug on short panes).
func newEmu(cols, rows, scrollCap int) *vtEmu {
	if cols <= 0 {
		cols = styledDefaultCols
	}
	if rows <= 0 {
		rows = styledDefaultRows
	}
	emu := &vtEmu{cols: cols, rows: rows}
	emu.marginBot = emu.rows - 1
	emu.attr.fg, emu.attr.bg = -1, -1
	emu.grid = make([][]vtCell, emu.rows)
	for i := range emu.grid {
		emu.grid[i] = make([]vtCell, emu.cols)
	}
	emu.scrollCap = scrollCap
	return emu
}

// spawn launches `hc attach --observe` and feeds its byte stream into a
// fresh emulator. The child's own context is killed on server shutdown
// or after observerIdleTTL without a snapshot.
func (o *AttachObserver) spawn(c *Client) {
	o.mu.Lock()
	cols, rows, cap := o.cols, o.rows, o.scrollCap
	emu := newEmu(cols, rows, cap)
	o.emu = emu
	o.fed = make(chan struct{})
	o.done = nil
	o.spawnErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	o.lastSpawn = time.Now()
	o.done = make(chan struct{})
	o.mu.Unlock()

	cmd := exec.CommandContext(ctx, c.CoreBin, "attach", "--observe",
		"--history", fmt.Sprintf("%d", cap), o.addr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		o.finish(cancel, err)
		return
	}
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		o.finish(cancel, err)
		return
	}
	go o.pump(cmd, stdout, emu)
	go o.reap(cancel)
}

func (o *AttachObserver) finish(cancel context.CancelFunc, err error) {
	o.mu.Lock()
	o.spawnErr = err
	if o.fed != nil {
		select {
		case <-o.fed:
		default:
			close(o.fed)
		}
	}
	o.mu.Unlock()
	o.markDone()
	cancel()
}

// pump streams attach output into the emulator until the child dies or
// is cancelled, then marks the observer for lazy respawn.
func (o *AttachObserver) pump(cmd *exec.Cmd, stdout io.Reader, emu *vtEmu) {
	defer o.markDone()
	buf := make([]byte, observerReadChunk)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			o.mu.Lock()
			// A mid-stream size report (RESIZE painted into the channel)
			// rebuilds the grid at the new geometry — scrollback starts
			// over, which beats rendering at stale dims.
			if c2, r2 := styledReportedSize(chunk); c2 > 0 && r2 > 0 &&
				(c2 != emu.cols || r2 != emu.rows) {
				emu.cols, emu.rows = c2, r2
				emu.marginTop, emu.marginBot = 0, r2-1
				emu.grid = make([][]vtCell, r2)
				for i := range emu.grid {
					emu.grid[i] = make([]vtCell, c2)
				}
				emu.row, emu.col = 0, 0
				emu.scrollback = nil
			}
			emu.feed(chunk)
			if o.fed != nil {
				select {
				case <-o.fed:
				default:
					close(o.fed)
				}
			}
			o.lastFeed = time.Now()
			o.mu.Unlock()
		}
		if err != nil {
			_ = cmd.Wait()
			return
		}
	}
}

// reap kills the attach after it sits unsnapshotted past the TTL — the
// phone navigated away; the next Observe respawns on demand.
func (o *AttachObserver) reap(cancel context.CancelFunc) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-o.done:
			return
		case <-ticker.C:
			o.mu.Lock()
			idle := time.Since(o.lastTouch) > observerIdleTTL
			dead := o.dead()
			o.mu.Unlock()
			if dead || idle {
				// dead: the child already exited — just retire.
				// idle past TTL: cancel the attach so an unattended
				// observer can't burn a core forever.
				cancel()
				return
			}
		}
	}
}
