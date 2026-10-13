package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/transport"
)

// Localhost browsing lets the paired phone render web UIs served on this
// machine's loopback interface. The surface is deliberately narrow:
//
//   - host is always the literal 127.0.0.1 (no DNS, no LAN, no internet)
//   - ports are restricted to a small dev-server range
//   - GET only, bounded body size, bounded duration
//
// Widening any of these turns a paired phone credential into an SSRF pivot,
// so keep the restrictions unless a real need shows up.
const (
	localhostMinPort          = 3000
	localhostMaxPort          = 3010
	localhostFetchTimeout     = 10 * time.Second
	localhostMaxResponseBytes = 2 << 20 // 2 MiB
)

var localhostHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: func(_ string, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return fmt.Errorf("localhost fetch dial rejected %q: %w", address, err)
				}
				ip := net.ParseIP(host)
				if ip == nil || !ip.IsLoopback() {
					return fmt.Errorf("localhost fetch dial rejected non-loopback address %q", address)
				}
				return nil
			},
		}).DialContext,
		DisableKeepAlives: true,
	},
	// A dev server that never answers must not hold a phone command slot
	// anywhere near the 15s COMMAND_TIMEOUT_MS tripwire.
	Timeout: localhostFetchTimeout,
}

// localhostBaseURL is a package var so tests can point the fetch core at an
// arbitrary loopback listener; the handler always calls it with a validated
// port, and production builds always produce the literal loopback host.
var localhostBaseURL = func(port int, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
}

// validateLocalhostPort enforces the allowed dev-server port range.
func validateLocalhostPort(port int) error {
	if port < localhostMinPort || port > localhostMaxPort {
		return fmt.Errorf("Port must be between %d and %d", localhostMinPort, localhostMaxPort)
	}
	return nil
}

// validateLocalhostPath accepts only absolute URL paths: no scheme, no
// authority, no userinfo, no control characters. Anything else could turn
// the fixed host into a different destination.
func validateLocalhostPath(path string) (string, error) {
	if path == "" {
		return "/", nil
	}
	if strings.ContainsAny(path, " \t\r\n") {
		return "", fmt.Errorf("path contains whitespace")
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must start with /")
	}
	if strings.Contains(path, "://") || strings.Contains(path, "@") || strings.Contains(path, "\\") {
		return "", fmt.Errorf("path must be a plain URL path")
	}
	return path, nil
}

type localhostResponse struct {
	Status      int
	ContentType string
	Body        []byte
	Truncated   bool
}

// localhostFetch performs the bounded loopback GET. Port and path must
// already be validated by the caller.
func localhostFetch(ctx context.Context, port int, path string) (*localhostResponse, error) {
	url := localhostBaseURL(port, path)
	ctx, cancel := context.WithTimeout(ctx, localhostFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid localhost request")
	}
	resp, err := localhostHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no service responded on port %d", port)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, localhostMaxResponseBytes+1))
	if readErr != nil && readErr != io.EOF {
		return nil, fmt.Errorf("service response could not be read")
	}
	truncated := false
	if len(body) > localhostMaxResponseBytes {
		body = body[:localhostMaxResponseBytes]
		truncated = true
	}
	return &localhostResponse{
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
		Truncated:   truncated,
	}, nil
}

func (s *Server) handleLocalhostFetch(client *transport.ClientConn, msg map[string]any) {
	requestID, _ := msg["request_id"].(string)
	fail := func(publicError string) {
		s.sendCommandResult(client, requestID, "local_http_fetch", false, "failed", publicError, "", nil)
	}

	port := messageInt(msg["port"], 0)
	if portErr := validateLocalhostPort(port); portErr != nil {
		fail(portErr.Error())
		return
	}
	rawPath, _ := msg["path"].(string)
	path, pathErr := validateLocalhostPath(rawPath)
	if pathErr != nil {
		fail(pathErr.Error())
		return
	}

	response, err := localhostFetch(client.Context(), port, path)
	if err != nil {
		fail(err.Error())
		return
	}

	s.sendCommandResult(client, requestID, "local_http_fetch", true, "completed", "", "", map[string]any{
		"status":       response.Status,
		"content_type": response.ContentType,
		"body":         base64.StdEncoding.EncodeToString(response.Body),
		"truncated":    response.Truncated,
	})
}
