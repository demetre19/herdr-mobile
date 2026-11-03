package hc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Live smoke: real `hc` core, real terminal. Run explicitly:
//   HC_LIVE_SMOKE=1 go test ./internal/hc -run TestObserveLive -v
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
