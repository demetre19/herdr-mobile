package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

// The L1/L63 field report: panes launched with `omp --model pi/slow` showed
// DEFAULT because the session JSONL records `role` on model_change only for
// mid-session switches — a pane running its launch role has no role entry.
func TestLaunchRoleFromArgv(t *testing.T) {
	for _, test := range []struct {
		name string
		argv []string
		want string
	}{
		{name: "space form", argv: []string{"omp", "--model", "pi/slow"}, want: "slow"},
		{name: "equals form", argv: []string{"omp", "--model=pi/TOP-DOG"}, want: "TOP-DOG"},
		{name: "default role", argv: []string{"omp", "--model", "pi/default"}, want: "default"},
		{name: "model literal is not a role", argv: []string{"omp", "--model", "devin/swe-2"}, want: ""},
		{name: "bare launch", argv: []string{"omp"}, want: ""},
		{name: "resume carries no selector", argv: []string{"omp", "--resume", "01a14858"}, want: ""},
		{name: "continue carries no selector", argv: []string{"omp", "--continue"}, want: ""},
		{name: "empty selector", argv: []string{"omp", "--model", "pi/"}, want: ""},
		{name: "missing value", argv: []string{"omp", "--model"}, want: ""},
		{name: "nil", argv: nil, want: ""},
	} {
		if got := launchRoleFromArgv(test.argv); got != test.want {
			t.Errorf("%s: launchRoleFromArgv(%q) = %q, want %q", test.name, test.argv, got, test.want)
		}
	}
}

type stubProcessReader struct {
	info  *herdr.PaneProcessInfo
	err   error
	calls int
}

func (s *stubProcessReader) PaneProcessInfo(_ context.Context, _ string) (*herdr.PaneProcessInfo, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.info, nil
}

func launchPaneInfo(pane string, procs ...herdr.PaneProcess) *herdr.PaneProcessInfo {
	return &herdr.PaneProcessInfo{PaneID: pane, ShellPID: 1, ForegroundProcessGroupID: 3003, ForegroundProcesses: procs}
}

// The foreground group leader wins; any omp-named foreground process is the
// fallback when the leader is something else.
func TestLaunchArgvForPanePrefersGroupLeader(t *testing.T) {
	reader := &stubProcessReader{info: launchPaneInfo("pane-1",
		herdr.PaneProcess{PID: 9000, Name: "node", Argv: []string{"node", "serve"}},
		herdr.PaneProcess{PID: 3003, Name: "omp", Argv: []string{"omp", "--model", "pi/slow"}},
	)}
	if got := launchRoleFromArgv(launchArgvForPane(context.Background(), reader, "pane-1")); got != "slow" {
		t.Fatalf("role = %q, want %q", got, "slow")
	}
	if reader.calls != 1 {
		t.Fatalf("process-info calls = %d, want 1", reader.calls)
	}
}

func TestLaunchArgvForPaneIgnoresNonOmp(t *testing.T) {
	reader := &stubProcessReader{info: launchPaneInfo("pane-1",
		herdr.PaneProcess{PID: 3003, Name: "node", Argv: []string{"node", "serve"}},
	)}
	if got := launchArgvForPane(context.Background(), reader, "pane-1"); got != nil {
		t.Fatalf("argv = %q, want nil for a pane with no omp process", got)
	}
}

func TestLaunchArgvForPaneRejectsPaneMismatch(t *testing.T) {
	reader := &stubProcessReader{info: launchPaneInfo("other-pane",
		herdr.PaneProcess{PID: 3003, Name: "omp", Argv: []string{"omp", "--model", "pi/slow"}},
	)}
	if got := launchArgvForPane(context.Background(), reader, "pane-1"); got != nil {
		t.Fatalf("argv = %q, want nil on pane id mismatch", got)
	}
}

func writeMetaSession(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// A pane running its launch role (no role entry in the JSONL) keeps the
// profile default when no process reader is available — and testServer has
// no live herdr client, so this is exactly that path.
func TestResolveMetaDefaultsWithoutLaunchLookup(t *testing.T) {
	path := writeMetaSession(t,
		`{"type":"session","version":3}`,
		`{"type":"model_change","model":"openrouter/meta/muse-spark-1.3-contributor"}`,
	)
	server := testServer()
	meta := server.resolveAgentRuntimeMeta(context.Background(), "omp", "pane-1", path)
	if meta.role != "default" {
		t.Fatalf("role = %q, want %q without a launch lookup", meta.role, "default")
	}
	if meta.model != "openrouter/meta/muse-spark-1.3-contributor" {
		t.Fatalf("model = %q, want the recorded model", meta.model)
	}
}

// The resolver honors the same precedence the fallback implements: a
// JSONL-recorded role (mid-session switch) is live state and must never be
// overwritten by the launch fallback — covered here at the parse layer the
// fallback keys off.
func TestResolveMetaJsonlRoleRecorded(t *testing.T) {
	path := writeMetaSession(t,
		`{"type":"session","version":3}`,
		`{"type":"model_change","role":"smol","model":"devin/swe-2"}`,
	)
	server := testServer()
	meta := server.resolveAgentRuntimeMeta(context.Background(), "omp", "pane-1", path)
	if meta.role != "smol" {
		t.Fatalf("role = %q, want %q (JSONL wins)", meta.role, "smol")
	}
	if meta.model != "devin/swe-2" {
		t.Fatalf("model = %q, want recorded model", meta.model)
	}
}
