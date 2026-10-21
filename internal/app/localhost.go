package app

import (
	"bytes"
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
//   - local_http_fetch: GET only; local_http_request: POST only with a
//     64 KiB base64 body; both bounded in duration and response size
//
// The mutating variant is a separate action so reader-role credentials are
// denied POSTs while keeping GET browsing. The visible port list is the
// phone's Localhost Ports setting; any valid TCP port is served. Widening
// the HOST beyond loopback would turn a paired phone credential into an
// SSRF pivot — keep that restriction.
const (
	localhostMinPort          = 1
	localhostMaxPort          = 65535
	localhostFetchTimeout     = 10 * time.Second
	localhostMaxResponseBytes = 2 << 20 // 2 MiB
	localhostMaxRequestBytes  = 64 << 10 // 64 KiB request body cap
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

// localhostRequest performs the bounded loopback request. Port and path must
// already be validated by the caller; method must already be validated.
// body may be nil; contentType is sent verbatim when non-empty.
func localhostRequest(ctx context.Context, method string, port int, path string, body []byte, contentType string) (*localhostResponse, error) {
	url := localhostBaseURL(port, path)
	ctx, cancel := context.WithTimeout(ctx, localhostFetchTimeout)
	defer cancel()
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("invalid localhost request")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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

	response, err := localhostRequest(client.Context(), http.MethodGet, port, path, nil, "")
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

// handleLocalhostRequest is the mutating counterpart of local_http_fetch:
// POST only, base64 body capped at localhostMaxRequestBytes, same loopback
// envelope. Registered as ActionMutating so reader-role devices are denied.
func (s *Server) handleLocalhostRequest(client *transport.ClientConn, msg map[string]any) {
	requestID, _ := msg["request_id"].(string)
	fail := func(publicError string) {
		s.sendCommandResult(client, requestID, "local_http_request", false, "failed", publicError, "", nil)
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
	method, _ := msg["method"].(string)
	if method != http.MethodPost {
		fail("Only POST is supported")
		return
	}
	var body []byte
	if encoded, _ := msg["body"].(string); encoded != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil {
			fail("Request body is not valid base64")
			return
		}
		if len(decoded) > localhostMaxRequestBytes {
			fail(fmt.Sprintf("Request body exceeds %d bytes", localhostMaxRequestBytes))
			return
		}
		body = decoded
	}
	contentType, _ := msg["content_type"].(string)
	if strings.ContainsAny(contentType, "\r\n") {
		fail("Invalid content type")
		return
	}

	response, err := localhostRequest(client.Context(), http.MethodPost, port, path, body, contentType)
	if err != nil {
		fail(err.Error())
		return
	}

	s.sendCommandResult(client, requestID, "local_http_request", true, "completed", "", "", map[string]any{
		"status":       response.Status,
		"content_type": response.ContentType,
		"body":         base64.StdEncoding.EncodeToString(response.Body),
		"truncated":    response.Truncated,
	})
}
