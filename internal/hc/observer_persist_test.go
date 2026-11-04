package hc

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Live proof for the CPU-burn defect: repeated Observe calls on the same
// pane must share ONE hc attach child — the inverted alive() respawned a
// fresh `hc attach` subprocess per read.
func TestObserveSharesOneAttach(t *testing.T) {
	if os.Getenv("HC_LIVE_SMOKE") == "" {
		t.Skip("set HC_LIVE_SMOKE=1")
	}
	attachPIDs := func() []string {
		out, _ := exec.Command("pgrep", "-f", "hc attach --observe").Output()
		var pids []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n != os.Getpid() {
				pids = append(pids, line)
			}
		}
		return pids
	}
	c := NewClient("", "hc")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := c.Observe(ctx, "t11", 206, 35, 0); err != nil {
		t.Fatalf("Observe 1: %v", err)
	}
	first := attachPIDs()
	if len(first) != 1 {
		t.Fatalf("attaches after first Observe = %v, want 1", first)
	}
	// Three more reads across ~1.5s — the watch-tick cadence — must reuse
	// the same child, not mint a new one per call.
	for i := 0; i < 3; i++ {
		time.Sleep(500 * time.Millisecond)
		if _, err := c.Observe(ctx, "t11", 206, 35, 0); err != nil {
			t.Fatalf("Observe %d: %v", i+2, err)
		}
	}
	after := attachPIDs()
	if len(after) != 1 || after[0] != first[0] {
		t.Fatalf("attaches changed across reads: %v -> %v (respawn per read)", first, after)
	}
}
