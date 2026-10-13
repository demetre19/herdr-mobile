package blackbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// bindLocalhostPort grabs a listener on a specific port inside the default
// 3000-3010 dev-server range (the phone's default setting).
func bindLocalhostPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	for port := 3000; port <= 3010; port++ {
		ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue // occupied by something else; try the next port
		}
		return ln, port
	}
	t.Skip("every port 3000-3010 is already in use")
	return nil, 0
}


func TestLocalHTTPFetch(t *testing.T) {
	ln, port := bindLocalhostPort(t)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"hello":"localhost"}`)
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	env := setupEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, env.wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	command := map[string]any{
		"type":       "command",
		"action":     "local_http_fetch",
		"request_id": "localhost-req-1",
		"protocol":   3,
		"port":       port,
		"path":       "/api/status",
	}
	data, _ := json.Marshal(command)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	msg := readJSON(t, conn, ctx, 5*time.Second)
	if msg["type"] != "command_result" {
		t.Fatalf("expected command_result, got %v", msg["type"])
	}
	if msg["ok"] != true {
		t.Fatalf("ok = %v, error = %v", msg["ok"], msg["error"])
	}
	result, _ := msg["data"].(map[string]any)
	if result == nil {
		t.Fatal("missing data payload")
	}
	if result["status"].(float64) != http.StatusOK {
		t.Errorf("status = %v", result["status"])
	}
	if result["content_type"] != "application/json" {
		t.Errorf("content_type = %v", result["content_type"])
	}
	decoded, decodeErr := base64.StdEncoding.DecodeString(result["body"].(string))
	if decodeErr != nil {
		t.Fatalf("body decode: %v", decodeErr)
	}
	if string(decoded) != `{"hello":"localhost"}` {
		t.Errorf("body = %q", string(decoded))
	}
}

func TestLocalHTTPFetchRejectsOutOfRangePort(t *testing.T) {
	env := setupEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, env.wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	command := map[string]any{
		"type":       "command",
		"action":     "local_http_fetch",
		"request_id": "localhost-req-2",
		"protocol":   3,
		"port":       0,
		"path":       "/",
	}
	data, _ := json.Marshal(command)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	msg := readJSON(t, conn, ctx, 5*time.Second)
	if msg["type"] != "command_result" {
		t.Fatalf("expected command_result, got %v", msg["type"])
	}
	if msg["ok"] != false {
		t.Fatal("out-of-range port must be refused")
	}
	if errorText, _ := msg["error"].(string); errorText != "Port must be between 1 and 65535" {
		t.Errorf("error = %q", errorText)
	}}
