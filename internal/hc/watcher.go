package hc

import (
	"context"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/question"
)

// Snapshot is the phone-facing view of the new Herdr app: spaces with their
// tabs (window topology when the window is open), plus every terminal as an
// AgentState so list/detail views can reuse the legacy card + frame plumbing.
type Snapshot struct {
	Available bool
	Message   string
	ServerID  string
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
	// metaStates caches per-terminal session-JSONL meta (role/model/effort)
	// so unchanged files never re-read and remote resolves throttle.
	metaStates map[string]*sessionMetaState
	// records is the live terminal store fed by `hc events` — the core's
	// own inventory primitive. While streamLive, polls read these full
	// records (labels, foreground, status) instead of per-terminal `hc get`.
	records    map[string]*CoreTerminal
	dirtyAddrs map[string]bool
	streamLive bool
	// pollMu serializes interval ticks and event-wake polls.
	pollMu  sync.Mutex
	// wake fires poll ahead of the interval after any event, debounced so a
	// spinner-title burst collapses into one rebuild.
	wake       chan struct{}
	wakeTimer  *time.Timer
	lastPoll   time.Time
	// wakeMu covers wakeTimer/lastPoll (the AfterFunc fires off the Run
	// goroutine).
	wakeMu sync.Mutex
}

const (
	// wakeDebounce collapses event bursts into one poll.
	wakeDebounce = 400 * time.Millisecond
	// wakeMinInterval floors event-driven poll frequency — a busy fleet
	// emits hundreds of title ticks per minute, each one a wake.
	wakeMinInterval = 2 * time.Second
)


func NewWatcher(client *Client, hostname string, logger *slog.Logger) *Watcher {
	return &Watcher{
		client:   client,
		hostname: hostname,
		interval: 5 * time.Second,
		records:    map[string]*CoreTerminal{},
		dirtyAddrs: map[string]bool{},
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
	w.wake = make(chan struct{}, 1)
	go w.runEvents(ctx)
	w.poll(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			// Debounce: a wake timer re-polls once the burst settles,
			// floored so a fleet of title ticks polls at most every
			// wakeMinInterval.
			w.wakeMu.Lock()
			if w.wakeTimer != nil {
				w.wakeMu.Unlock()
				continue
			}
			delay := wakeDebounce
			if since := time.Since(w.lastPoll); since < wakeMinInterval {
				delay = wakeMinInterval - since
			}
			w.wakeTimer = time.AfterFunc(delay, func() {
				w.wakeMu.Lock()
				w.wakeTimer = nil
				w.wakeMu.Unlock()
				w.poll(ctx)
			})
			w.wakeMu.Unlock()
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

// runEvents keeps the `hc events` feed alive, restarting after drops. The
// stream is the core's single source of full terminal records — labels,
// foreground argv, status — so while it is live no per-terminal `hc get`
// is needed at all.
func (w *Watcher) runEvents(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		err := w.client.Events(ctx, w.onCoreEvent)
		w.mu.Lock()
		w.streamLive = false
		w.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		w.logger.Warn("hc events stream dropped; restarting", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// onCoreEvent maintains the live terminal store. The connect frame replaces
// it wholesale; title events patch in place; every other event kind marks
// the terminal dirty so the next poll re-reads its full record once.
func (w *Watcher) onCoreEvent(event CoreEvent) {
	w.mu.Lock()
	if event.Snapshot != nil {
		if w.records == nil {
			w.records = map[string]*CoreTerminal{}
		}
		for i := range event.Snapshot {
			term := event.Snapshot[i]
			w.records[term.Address] = &term
		}
		w.streamLive = true
	} else if w.streamLive {
		if rec := w.records[event.Terminal]; rec != nil && event.Type == "title" {
			rec.Title = event.Title
		} else if event.Terminal != "" && w.dirtyAddrs != nil {
			w.dirtyAddrs[event.Terminal] = true
		}
	}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Watcher) poll(ctx context.Context) {
	w.pollMu.Lock()
	defer w.pollMu.Unlock()
	now := time.Now().Unix()
	snap := Snapshot{LastAttempt: now}

	// The hc events stream is the primary inventory: one connect frame
	// carries every terminal's FULL record (labels, foreground argv,
	// status), and incremental events keep it fresh. Copy it under the
	// lock, then poll without the lock held.
	w.mu.Lock()
	streamLive := w.streamLive
	records := w.records
	dirty := w.dirtyAddrs
	w.dirtyAddrs = map[string]bool{}
	w.wakeMu.Lock()
	w.lastPoll = time.Now()
	w.wakeMu.Unlock()
	w.mu.Unlock()

	serverID, serverErr := w.client.ServerID(ctx)
	spaces, topoErr := w.client.Topology(ctx)
	terminals, machines, termErr := w.client.Terminals(ctx)

	if serverErr != nil && termErr != nil && !streamLive {
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
	// Machine names come from `hc ls`; when it fails mid-stream the remote
	// address prefixes ("machine/addr") supply them instead.
	if streamLive {
		merged := append([]string{}, machines...)
		seen := map[string]bool{}
		for _, m := range merged {
			seen[m] = true
		}
		for addr := range records {
			if slash := strings.IndexByte(addr, '/'); slash > 0 {
				if m := addr[:slash]; !seen[m] {
					seen[m] = true
					merged = append(merged, m)
				}
			}
		}
		machines = merged
	}
	snap.Machines = machines
	snap.Spaces = spaces
	snap.LastSuccess = now

	byAddress := map[string]*CoreTerminal{}
	labelsByAddr := map[string]map[string]string{}
	if streamLive {
		for addr, rec := range records {
			byAddress[addr] = rec
			if len(rec.Labels) > 0 {
				labelsByAddr[addr] = rec.Labels
			}
		}
		terminals = terminals[:0]
		for _, rec := range records {
			terminals = append(terminals, *rec)
		}
		sort.Slice(terminals, func(i, j int) bool { return terminals[i].Address < terminals[j].Address })
	} else {
		for i := range terminals {
			byAddress[terminals[i].Address] = &terminals[i]
		}
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
	// The app view mirrors what the window mounts. With the window
	// unreachable, mount state comes from labels: terminals carrying
	// herdr.space/ofm.space are regrouped below; anything else live in
	// the core (hc's own add-* jobs, stray shells) is invisible in the
	// app, so it stays off the phone too.
	if topoErr != nil {
		for i := range terminals {
			t := &terminals[i]
			if seen[t.Address] || t.State != "live" {
				continue
			}
			labels := labelsByAddr[t.Address]
			if labels == nil {
				if full := byAddress[t.Address]; full != nil {
					labels = full.Labels
				}
			}
			if labels["herdr.space"] == "" && labels["ofm.space"] == "" {
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
	// The events stream already delivers full records; `hc get` is only the
	// fallback — for terminals missing from the stream snapshot, marked
	// dirty by a non-title event, or every poll when the stream is down
	// (bounded, since ls carries no status).
	const getLimit = 24
	fetched := 0
	for _, a := range agents {
		full := byAddress[a.TerminalID]
		needsGet := full == nil || dirty[a.TerminalID] || !streamLive
		if needsGet {
			if fetched >= getLimit {
				continue
			}
			fetched++
			got, err := w.client.Get(ctx, a.TerminalID)
			if err != nil {
				continue
			}
			full = got
		}
		if full == nil {
			continue
		}
		if len(full.Labels) > 0 {
			labelsByAddr[full.Address] = full.Labels
		}
		if full.Status.State != "" {
			a.Status = full.Status.State
		} else if a.Status == "idle" && BusyTitle(full.Title) {
			a.Status = "working"
		}
		if full.Title != "" && a.TabLabel == "" {
			a.TabLabel = full.Title
		}
		// Role/model/effort chips: launch argv flags first (`--model
		// pi/<role>` covers the role), then the OMP session JSONL — window
		// tabs launch `omp` bare with only --session-dir, so the JSONL's
		// model_change/thinking_level_change entries are the only source
		// of the model/effort a flag-less launch is running.
		argv := full.AgentArgv()
		role, model, effort := AgentFlags(argv)
		if a.Agent == "omp" {
			// Window tabs launch `omp` bare — no --session-dir in argv —
			// so resolve the dir from the foreground process (lsof on
			// the live pid, else the cwd→slug convention) before giving
			// up on the JSONL scan.
			dir := SessionDir(argv)
			if dir == "" {
				dir = w.agentSessionDirFor(ctx, a.TerminalID, full)
			}
			if dir != "" {
				meta := w.sessionMetaFor(ctx, a.TerminalID, dir)
				if meta.roleLive {
					role = meta.role
				}
				if meta.model != "" {
					model = meta.model
				}
				if meta.effort != "" {
					effort = meta.effort
				}
			}
			if role == "" {
				role = "default"
			}
		}
		a.AgentRole = role
		a.AgentModel = model
		a.AgentEffort = effort
	}

	// Re-group label-carrying terminals into their real spaces when the
	// window is unreachable (they were created as per-terminal core
	// cards). herdr.space/herdr.tab come from the window when the space
	// was mounted; `where` ("<space> / <tab>") supplies names.
	if topoErr != nil {
		snap.Spaces = regroupLabeledTerminals(snap.Spaces, agents, labelsByAddr)
	}
	// Idle omp-shaped terminals might be sitting on a question/approval —
	// hc reports no attention state, so the same parser the legacy lane
	// runs over pane content classifies what the screen is asking for.
	// Reads are bounded per poll and only idle terminals pay for one;
	// a working terminal is by definition not waiting on input.
	const questionLimit = 8
	interrogated := 0
	for _, a := range agents {
		if interrogated >= questionLimit {
			break
		}
		if a.Status != "idle" && a.Status != "" {
			continue
		}
		if !question.Supports(a.Agent) {
			continue
		}
		interrogated++
		content, err := w.client.Read(ctx, a.TerminalID, 80)
		if err != nil {
			continue
		}
		classification := question.Classify(content, a.Agent)
		switch classification.Kind {
		case question.AttentionQuestion, question.AttentionApproval:
			a.Status = "blocked"
			a.AttentionKind = classification.Kind
			a.Prompt = classification.Prompt
			a.Command = classification.Command
			a.Options = classification.Options
			a.Interaction = classification.Interaction
			a.QuestionLayout = classification.QuestionLayout
			if classification.Interaction != nil {
				a.InteractionID = classification.Interaction.ID
			}
			if classification.Kind == question.AttentionApproval {
				a.ApprovalFingerprint = question.ApprovalFingerprint(classification)
			}
		default:
			a.AttentionKind = classification.Kind
			a.Prompt = classification.Prompt
		}
	}
	snap.Agents = agents
	w.commit(snap)
}

// regroupLabeledTerminals rebuilds window-faithful space/tab groups from
// terminal labels when the app window is unreachable. Terminals carrying
// herdr.space (mounted tabs) or ofm.space (of-machinery lanes) move into
// shared space groups keyed on the space id; tab rows come from
// herdr.tab/ofm.tab + ofm.tab_label, space names from the `where`
// label's left half. Label-less terminals are filtered upstream — the
// app never shows unmounted workers, so the phone does not either.
func regroupLabeledTerminals(
	spaces []Space,
	agents []*coordinator.AgentState,
	labelsByAddr map[string]map[string]string,
) []Space {
	type tabGroup struct {
		label string
		addrs []string
	}
	var spaceOrder []string
	spaceNames := map[string]string{}
	tabsBySpace := map[string]map[string]*tabGroup{}
	relabeled := map[string]bool{}

	for _, a := range agents {
		labels := labelsByAddr[a.TerminalID]
		spaceID := labels["herdr.space"]
		if spaceID == "" {
			// of-machinery lanes mount via ofm.* labels only — group them
			// under the ofm space id so they land beside their window tabs.
			spaceID = labels["ofm.space"]
		}
		if spaceID == "" {
			continue
		}
		if _, ok := tabsBySpace[spaceID]; !ok {
			tabsBySpace[spaceID] = map[string]*tabGroup{}
			spaceOrder = append(spaceOrder, spaceID)
			if where := labels["where"]; where != "" {
				spaceNames[spaceID] = strings.TrimSpace(strings.SplitN(where, " / ", 2)[0])
			}
		}
		tabID := labels["herdr.tab"]
		if tabID == "" {
			tabID = labels["ofm.tab"]
		}
		if tabID == "" {
			tabID = a.TerminalID
		}
		tabLabel := labels["ofm.tab_label"]
		if tabLabel == "" {
			if where := labels["where"]; where != "" {
				if parts := strings.SplitN(where, " / ", 2); len(parts) == 2 {
					tabLabel = strings.TrimSpace(parts[1])
				}
			}
		}
		tg := tabsBySpace[spaceID][tabID]
		if tg == nil {
			tg = &tabGroup{label: tabLabel}
			tabsBySpace[spaceID][tabID] = tg
		}
		if tg.label == "" {
			tg.label = tabLabel
		}
		tg.addrs = append(tg.addrs, a.TerminalID)
		a.WorkspaceID = spaceID
		a.TabID = tabID
		if tabLabel != "" {
			a.TabLabel = tabLabel
		}
		if spaceNames[spaceID] != "" {
			a.SessionName = spaceNames[spaceID]
		}
		relabeled[a.TerminalID] = true
	}
	out := spaces[:0]
	for _, sp := range spaces {
		// The synthetic per-terminal space id is "hc-core:<addr>"; drop the
		// ones whose terminal just gained a real space.
		if strings.HasPrefix(sp.ID, "hc-core:") && relabeled[sp.ID[len("hc-core:"):]] {
			continue
		}
		out = append(out, sp)
	}
	for _, spaceID := range spaceOrder {
		sp := Space{ID: spaceID, Name: spaceNames[spaceID]}
		tabIDs := make([]string, 0, len(tabsBySpace[spaceID]))
		for tabID := range tabsBySpace[spaceID] {
			tabIDs = append(tabIDs, tabID)
		}
		sort.Strings(tabIDs)
		for _, tabID := range tabIDs {
			tg := tabsBySpace[spaceID][tabID]
			tab := Tab{ID: tabID, Name: tg.label}
			for _, addr := range tg.addrs {
				tab.Terminals = append(tab.Terminals, Terminal{Address: addr})
			}
			sp.Tabs = append(sp.Tabs, tab)
		}
		out = append(out, sp)
	}
	return out
}

// AgentFlags extracts --model/--role/--effort (and =forms) from a launch
// argv for the card chips. Only a pi/<role> --model counts as the role —
// a model literal stays in the model slot, matching the legacy scanner.
func AgentFlags(argv []string) (role, model, effort string) {
	take := func(i *int, arg, name string) (string, bool) {
		if arg == name && *i+1 < len(argv) {
			return argv[*i+1], true
		}
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"="), true
		}
		return "", false
	}
	for i, arg := range argv {
		if value, ok := take(&i, arg, "--model"); ok {
			if r, isRole := strings.CutPrefix(value, "pi/"); isRole {
				role = r
			} else {
				model = value
			}
			continue
		}
		if value, ok := take(&i, arg, "--role"); ok && role == "" {
			role = value
			continue
		}
		if value, ok := take(&i, arg, "--effort"); ok {
			effort = value
		}
	}
	return role, model, effort
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
	if status == "" && core != nil {
		status = core.Status.State
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
	if agent == "" && core != nil && core.Status.App != "" {
		agent = core.Status.App
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
