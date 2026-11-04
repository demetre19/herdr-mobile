package hc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Live check: a 35-row pane must serialize AT MOST its 35 grid rows —
// the old max(rows, styledDefaultRows) padded the grid to 50 and the
// phone pinned to 15 rows of dead space under the live edge.
// Run explicitly: HC_LIVE_SMOKE=1 go test ./internal/hc -run TestObserveShortPaneNoPadding -v
func TestObserveShortPaneNoPadding(t *testing.T) {
	if os.Getenv("HC_LIVE_SMOKE") == "" {
		t.Skip("set HC_LIVE_SMOKE=1 to run against the live hc core")
	}
	c := NewClient("", "hc")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := c.Observe(ctx, "t11", 206, 35, 0)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	rows := strings.Split(out, "\n")
	if len(rows) > 35 {
		t.Fatalf("serialized %d rows for a 35-row pane — grid inflated", len(rows))
	}
}

// LRU eviction: creating a fourth live observer kills the least-recently
// touched one so watched-pane count can never exceed the cap — each live
// `hc attach` can burn a full core on a busy pane.
func TestObserverCapEvictsOldest(t *testing.T) {
	c := NewClient("", "hc")
	touch := time.Now().Add(-time.Hour)
	for _, addr := range []string{"t1", "t2", "t3"} {
		ob := c.observerFor(addr, 0, 0, 0)
		ob.mu.Lock()
		ob.lastTouch = touch
		ob.mu.Unlock()
		touch = touch.Add(time.Minute)
	}
	c.observerFor("t4", 0, 0, 0)
	c.observersMu.Lock()
	live := []string{}
	for addr, ob := range c.observers {
		if ob.alive() {
			live = append(live, addr)
		}
	}
	c.observersMu.Unlock()
	if len(live) != observerMaxConcurrent {
		t.Fatalf("live observers = %v, want %d", live, observerMaxConcurrent)
	}
	for _, addr := range live {
		if addr == "t1" {
			t.Fatalf("t1 (oldest) survived eviction: %v", live)
		}
	}
}
