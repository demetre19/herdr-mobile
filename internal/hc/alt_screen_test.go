package hc

import (
	"strings"
	"testing"
)

// Alt-screen transitions must save/restore the main grid: after a TUI
// exits the alternate screen, the main screen's content comes back, and
// the alt frame is no longer painted over it.
func TestAltScreenExitRestoresMainGrid(t *testing.T) {
	var b strings.Builder
	b.WriteString("MAIN-1\r\nMAIN-2\r\nMAIN-3\r\n")
	b.WriteString("\x1b[?1049h\x1b[H\x1b[2J")
	for i := 0; i < 5; i++ {
		b.WriteString("TUI-LINE\r\n")
	}
	b.WriteString("\x1b[?1049l") // exit alt — main screen restores
	out := RenderVT([]byte(b.String()), 80, 10, 100)
	plain := sgrPattern.ReplaceAllString(out, "")
	if n := strings.Count(plain, "MAIN-"); n != 3 {
		t.Fatalf("main screen not restored on alt exit: %d MAIN rows in %q", n, plain)
	}
}

// While in the alt screen, the live alt grid is what serializes (the TUI's
// current frame), appended after the shared scrollback.
func TestAltScreenShowsLiveAltGrid(t *testing.T) {
	var b strings.Builder
	b.WriteString("HISTORY-1\r\n")
	b.WriteString("\x1b[?1049h\x1b[H\x1b[2J")
	for i := 0; i < 5; i++ {
		b.WriteString("TUI-LINE\r\n")
	}
	b.WriteString("\x1b[9;1HSTATUS-BAR") // draw a bottom status row
	out := RenderVT([]byte(b.String()), 80, 10, 100)
	plain := sgrPattern.ReplaceAllString(out, "")
	if n := strings.Count(plain, "STATUS-BAR"); n != 1 {
		t.Fatalf("live alt grid status missing: %d in %q", n, plain)
	}
}
