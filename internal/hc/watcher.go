package hc

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
)

// Snapshot is the phone-facing view of the new Herdr app: spaces with their
// tabs (window topology when the window is open), plus every terminal as an
// AgentState so list/detail views can reuse the legacy card + frame plumbing.
type Snapshot struct {
	Available   bool
	Message     string
	ServerID    string
	// Machines lists the federation's remote machine names; qualified
	// <machine>/<addr> terminals mirror an owning machine's bare terminal, so
	// the phone needs them to fold duplicates when both machines are paired.
	Machines    []string
	Spaces      []Space
	Agents      []*coordinator.AgentState
	LastSuccess int64
	LastAttempt int64
}

// Watcher polls the app window and the core daemon, diffs the result, and
// calls onChange whenever the phone-visible snapshot moved.
type Watcher struct {
	client   *Client
	hostname string
	interval time.Duration
	logger   *slog.Logger
	onChange func(Snapshot)

	mu       sync.Mutex
	snapshot Snapshot
	// fingerprints detect changes without re-broadcasting identical polls.
	fingerprint string
}

func NewWatcher(client *Client, hostname string, logger *slog.Logger) *Watcher {
	return &Watcher{
		client:   client,
		hostname: hostname,
		interval: 5 * time.Second,
		logger:   logger,
	}
}

// SetOnChange installs the single change callback (called with the watcher
// lock released).
func (w *Watcher) SetOnChange(fn func(Snapshot)) { w.onChange = fn }

// SetInterval overrides the poll cadence (tests).
func (w *Watcher) SetInterval(d time.Duration) {
	if d > 0 {
		w.interval = d
	}
}

// Current returns the latest snapshot.
func (w *Watcher) Current() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshot
}

// Agent returns the synthesized agent record for a pane id (hc:<address>).
func (w *Watcher) Agent(paneID string) (*coordinator.AgentState, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, a := range w.snapshot.Agents {
		if a.PaneID == paneID {
			return a, true
		}
	}
	return nil, false
}

// Run polls until ctx ends. Poll failures keep the last good snapshot but mark
// availability false only when both the window and the core are unreachable.
func (w *Watcher) Run(ctx context.Context) {
	w.poll(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *Watcher) poll(ctx context.Context) {
	now := time.Now().Unix()
	snap := Snapshot{LastAttempt: now}

	serverID, serverErr := w.client.ServerID(ctx)
	spaces, topoErr := w.client.Topology(ctx)
	terminals, machines, termErr := w.client.Terminals(ctx)

	if serverErr != nil && termErr != nil {
		snap.Available = false
		snap.Message = "Herdr App core is not running"
		w.commit(snap)
		return
	}
	snap.Available = true
	snap.ServerID = serverID
	if topoErr != nil {
		snap.Message = "Herdr App window is closed; showing core terminals"
	}
	snap.Machines = machines
	snap.Spaces = spaces
	snap.LastSuccess = now

	byAddress := map[string]*CoreTerminal{}
	for i := range terminals {
		byAddress[terminals[i].Address] = &terminals[i]
	}
	seen := map[string]bool{}
	var agents []*coordinator.AgentState

	for _, space := range spaces {
		for _, tab := range space.Tabs {
			for _, term := range tab.Terminals {
				core := byAddress[term.Address]
				agents = append(agents, agentRecord(w.hostname, serverID, space, tab, term, core))
				seen[term.Address] = true
			}
		}
	}
	// The app view mirrors what the window mounts — detached core workers
	// (hc's own add-* jobs, finished shells) must NOT appear alongside real
	// spaces; hc ls still lists them as "live" long after their work ended.
	// `hc ls` remains the fallback ONLY for a closed/unreachable window, and
	// even then finished terminals are noise, not content.
	if topoErr != nil {
		for i := range terminals {
			t := &terminals[i]
			if seen[t.Address] || t.State != "live" {
				continue
			}
			space := Space{ID: "hc-core:" + t.Address, Name: "Core terminals", Tabs: []Tab{{
				ID: "hc-core:" + t.Address, Name: t.Name,
			}}}
			// The phone renders core terminals from spaces; the synthetic
			// space must ride the snapshot, not just the agent record.
			snap.Spaces = append(snap.Spaces, space)
			term := Terminal{Address: t.Address}
			agents = append(agents, agentRecord(w.hostname, serverID, space, space.Tabs[0], term, t))
		}
	}
	// The window reports status; the compact `hc ls` record does not. Fetch
	// full records (bounded) so un-windowed and still-live terminals get a
	// real working/idle status and their live title.
	const getLimit = 24
	fetched := 0
	for _, a := range agents {
		if fetched >= getLimit {
			break
		}
		full, err := w.client.Get(ctx, a.TerminalID)
		if err != nil {
			continue
		}
		fetched++
		if full.Status != nil && *full.Status != "" {
			a.Status = *full.Status
		} else if a.Status == "idle" && BusyTitle(full.Title) {
			a.Status = "working"
		}
		if full.Title != "" && a.TabLabel == "" {
			a.TabLabel = full.Title
		}
	}
	snap.Agents = agents
	w.commit(snap)
}

// agentRecord synthesizes the AgentState the phone consumes. Pane ids are
// namespaced with hc: so they can never collide with legacy pane ids; the
// hc <address> (which may itself be machine/tN for remote machines) is the
// raw pane id and the hc terminal_id alike.
func agentRecord(hostname, serverID string, space Space, tab Tab, term Terminal, core *CoreTerminal) *coordinator.AgentState {
	address := term.Address
	if address == "" && core != nil {
		address = core.Address
	}
	status := ""
	if term.Status != nil {
		status = *term.Status
	}
	if status == "" && core != nil && core.Status != nil {
		status = *core.Status
	}
	if status == "" && core != nil {
		// hc statuses: live terminals report working/idle via title events;
		// state is the durable truth (live/finished).
		if core.State == "finished" || core.ExitCode != nil {
			status = "done"
		} else {
			status = "idle"
		}
	}
	agent := ""
	if term.App != nil {
		agent = *term.App
	}
	if agent == "" && core != nil {
		agent = core.Agent
	}
	name := ""
	if term.Name != nil {
		name = *term.Name
	}
	cwd := space.Folder
	if core != nil && core.Cwd != "" {
		cwd = core.Cwd
	}
	title := ""
	if core != nil {
		title = core.Title
	}
	tabLabel := tab.Name
	if tabLabel == "" {
		tabLabel = title
	}
	updated := int64(0)
	if core != nil {
		updated = core.CreatedMs
	}
	return &coordinator.AgentState{
		PaneID:          PanePrefix + address,
		RawPaneID:       PanePrefix + address,
		TerminalID:      address,
		ServerSessionID: "primary",
		Generation:      1,
		TabID:           tab.ID,
		TabLabel:        tabLabel,
		WorkspaceID:     space.ID,
		Agent:           agent,
		Name:            name,
		Status:          status,
		Focused:         tab.Active && space.Active,
		Cwd:             cwd,
		Project:         filepath.Base(strings.TrimRight(cwd, "/")),
		Host:            hostname,
		Session:         "hc",
		SessionName:     space.Name,
		UpdatedAt:       updated,
	}
}

// commit swaps the snapshot and fires onChange only when something the phone
// can see moved.
func (w *Watcher) commit(next Snapshot) {
	fp := snapshotFingerprint(next)
	w.mu.Lock()
	changed := fp != w.fingerprint
	w.fingerprint = fp
	w.snapshot = next
	w.mu.Unlock()
	if changed && w.onChange != nil {
		w.onChange(next)
	}
}
