package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestValidateLocalhostPath(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: "/"},
		{in: "/", want: "/"},
		{in: "/api/status", want: "/api/status"},
		{in: "/search?q=1", want: "/search?q=1"},
		{in: "api/status", wantErr: true},
		{in: "/a b", wantErr: true},
		{in: "/redirect?to=http://x", wantErr: true}, // contains "://"
		{in: "/user@host", wantErr: true},
	}
	for _, tc := range cases {
		got, err := validateLocalhostPath(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("validateLocalhostPath(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("validateLocalhostPath(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("validateLocalhostPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateLocalhostPort(t *testing.T) {
	if err := validateLocalhostPort(0); err == nil {
		t.Error("port 0 should fail")
	}
	if err := validateLocalhostPort(3000); err != nil {
		t.Errorf("port 3000 should pass, got %v", err)
	}
	if err := validateLocalhostPort(8080); err != nil {
		t.Errorf("port 8080 should pass, got %v", err)
	}
	if err := validateLocalhostPort(65535); err != nil {
		t.Errorf("port 65535 should pass, got %v", err)
	}
	if err := validateLocalhostPort(65536); err == nil {
		t.Error("port 65536 should fail")
	}
	if err := validateLocalhostPort(-1); err == nil {
		t.Error("negative port should fail")
	}
}

// The dial Control hook is the loopback guarantee: a dial to any non-loopback
// address must be refused even if a URL were constructed wrong.
func TestLocalhostDialControlRejectsNonLoopback(t *testing.T) {
	transport, ok := localhostHTTPClient.Transport.(*http.Transport)
	if !ok || transport.DialContext == nil {
		t.Fatal("localhostHTTPClient must carry a DialContext transport")
	}
	dialer := transport.DialContext

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	if _, err := dialer(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", port)); err != nil {
		t.Fatalf("loopback dial should pass, got %v", err)
	}

	// A LAN-looking address (nothing listening needed; Control fires first).
	if _, err := dialer(context.Background(), "tcp", net.JoinHostPort("192.168.1.1", port)); err == nil {
		t.Fatal("non-loopback dial should be rejected by Control")
	}
}

func TestLocalhostFetch(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port := atoiTest(portStr)

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	// Point the fetch core at the real listener; production always builds
	// the literal 127.0.0.1 URL (covered by the Control-hook test above).
	previousBaseURL := localhostBaseURL
	t.Cleanup(func() { localhostBaseURL = previousBaseURL })
	localhostBaseURL = func(int, string) string {
		return "http://127.0.0.1:" + portStr
	}

	resp, err := localhostFetch(context.Background(), port, "/api/status")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if resp.Status != http.StatusOK || resp.ContentType != "application/json" {
		t.Errorf("status=%d content_type=%q", resp.Status, resp.ContentType)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Errorf("body = %q", string(resp.Body))
	}
	if resp.Truncated {
		t.Error("small body must not be truncated")
	}
}

func TestLocalhostFetchClosedPortBounded(t *testing.T) {
	closed, listenErr := net.Listen("tcp4", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatalf("listen: %v", listenErr)
	}
	_, portStr, _ := net.SplitHostPort(closed.Addr().String())
	closedPort := atoiTest(portStr)
	_ = closed.Close() // guaranteed no listener

	previousBaseURL := localhostBaseURL
	t.Cleanup(func() { localhostBaseURL = previousBaseURL })
	localhostBaseURL = func(int, string) string {
		return "http://127.0.0.1:" + portStr
	}

	start := time.Now()
	if _, err := localhostFetch(context.Background(), closedPort, "/"); err == nil {
		t.Fatal("expected failure for closed port")
	}
	if elapsed := time.Since(start); elapsed > localhostFetchTimeout+time.Second {
		t.Errorf("failure took %v", elapsed)
	}
}

func atoiTest(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}
