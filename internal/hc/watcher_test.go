package hc

import (
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
)

func TestAgentRecordStatusFromAppWindow(t *testing.T) {
	space := Space{ID: "sp1", Name: "of-machinery", Folder: "/Users/apple/Developer/of-machinery"}
	tab := Tab{ID: "tb1", Name: "THE BOSS"}
	term := Terminal{Address: "t1s", Status: new("idle"), App: new("omp"), Name: new("the-boss-9c")}
	agent := agentRecord("mac-mini", "core1", space, tab, term, nil)

	if agent.PaneID != "hc:t1s" || agent.RawPaneID != "hc:t1s" {
		t.Fatalf("pane id = %q", agent.PaneID)
	}
	if agent.TerminalID != "t1s" {
		t.Fatalf("terminal id = %q", agent.TerminalID)
	}
	if agent.Status != "idle" || agent.Agent != "omp" || agent.Name != "the-boss-9c" {
		t.Fatalf("record = %#v", agent)
	}
	if agent.Project != "of-machinery" {
		t.Fatalf("project = %q", agent.Project)
	}
	if agent.TabLabel != "THE BOSS" || agent.WorkspaceID != "sp1" {
		t.Fatalf("topology fields = %#v", agent)
	}
	if agent.ServerSessionID != "primary" {
		t.Fatalf("server_session_id = %q", agent.ServerSessionID)
	}
}

func TestAgentRecordStatusFromCoreFallback(t *testing.T) {
	space := Space{ID: "hc-core:t9", Name: "Core terminals"}
	tab := Tab{ID: "hc-core:t9"}
	term := Terminal{Address: "t9"}
	core := &CoreTerminal{Address: "t9", Agent: "hc", Cwd: "/tmp/x", State: "live"}

	agent := agentRecord("mac-mini", "core1", space, tab, term, core)
	if agent.Status != "idle" {
		t.Fatalf("live terminal status = %q, want idle", agent.Status)
	}
	if agent.Cwd != "/tmp/x" || agent.Project != "x" {
		t.Fatalf("cwd/project = %q %q", agent.Cwd, agent.Project)
	}
}

func TestAgentRecordFinishedTerminalReadsDone(t *testing.T) {
	space := Space{ID: "hc-core:t3", Name: "Core terminals"}
	tab := Tab{ID: "hc-core:t3"}
	term := Terminal{Address: "t3"}
	core := &CoreTerminal{Address: "t3", State: "finished", ExitCode: new(0)}

	agent := agentRecord("mac-mini", "core1", space, tab, term, core)
	if agent.Status != "done" {
		t.Fatalf("finished terminal status = %q, want done", agent.Status)
	}
}

func TestAgentRecordMachineScopedAddress(t *testing.T) {
	space := Space{ID: "macbookpro-codex/sp1", Name: "prd-herdr", Machine: new("macbookpro-codex")}
	tab := Tab{ID: "macbookpro-codex/tb1", Name: "THE BOSS"}
	term := Terminal{Address: "macbookpro-codex/t1s", Status: new("working")}

	agent := agentRecord("mac-mini", "core1", space, tab, term, nil)
	if agent.PaneID != "hc:macbookpro-codex/t1s" {
		t.Fatalf("machine-scoped pane id = %q", agent.PaneID)
	}
	if agent.TerminalID != "macbookpro-codex/t1s" {
		t.Fatalf("terminal id = %q", agent.TerminalID)
	}
	if agent.Status != "working" {
		t.Fatalf("status = %q", agent.Status)
	}
}

func TestBusyTitleDetectsBrailleSpinner(t *testing.T) {
	if !BusyTitle("π ⠙ ADMIT: fix") {
		t.Fatal("spinner title not detected")
	}
	if BusyTitle("plain title") || BusyTitle("") {
		t.Fatal("false positive on plain title")
	}
}

func TestSnapshotFingerprintDetectsVisibleChanges(t *testing.T) {
	mk := func(status string) Snapshot {
		a := agentRecord("h", "c1",
			Space{ID: "s", Name: "n"}, Tab{ID: "t", Name: "tab"},
			Terminal{Address: "t1", Status: new(status)}, nil)
		return Snapshot{Available: true, ServerID: "c1", Agents: []*coordinator.AgentState{a}}
	}
	idle := mk("idle")
	working := mk("working")
	if snapshotFingerprint(idle) == snapshotFingerprint(working) {
		t.Fatal("status change did not move fingerprint")
	}
	again := mk("idle")
	if snapshotFingerprint(idle) != snapshotFingerprint(again) {
		t.Fatal("identical snapshots fingerprint differently")
	}
	down := idle
	down.Available = false
	if snapshotFingerprint(idle) == snapshotFingerprint(down) {
		t.Fatal("availability flip did not move fingerprint")
	}
}
