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
