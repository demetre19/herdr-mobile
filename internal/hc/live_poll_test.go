//go:build livepoll

package hc

// Live diagnostic: run the REAL watcher against the live hc core and dump
// the agent snapshot the phone would receive.
//   go test -tags livepoll ./internal/hc -run TestLivePollSnapshot -v
import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestLivePollSnapshot(t *testing.T) {
	c := NewClient("", "hc")
	w := NewWatcher(c, "mac-mini", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go w.runEvents(ctx)
	time.Sleep(3 * time.Second) // let the connect frame land
	w.poll(ctx)
	snap := w.Current()
	t.Logf("available=%v machines=%v spaces=%d agents=%d", snap.Available, snap.Machines, len(snap.Spaces), len(snap.Agents))
	for _, a := range snap.Agents {
		t.Logf("  %-32s status=%-8s ws=%-30s tab=%-22s cwd=%s", a.TerminalID, a.Status, a.SessionName, a.TabLabel, a.Cwd)
	}
}
