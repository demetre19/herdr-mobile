package hc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Live smoke: real `hc` core, real terminal. Run explicitly:
//
//	HC_LIVE_SMOKE=1 go test ./internal/hc -run TestObserveLive -v
func TestObserveLive(t *testing.T) {
	if os.Getenv("HC_LIVE_SMOKE") == "" {
		t.Skip("set HC_LIVE_SMOKE=1 to run against the live hc core")
	}
	c := NewClient("", "hc")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	start := time.Now()
	out, err := c.Observe(ctx, "t11", 0, 0, 50)
	if err != nil {
		t.Fatalf("first Observe: %v", err)
	}
	first := time.Since(start)
	t.Logf("first paint %s, %d bytes", first, len(out))
	if len(out) == 0 {
		t.Fatal("empty frame")
	}
	// Second call: the observer is already fed — serialize must be
	// effectively instant (the old collect path cost ~8s on busy panes).
	start = time.Now()
	out2, err := c.Observe(ctx, "t11", 0, 0, 50)
	if err != nil {
		t.Fatalf("second Observe: %v", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("second Observe took %s — observer not serving from grid", d)
	}
	_ = out2
	// Output must be SGR-only.
	for i := 0; i < len(out); i++ {
		if out[i] != 0x1b {
			continue
		}
		j := i + 2
		for j < len(out) && out[j] >= '0' && out[j] <= ';' {
			j++
		}
		if j >= len(out) || out[i+1] != '[' || out[j] != 'm' {
			t.Fatalf("non-SGR escape at %d", i)
		}
		i = j
	}
	if !strings.Contains(out, "\n") {
		t.Fatal("single-line render — expected scrollback+screen")
	}
}

// A pane with a real reported height must keep that exact grid — the old
// max(rows, styledDefaultRows) padded short panes with phantom rows that
// serialized as dead space under the app's live edge.
func TestObserverEmuKeepsRealDims(t *testing.T) {
	// A 206x35 pane must stay 35 rows — the old max(rows, styledDefaultRows)
	// appended phantom blank rows the phone pinned to as dead space.
	emu := newEmu(206, 35, 50)
	if emu.cols != 206 || emu.rows != 35 {
		t.Fatalf("dims = %dx%d, want 206x35", emu.cols, emu.rows)
	}
	if len(emu.grid) != 35 {
		t.Fatalf("grid rows = %d, want 35", len(emu.grid))
	}
	// Unknown dims still fall back to the wide default.
	def := newEmu(0, 0, 50)
	if def.cols != styledDefaultCols || def.rows != styledDefaultRows {
		t.Fatalf("defaults = %dx%d, want %dx%d", def.cols, def.rows, styledDefaultCols, styledDefaultRows)
	}
}
