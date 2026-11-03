package hc

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// fakeHcBins writes script binaries that answer the CLI calls the watcher
// makes: `herdr-app ls`, `herdr-core server status|ls|get`.
func fakeHcBins(t *testing.T, appScript, coreScript string) *Client {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "herdr-app")
	core := filepath.Join(dir, "herdr-core")
	if err := os.WriteFile(app, []byte(appScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte(coreScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewClient(app, core)
}

func TestPollShowsOnlyMountedTerminalsWhenWindowOpen(t *testing.T) {
	// The bug: hc ls lists detached workers + finished shells the window
	// never mounted, and the phone rendered them all as "Core terminals".
	// Only the mounted t11 may appear while the window answers.
	app := `#!/bin/sh
cat <<'EOF'
{"spaces":[{"id":"sp1","name":"~","folder":"/Users/apple","machine":null,"active":true,"tabs":[{"id":"tb1","name":"1","active":true,"settled":false,"terminals":[{"terminal":"t11","pane":"p1","status":"idle","app":"zsh","name":null,"sub_agents":[]}]}]}]}
EOF`
	core := `#!/bin/sh
case "$1" in
server) echo '{"core":{"id":"c1","name":"mac-mini"},"version":"0.1.16"}' ;;
ls) echo '{"machines":[{"name":"macbookpro-codex","state":"connected"}],"terminals":[
	{"address":"t1","agent":"hc","cwd":"/Users/apple","name":"add-jump","state":"live"},
	{"address":"t11","agent":"zsh","cwd":"/Users/apple","state":"live"},
	{"address":"t12","agent":"zsh","cwd":"/Users/apple","state":"finished"},
	{"address":"macbookpro-codex/t1s","agent":"omp","cwd":"/Users/apple","state":"live"}]}' ;;
get) echo '{"address":"'$2'","agent":"zsh","cwd":"/Users/apple","state":"live","title":""}' ;;
esac`
	w := NewWatcher(fakeHcBins(t, app, core), "mac-mini", slog.Default())
	w.poll(context.Background())

	snap := w.Current()
	if len(snap.Agents) != 1 || snap.Agents[0].TerminalID != "t11" {
		t.Fatalf("agents = %v, want only mounted t11", paneIDs(snap.Agents))
	}
	for _, space := range snap.Spaces {
		if strings.HasPrefix(space.ID, "hc-core:") {
			t.Fatalf("synthetic core space leaked into an open-window snapshot: %v", snap.Spaces)
		}
	}
	if len(snap.Machines) != 1 || snap.Machines[0] != "macbookpro-codex" {
		t.Fatalf("machines = %v", snap.Machines)
	}
}

func TestPollCoreFallbackWhenWindowClosed(t *testing.T) {
	// herdr-app fails (window closed) → live core terminals become the
	// inventory; finished ones are still noise.
	app := "#!/bin/sh\nexit 1"
	core := `#!/bin/sh
case "$1" in
server) echo '{"core":{"id":"c1","name":"mac-mini"},"version":"0.1.16"}' ;;
ls) echo '{"machines":[],"terminals":[
	{"address":"t1","agent":"hc","cwd":"/Users/apple","name":"add-jump","state":"live"},
	{"address":"t12","agent":"zsh","cwd":"/Users/apple","state":"finished"}]}' ;;
get) echo '{"address":"'$2'","agent":"hc","cwd":"/Users/apple","state":"live"}' ;;
esac`
	w := NewWatcher(fakeHcBins(t, app, core), "mac-mini", slog.Default())
	w.poll(context.Background())

	snap := w.Current()
	if len(snap.Agents) != 1 || snap.Agents[0].TerminalID != "t1" {
		t.Fatalf("agents = %v, want live-only [t1]", paneIDs(snap.Agents))
	}
	if len(snap.Spaces) != 1 || snap.Spaces[0].ID != "hc-core:t1" {
		t.Fatalf("spaces = %v, want [hc-core:t1]", snap.Spaces)
	}
	if snap.Message == "" {
		t.Fatal("window-closed message missing")
	}
}

func paneIDs(agents []*coordinator.AgentState) []string {
	ids := make([]string, 0, len(agents))
	for _, a := range agents {
		ids = append(ids, a.TerminalID)
	}
	return ids
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

func TestLocalSpaceIDPinsThisMachine(t *testing.T) {
	remote := "macbookpro-codex"
	local := Space{ID: "sp1", Name: "~"}
	remoteSpace := Space{ID: "sp2", Name: "prd", Machine: &remote}
	if got := LocalSpaceID([]Space{remoteSpace, local}); got != "sp1" {
		t.Fatalf("local space = %q, want sp1", got)
	}
	if got := LocalSpaceID([]Space{remoteSpace}); got != "" {
		t.Fatalf("remote-only topology = %q, want empty (space.new path)", got)
	}
	if got := LocalSpaceID(nil); got != "" {
		t.Fatalf("empty topology = %q", got)
	}
}

func TestTabNewPinsSpaceAndRunsDo(t *testing.T) {
	// A recorder binary: captures argv, answers {"ok":true}.
	dir := t.TempDir()
	bin := filepath.Join(dir, "herdr-app")
	rec := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" > '" + rec + "'\necho '{\"ok\":true}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := NewClient(bin, bin)
	if err := c.TabNew(context.Background(), "sp1", "tab", "/tmp/x", []string{"zsh"}, "hi"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(rec)
	want := "do\ntab.new\n--space\nsp1\n--name\ntab\n--folder\n/tmp/x\n--argv\n[\"zsh\"]\n--task\nhi\n"
	if string(got) != want {
		t.Fatalf("args:\n%s\nwant:\n%s", got, want)
	}

	// No local space → space.new with --tab instead of --name.
	if err := c.TabNew(context.Background(), "", "tab", "/tmp/x", nil, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(rec)
	want = "do\nspace.new\n--tab\ntab\n--folder\n/tmp/x\n"
	if string(got) != want {
		t.Fatalf("space.new args:\n%s\nwant:\n%s", got, want)
	}
}

func TestPollGroupsLabeledTerminalsWhenWindowClosed(t *testing.T) {
	// Window unreachable → flat hc inventory; but the events stream's full
	// records carry herdr.space/herdr.tab labels, so mounted terminals
	// must regroup into their real space/tab instead of Core terminals.
	app := "#!/bin/sh\nexit 1"
	core := `#!/bin/sh
case "$1" in
server) echo '{"core":{"id":"c1","name":"mac-mini"},"version":"0.1.16"}' ;;
ls) echo '{"machines":[],"terminals":[
	{"address":"t2f","agent":"omp","cwd":"/Users/apple/Developer/of-machinery","name":"boss","state":"live"},
	{"address":"t9g","agent":"omp","cwd":"/Users/apple/Developer/of-machinery","name":"lane","state":"live"},
	{"address":"t1","agent":"hc","cwd":"/Users/apple","name":"add-jump","state":"live"}]}' ;;
get) echo '{"address":"'$2'","agent":"omp","cwd":"/Users/apple","state":"live"}' ;;
esac`
	w := NewWatcher(fakeHcBins(t, app, core), "mac-mini", slog.Default())
	w.onCoreEvent(CoreEvent{Snapshot: []CoreTerminal{
		{Address: "t2f", State: "live", Status: TerminalStatus{State: "working", App: "omp"},
			Labels: map[string]string{
				"herdr.space": "spa1", "herdr.tab": "tb1",
				"ofm.tab_label": "THE BOSS", "where": "of-machinery / THE BOSS"}},
		{Address: "t9g", State: "live", Status: TerminalStatus{State: "working", App: "omp"},
			Labels: map[string]string{
				"herdr.space": "spa1", "herdr.tab": "tb2",
				"ofm.tab_label": "L706", "where": "of-machinery / L706"}},
		{Address: "t1", State: "live", Name: "add-jump"},
	}})
	w.poll(context.Background())

	snap := w.Current()
	// Labeled terminals must land in ONE synthesized space with two tabs.
	var space *Space
	for i := range snap.Spaces {
		if snap.Spaces[i].ID == "spa1" {
			space = &snap.Spaces[i]
		}
	}
	if space == nil {
		t.Fatalf("no labeled space in snapshot: %+v", snap.Spaces)
	}
	if space.Name != "of-machinery" || len(space.Tabs) != 2 {
		t.Fatalf("space = %+v", space)
	}
	names := map[string]bool{space.Tabs[0].Name: true, space.Tabs[1].Name: true}
	if !names["L706"] || !names["THE BOSS"] {
		t.Fatalf("tabs = %+v", space.Tabs)
	}
	// The label-less worker stays in Core terminals.
	var coreSpace bool
	for _, sp := range snap.Spaces {
		if sp.ID == "hc-core:t1" {
			coreSpace = true
		}
	}
	if !coreSpace {
		t.Fatalf("unlabeled worker lost: %+v", snap.Spaces)
	}
	// Agents carry the synthesized workspace/tab identity.
	byTerm := map[string]*coordinator.AgentState{}
	for _, a := range snap.Agents {
		byTerm[a.TerminalID] = a
	}
	if byTerm["t2f"].WorkspaceID != "spa1" || byTerm["t2f"].TabID != "tb1" ||
		byTerm["t2f"].TabLabel != "THE BOSS" || byTerm["t2f"].SessionName != "of-machinery" {
		t.Fatalf("t2f agent = %+v", byTerm["t2f"])
	}
	if byTerm["t9g"].TabID != "tb2" {
		t.Fatalf("t9g agent = %+v", byTerm["t9g"])
	}
}

func TestEventStorePatchesTitlesAndDirtiesOthers(t *testing.T) {
	w := NewWatcher(&Client{}, "mac-mini", slog.Default())
	w.wake = make(chan struct{}, 1)
	w.onCoreEvent(CoreEvent{Snapshot: []CoreTerminal{
		{Address: "t1", State: "live", Title: "old"},
	}})
	w.onCoreEvent(CoreEvent{Type: "title", Terminal: "t1", Title: "new"})
	if got := w.records["t1"].Title; got != "new" {
		t.Fatalf("title patch = %q", got)
	}
	w.onCoreEvent(CoreEvent{Type: "status", Terminal: "t1"})
	if !w.dirtyAddrs["t1"] {
		t.Fatal("status event did not dirty the record")
	}
}

func TestPollUsesStreamRecordsWithoutGet(t *testing.T) {
	// With the stream live, poll must read chips/status from the record
	// store and never shell out to `hc get` (the fake would fail it).
	app := "#!/bin/sh\nexit 1"
	core := `#!/bin/sh
case "$1" in
server) echo '{"core":{"id":"c1","name":"m"},"version":"0.1.16"}' ;;
ls) echo '{"machines":[],"terminals":[]}' ;;
get) exit 1 ;;
esac`
	w := NewWatcher(fakeHcBins(t, app, core), "m", slog.Default())
	w.onCoreEvent(CoreEvent{Snapshot: []CoreTerminal{{
		Address: "t2f", State: "live",
		Status: TerminalStatus{State: "working", App: "omp"},
		Argv:   []string{"/usr/bin/env", "OFM_ROLE=boss", "/bin/zsh", "-l"},
		Foreground: &struct {
			Argv []string `json:"argv"`
			Name string   `json:"name"`
		}{Argv: []string{"omp", "--model", "pi/slow"}, Name: "omp"},
		Labels: map[string]string{"herdr.space": "spa1", "herdr.tab": "tb1",
			"ofm.tab_label": "THE BOSS", "where": "of-machinery / THE BOSS"},
	}}})
	w.poll(context.Background())

	snap := w.Current()
	if len(snap.Agents) != 1 {
		t.Fatalf("agents = %v", paneIDs(snap.Agents))
	}
	a := snap.Agents[0]
	if a.Status != "working" || a.AgentRole != "slow" || a.TabLabel != "THE BOSS" {
		t.Fatalf("agent = status:%q role:%q tab:%q", a.Status, a.AgentRole, a.TabLabel)
	}
}
