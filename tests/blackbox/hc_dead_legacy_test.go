package blackbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestHcInventoryFlowsWithLegacyServerDead reproduces the production state
// of 2026-11-25: the legacy herdr server socket does not exist at all
// (herdr-core 0.9.x no longer runs it), while the hc lane (real herdr-core /
// hc CLI) is fully alive. The phone must receive hc sessions.
func TestHcInventoryFlowsWithLegacyServerDead(t *testing.T) {
	if os.Getenv("HC_LIVE_SMOKE") == "" {
		t.Skip("set HC_LIVE_SMOKE=1 — uses the machine's real herdr-core")
	}
	if _, err := exec.LookPath("herdr-core"); err != nil {
		t.Skip("herdr-core not installed")
	}

	tmpDir := t.TempDir()
	relayBin := filepath.Join(tmpDir, "herdr-mobile-relay")
	build := exec.Command("go", "build", "-o", relayBin, "./cmd/herdr-mobile-relay")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build relay: %v\n%s", err, out)
	}
	port := freePort(t)
	pluginPort := freePort(t)
	webDir := filepath.Join(tmpDir, "web")
	os.MkdirAll(webDir, 0o755)
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>test</html>"), 0o644)

	// Legacy socket path points into the empty tmp dir — nothing listens.
	deadSocket := filepath.Join(tmpDir, "herdr.sock")

	cmd := exec.Command(relayBin)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HERDR_RELAY_PORT=%d", port),
		fmt.Sprintf("HERDR_RELAY_PLUGIN_PORT=%d", pluginPort),
		"HERDR_RELAY_HOST=127.0.0.1",
		"HERDR_RELAY_TOKEN=",
		"HERDR_RELAY_INSTANCE_ID=blackbox-dead-legacy",
		fmt.Sprintf("HERDR_WEB_ROOT=%s", webDir),
		"HERDR_RELAY_POLL_INTERVAL=0.5",
		fmt.Sprintf("XDG_CONFIG_HOME=%s", filepath.Join(tmpDir, "config")),
		fmt.Sprintf("XDG_CACHE_HOME=%s", filepath.Join(tmpDir, "cache")),
		fmt.Sprintf("XDG_DATA_HOME=%s", filepath.Join(tmpDir, "data")),
		fmt.Sprintf("HERDR_SOCKET_PATH=%s", deadSocket),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	defer func() {
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			cmd.Process.Kill()
		}
	}()
	waitForStatus(t, fmt.Sprintf("http://127.0.0.1:%d", port), "/health", 200)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws", port), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	var hcInv map[string]any
	var firstHcInv map[string]any
	var firstHcAt time.Time
	start := time.Now()
	var invStatus map[string]any
	var agents []any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
		_, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			continue
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg["type"] {
		case "push_config":
			if hc, ok := msg["hc"].(map[string]any); ok {
				hcInv = hc
				if firstHcInv == nil {
					firstHcInv = hc
					firstHcAt = time.Now()
				}
			}
		case "hc_inventory":
			hcInv = msg
		case "inventory_status":
			invStatus = msg
		case "agents":
			agents, _ = msg["agents"].([]any)
		}
	}

	if hcInv == nil {
		t.Fatal("no hc inventory payload received (neither push_config.hc nor hc_inventory)")
	}
	hcAgents, _ := hcInv["agents"].([]any)
	t.Logf("hc_inventory state=%v agents=%d message=%v", hcInv["state"], len(hcAgents), hcInv["message"])
	if firstHcInv != nil {
		firstAgents, _ := firstHcInv["agents"].([]any)
		t.Logf("HANDSHAKE hc payload at +%v: state=%v agents=%d (last_success_at=%v)", firstHcAt.Sub(start).Round(time.Millisecond), firstHcInv["state"], len(firstAgents), firstHcInv["last_success_at"])
	}
	if invStatus != nil {
		t.Logf("legacy inventory status: state=%v error=%v", invStatus["state"], invStatus["error_code"])
	}
	t.Logf("legacy agents message len=%d", len(agents))
	if len(hcAgents) == 0 {
		t.Error("ZERO hc agents despite live herdr-core — this is the user's empty phone")
	}
}
