package coordinator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/profiles"
)

func TestLifecycleStartsAgentInNestedWorkspaceCreateRootPane(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	record := filepath.Join(dir, "invocations.log")
	bin := writeScript(t, dir, "herdr", "#!/bin/sh\n"+
		"printf '%s\\n' \"$*\" >> \""+record+"\"\n"+
		"case \"$1 $2\" in\n"+
		"  'pane list') printf '%s\\n' '{\"result\":{\"panes\":[]}}' ;;\n"+
		"  'workspace list') printf '%s\\n' '{\"result\":{\"workspaces\":[]}}' ;;\n"+
		"  'workspace create') printf '%s\\n' '{\"result\":{\"type\":\"workspace_created\",\"workspace\":{\"workspace_id\":\"workspace-new\",\"label\":\"project\"},\"tab\":{\"tab_id\":\"tab-new\",\"workspace_id\":\"workspace-new\",\"label\":\"project\"},\"root_pane\":{\"pane_id\":\"pane-new\",\"tab_id\":\"tab-new\",\"workspace_id\":\"workspace-new\"}}}' ;;\n"+
		"  'tab rename') printf '%s\\n' '{\"result\":{\"tab_id\":\"tab-new\"}}' ;;\n"+
		"  'agent start') printf '%s\\n' '{\"result\":{\"type\":\"agent_started\",\"agent\":{\"pane_id\":\"pane-new\",\"agent\":\"codex\",\"name\":\"project-codex\"}}}' ;;\n"+
		"  *) exit 2 ;;\n"+
		"esac\n")

	resolver := profiles.NewResolver(filepath.Join(dir, "config"), nil)
	socketPath := filepath.Join(dir, "herdr.sock")
	startInventorySocket(t, socketPath, nil)
	lifecycle := &Lifecycle{
		herdr:    herdr.NewClient(bin, socketPath),
		profiles: resolver,
		home:     home,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := lifecycle.Start(ctx, profiles.Profile{ID: "codex", Kind: "codex"}, StartRequest{
		ProfileID: "codex",
		Name:      "project-codex",
		Cwd:       cwd,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if result.PaneID != "pane-new" {
		t.Fatalf("Start() pane_id = %q, want pane-new", result.PaneID)
	}

	invocations, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read invocations: %v", err)
	}
	if !strings.Contains(string(invocations), "agent start project-codex --kind codex --pane pane-new") {
		t.Fatalf("agent was not started in created root pane:\n%s", invocations)
	}
}

func TestLifecycleStartsAgentInExplicitWorkspace(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "invocations.log")
	bin := writeScript(t, dir, "herdr", "#!/bin/sh\n"+
		"printf '%s\\n' \"$*\" >> \""+record+"\"\n"+
		"case \"$1 $2\" in\n"+
		"  'pane list') printf '%s\\n' '{\"result\":{\"panes\":[]}}' ;;\n"+
		"  'workspace list') printf '%s\\n' '{\"result\":{\"workspaces\":[{\"workspace_id\":\"workspace-existing\",\"label\":\"Project\"}]}}' ;;\n"+
		"  'tab create') printf '%s\\n' '{\"result\":{\"type\":\"tab_created\",\"tab\":{\"tab_id\":\"tab-new\",\"workspace_id\":\"workspace-existing\"},\"root_pane\":{\"pane_id\":\"pane-new\",\"tab_id\":\"tab-new\",\"workspace_id\":\"workspace-existing\"}}}' ;;\n"+
		"  'agent start') printf '%s\\n' '{\"result\":{\"type\":\"agent_started\",\"agent\":{\"pane_id\":\"pane-new\",\"agent\":\"codex\",\"name\":\"project-codex\"}}}' ;;\n"+
		"  *) exit 2 ;;\n"+
		"esac\n")
	socketPath := filepath.Join(dir, "herdr.sock")
	startInventorySocket(t, socketPath, []any{
		map[string]any{"workspace_id": "workspace-existing", "label": "Project"},
	})
	lifecycle := &Lifecycle{
		herdr:    herdr.NewClient(bin, socketPath),
		profiles: profiles.NewResolver(filepath.Join(dir, "config"), nil),
		home:     home,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := lifecycle.Start(ctx, profiles.Profile{ID: "codex", Kind: "codex"}, StartRequest{
		ProfileID:   "codex",
		WorkspaceID: "workspace-existing",
		Name:        "project-codex",
		Cwd:         cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspaceID != "workspace-existing" || result.PaneID != "pane-new" {
		t.Fatalf("result = %+v", result)
	}
	invocations, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(invocations), "tab create --workspace workspace-existing") {
		t.Fatalf("explicit workspace was not used:\n%s", invocations)
	}
	if strings.Contains(string(invocations), "workspace create") {
		t.Fatalf("unexpected workspace create:\n%s", invocations)
	}
}

// An agent start whose dispatch outcome is uncertain (dispatched_unknown) but
// that left no running agent must close the workspace/tab createTarget just
// made. Otherwise the orphaned empty workspace lingers on the desktop and
// resurfaces on the phone as a recurring empty item.
func TestLifecycleClosesOrphanedWorkspaceOnEmptyDispatch(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "invocations.log")
	// agent start exits non-zero with a non-JSON error → ErrDispatchedUnknown
	// (the subprocess ran but its result is uncertain). agent get then reports
	// no running agent, so the orphaned workspace must be closed.
	bin := writeScript(t, dir, "herdr", "#!/bin/sh\n"+
		"printf '%s\\n' \"$*\" >> \""+record+"\"\n"+
		"case \"$1 $2\" in\n"+
		"  'pane list') printf '%s\\n' '{\"result\":{\"panes\":[]}}' ;;\n"+
		"  'workspace list') printf '%s\\n' '{\"result\":{\"workspaces\":[]}}' ;;\n"+
		"  'workspace create') printf '%s\\n' '{\"result\":{\"type\":\"workspace_created\",\"workspace\":{\"workspace_id\":\"workspace-new\",\"label\":\"project\"},\"root_pane\":{\"pane_id\":\"pane-new\",\"workspace_id\":\"workspace-new\"}}}' ;;\n"+
		"  'agent start') printf '%s\\n' 'spawn blew up' >&2; exit 1 ;;\n"+
		"  'agent get') printf '%s\\n' '{\"result\":{\"pane_id\":\"pane-new\",\"running\":false}}' ;;\n"+
		"  'pane close') printf '%s\\n' '{\"result\":{\"type\":\"ok\"}}' ;;\n"+
		"  *) exit 2 ;;\n"+
		"esac\n")

	resolver := profiles.NewResolver(filepath.Join(dir, "config"), nil)
	socketPath := filepath.Join(dir, "herdr.sock")
	startInventorySocket(t, socketPath, nil)
	lifecycle := &Lifecycle{
		herdr:    herdr.NewClient(bin, socketPath),
		profiles: resolver,
		home:     home,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := lifecycle.Start(ctx, profiles.Profile{ID: "codex", Kind: "codex"}, StartRequest{
		ProfileID: "codex",
		Name:      "project-codex",
		Cwd:       cwd,
	})
	if err == nil || !errors.Is(err, herdr.ErrDispatchedUnknown) {
		t.Fatalf("Start() error = %v, want ErrDispatchedUnknown", err)
	}
	invocations, rerr := os.ReadFile(record)
	if rerr != nil {
		t.Fatalf("read invocations: %v", rerr)
	}
	if !strings.Contains(string(invocations), "pane close pane-new") {
		t.Fatalf("orphaned pane was not closed:\n%s", invocations)
	}
}

// When dispatched_unknown leaves a live agent in the target, the workspace
// must stay open — closing it would kill work the phone is told to review.
func TestLifecycleKeepsWorkspaceWhenAgentRunning(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "invocations.log")
	bin := writeScript(t, dir, "herdr", "#!/bin/sh\n"+
		"printf '%s\\n' \"$*\" >> \""+record+"\"\n"+
		"case \"$1 $2\" in\n"+
		"  'pane list') printf '%s\\n' '{\"result\":{\"panes\":[]}}' ;;\n"+
		"  'workspace list') printf '%s\\n' '{\"result\":{\"workspaces\":[]}}' ;;\n"+
		"  'workspace create') printf '%s\\n' '{\"result\":{\"type\":\"workspace_created\",\"workspace\":{\"workspace_id\":\"workspace-new\",\"label\":\"project\"},\"root_pane\":{\"pane_id\":\"pane-new\",\"workspace_id\":\"workspace-new\"}}}' ;;\n"+
		"  'agent start') printf '%s\\n' 'spawn blew up' >&2; exit 1 ;;\n"+
		"  'agent get') printf '%s\\n' '{\"result\":{\"pane_id\":\"pane-new\",\"running\":true,\"agent_status\":\"working\"}}' ;;\n"+
		"  'pane close') printf '%s\\n' '{\"result\":{\"type\":\"ok\"}}' ;;\n"+
		"  *) exit 2 ;;\n"+
		"esac\n")

	resolver := profiles.NewResolver(filepath.Join(dir, "config"), nil)
	socketPath := filepath.Join(dir, "herdr.sock")
	startInventorySocket(t, socketPath, nil)
	lifecycle := &Lifecycle{
		herdr:    herdr.NewClient(bin, socketPath),
		profiles: resolver,
		home:     home,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := lifecycle.Start(ctx, profiles.Profile{ID: "codex", Kind: "codex"}, StartRequest{
		ProfileID: "codex",
		Name:      "project-codex",
		Cwd:       cwd,
	})
	if err == nil || !errors.Is(err, herdr.ErrDispatchedUnknown) {
		t.Fatalf("Start() error = %v, want ErrDispatchedUnknown", err)
	}
	if result.PaneID != "pane-new" {
		t.Fatalf("Start() pane_id = %q, want pane-new (live agent kept)", result.PaneID)
	}
	invocations, rerr := os.ReadFile(record)
	if rerr != nil {
		t.Fatalf("read invocations: %v", rerr)
	}
	if strings.Contains(string(invocations), "pane close") {
		t.Fatalf("live agent's workspace was closed:\n%s", invocations)
	}
}
