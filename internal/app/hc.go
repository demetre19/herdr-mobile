package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/hc"
	"github.com/0cv/herdr-mobile-relay/internal/history"
	"github.com/0cv/herdr-mobile-relay/internal/protocol"
	"github.com/0cv/herdr-mobile-relay/internal/question"
	"github.com/0cv/herdr-mobile-relay/internal/push"
	"github.com/0cv/herdr-mobile-relay/internal/slashcmd"
	"github.com/0cv/herdr-mobile-relay/internal/transport"
	"github.com/0cv/herdr-mobile-relay/internal/workspace"
)

// This file is the Herdr App (herdr-app/herdr-core, `hc`) lane: pane ids
// carrying the hc: prefix are answered from the hc watcher + hc CLI instead of
// the legacy herdr session, so the phone sees the new app's spaces/tabs/
// terminals beside the legacy inventory.

func isHcPaneID(paneID string) bool {
	return strings.HasPrefix(paneID, hc.PanePrefix)
}

func hcAddress(paneID string) string {
	return strings.TrimPrefix(paneID, hc.PanePrefix)
}

// hcInventoryMessage is the phone-visible snapshot of the new app's topology.
func hcInventoryMessage(snap hc.Snapshot) map[string]any {
	state := "ready"
	if !snap.Available {
		state = "error"
	}
	spaces := snap.Spaces
	if spaces == nil {
		spaces = []hc.Space{}
	}
	agents := snap.Agents
	if agents == nil {
		agents = []*coordinator.AgentState{}
	}
	return map[string]any{
		"type":            "hc_inventory",
		"state":           state,
		"message":         snap.Message,
		"server_id":       snap.ServerID,
		"machines":        snap.Machines,
		"spaces":          spaces,
		"agents":          agents,
		"last_attempt_at": snap.LastAttempt,
		"last_success_at": snap.LastSuccess,
	}
}

func (s *Server) hcSnapshot() hc.Snapshot {
	if s.hcW == nil {
		return hc.Snapshot{}
	}
	return s.hcW.Current()
}

// hcPayload returns the snapshot body embedded in push_config.hc, or nil when
// the relay does not host the Herdr App (field omits entirely then).
func (s *Server) hcPayload() any {
	if s.hcW == nil {
		return nil
	}
	msg := hcInventoryMessage(s.hcW.Current())
	delete(msg, "type")
	return msg
}

func (s *Server) publishHcInventory() {
	if s.hcW == nil {
		return
	}
	snap := s.hcW.Current()
	s.publishHcTransitions(snap)
	s.hub.Broadcast(hcInventoryMessage(snap))
}

// publishHcTransitions diffs hc agent statuses across snapshots and pushes the
// same attention/finished notifications legacy panes get via SetOnTransition —
// hc agents never enter s.state, so without this their transitions are silent.
func (s *Server) publishHcTransitions(snap hc.Snapshot) {
	s.hcStatusMu.Lock()
	prev := s.hcPrevStatus
	next := make(map[string]string, len(snap.Agents))
	changed := make([]*coordinator.AgentState, 0)
	for _, agent := range snap.Agents {
		if agent == nil || agent.PaneID == "" {
			continue
		}
		next[agent.PaneID] = agent.Status
		if old, ok := prev[agent.PaneID]; ok && old != agent.Status {
			changed = append(changed, agent)
		}
	}
	s.hcPrevStatus = next
	s.hcStatusMu.Unlock()
	if s.pushM == nil || len(prev) == 0 {
		return // no baseline or no push manager — first snapshot never notifies
	}
	ctx := context.Background()
	for _, agent := range changed {
		switch agent.Status {
		case "blocked":
			category := push.CategoryAttention
			preview := push.PreviewHidden
			if agent.AttentionKind == question.AttentionQuestion {
				category = push.CategoryQuestion
				preview = push.PreviewQuestion
			}
			s.publishAgentPush(ctx, agent,
				fmt.Sprintf("hc-attn-%d", time.Now().UnixNano()), 0, category, preview)
		default:
			if agent.Status == "done" || agent.Status == "finished" {
				s.publishAgentPush(ctx, agent,
					fmt.Sprintf("hc-finished-%d", time.Now().UnixNano()), 0,
					push.CategoryFinished, push.PreviewBrief)
			}
			// Leaving blocked → retract the outstanding attention push.
			if err := s.pushM.ResolvePaneID(ctx, agent.PaneID, ""); err != nil {
				s.logger.Warn("hc push retraction failed", "pane_id", agent.PaneID, "error", err)
			}
		}
	}
}

// validateHcPaneTarget mirrors validateExactPaneTarget for the hc lane: the
// pane must still exist in the hc snapshot and the client-supplied target
// tuple must match exactly, so a recycled terminal can never swallow input
// meant for its predecessor.
func (s *Server) validateHcPaneTarget(inbound protocol.Inbound, authenticated bool) *protocol.ApiError {
	if inbound.PaneID == "" || !isHcPaneID(inbound.PaneID) {
		return nil
	}
	target := inbound.Target
	if inbound.Type == "unwatch_pane" || inbound.Type == "release_pane_size" || inbound.Type == "cancel_speech" {
		if target != nil && target.PaneID != inbound.PaneID {
			apiErr := protocol.NewApiError(protocol.ErrorInvalidRequest, map[string]any{"field": "target.pane_id"})
			return &apiErr
		}
		return nil
	}
	if target == nil {
		if !authenticated {
			return nil
		}
		apiErr := protocol.NewApiError(protocol.ErrorInvalidRequest, map[string]any{"field": "target"})
		return &apiErr
	}
	if target.PaneID != inbound.PaneID {
		apiErr := protocol.NewApiError(protocol.ErrorInvalidRequest, map[string]any{"field": "target.pane_id"})
		return &apiErr
	}
	agent, ok := s.hcAgent(inbound.PaneID)
	if !ok {
		apiErr := protocol.NewApiError(protocol.ErrorInvalidRequest, map[string]any{"field": "target"})
		return &apiErr
	}
	if target.TerminalID == "" || target.TerminalID != agent.TerminalID ||
		target.Generation != agent.Generation {
		apiErr := protocol.NewApiError(protocol.ErrorInvalidRequest, map[string]any{"field": "target"})
		return &apiErr
	}
	return nil
}

func (s *Server) hcAgent(paneID string) (*coordinator.AgentState, bool) {
	if s.hcW == nil {
		return nil, false
	}
	return s.hcW.Agent(paneID)
}

// hcReadPane emits the same pane_content shape as dispatcher.HandleReadPane so
// terminalFrames, pane watchers and TerminalView work unchanged. hc read
// output is already ANSI-stripped plain text.
func (s *Server) hcReadPane(ctx context.Context, message map[string]any) map[string]any {
	paneID, _ := message["pane_id"].(string)
	format, _ := message["format"].(string)
	if format != "ansi" {
		format = "text"
	}
	if paneID == "" || s.hcC == nil {
		return map[string]any{"type": "pane_content", "pane_id": paneID, "content": "", "format": format, "error": "Herdr App is unavailable"}
	}
	lines := messageInt(message["lines"], 30)
	if lines < 1 {
		lines = 1
	}
	if lines > 500 {
		lines = 500
	}
	var content string
	var err error
	if format == "ansi" && !strings.Contains(hcAddress(paneID), "/") {
		content, err = s.hcStyledContent(ctx, hcAddress(paneID), lines)
	} else {
		// Remote-machine addresses (machine/addr) attach over federation —
		// the observe replay bursts for seconds before first paint, so the
		// fast plain read is the right frame there.
		content, err = s.hcC.Read(ctx, hcAddress(paneID), lines)
	}
	if err != nil {
		return map[string]any{"type": "pane_content", "pane_id": paneID, "content": "", "format": format, "error": "Unable to read the Herdr App terminal"}
	}
	return map[string]any{
		"type": "pane_content", "pane_id": paneID, "content": content,
		"format": format, "truncated": false, "viewport_only": false,
		"interaction": nil, "question_layout": false,
	}
}

// hcStyledFrame caches one attach-collected ANSI frame per terminal. `hc
// attach --observe` replays the styled screen then streams live output, so a
// full collect costs ~1.5s of quiet detection — too expensive per watch tick
// (250ms+). A fresh-enough frame is served verbatim; TerminalView's keyed-row
// splice keeps the repaint stable anyway.
type hcStyledFrame struct {
	content  string
	captured time.Time
}

const hcStyledFreshness = 1500 * time.Millisecond

func (s *Server) hcStyledContent(ctx context.Context, address string, lines int) (string, error) {
	s.hcStyledMu.Lock()
	if s.hcStyledCache == nil {
		s.hcStyledCache = make(map[string]hcStyledFrame)
	}
	cached, ok := s.hcStyledCache[address]
	s.hcStyledMu.Unlock()
	if ok && time.Since(cached.captured) < hcStyledFreshness {
		return cached.content, nil
	}
	raw, err := s.hcC.ReadStyled(ctx, address, lines)
	if err != nil {
		// A dead/finished terminal falls back to the plain-text read — a
		// monochrome frame beats an error toast for a pane that is simply
		// finished.
		fallback, ferr := s.hcC.Read(ctx, address, lines)
		if ferr != nil {
			return "", err
		}
		return fallback, nil
	}
	// The attach stream is a raw VT replay — cursor addressing, mode
	// sets, OSC links, probes — and the phone's ansi parser only renders
	// SGR. Render the replay into the final screen first (same contract
	// as `pane read --format ansi`), with the terminal's live dimensions
	// so rows land on their real wrap boundaries.
	var cols, rows int
	if term, gerr := s.hcC.Get(ctx, address); gerr == nil && term != nil {
		cols, rows = term.Cols, term.Rows
	}
	content := hc.RenderVT(raw, cols, rows, lines)
	s.hcStyledMu.Lock()
	if s.hcStyledCache == nil {
		s.hcStyledCache = make(map[string]hcStyledFrame)
	}
	s.hcStyledCache[address] = hcStyledFrame{content: content, captured: time.Now()}
	s.hcStyledMu.Unlock()
	return content, nil
}

// hcSendKeys maps the phone's semantic key names onto `hc keys` arguments.
// Unknown names are passed through — hc keys understands its own vocabulary
// (enter, esc, up, ctrl+c, …).
func hcSendKeyArgs(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		mapped := strings.ToLower(strings.TrimSpace(key))
		switch mapped {
		case "enter", "return":
			mapped = "enter"
		case "escape", "esc":
			mapped = "esc"
		case "up", "down", "left", "right", "tab", "space", "backspace":
			// already hc vocabulary
		default:
			// PlanInput emits "Shift+Tab"/"Ctrl+U"-style combos; hc keys
			// takes the same names lowercase ("shift+tab", "ctrl+u").
		}
		out = append(out, mapped)
	}
	return out
}

// hcQuestionKeys executes a PlanInput sequence on an hc terminal: key
// steps go through hc keys one press at a time (the legacy dispatcher's
// 150ms pacing applies), text steps type without submitting — question
// UIs would treat a pasted+submitted string as a finished answer.
func (s *Server) hcQuestionKeys(ctx context.Context, address string, steps []question.InputStep) error {
	for index, step := range steps {
		if len(step.Keys) > 0 {
			for _, key := range hcSendKeyArgs(step.Keys) {
				if err := s.hcC.Keys(ctx, address, []string{key}); err != nil {
					if index > 0 {
						return fmt.Errorf("question input was only partially applied: %w", err)
					}
					return err
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(150 * time.Millisecond):
				}
			}
		}
		if step.Text != "" {
			if err := s.hcC.TypeText(ctx, address, step.Text); err != nil {
				return fmt.Errorf("question input was only partially applied: %w", err)
			}
		}
	}
	return nil
}

// hcQuestionInteraction reads a terminal and parses its live question
// surface; the result must match the interaction the phone thinks it is
// answering before any key is sent.
func (s *Server) hcQuestionInteraction(ctx context.Context, address, agent string) (*question.Interaction, error) {
	content, err := s.hcC.Read(ctx, address, 80)
	if err != nil {
		return nil, err
	}
	return question.Parse(content, agent), nil
}

// dispatchHcCommand handles pane commands whose pane_id is namespaced hc:.
// Callers only invoke it for hc: ids, so every action is answered here.
func (s *Server) dispatchHcCommand(ctx context.Context, client *transport.ClientConn, inbound protocol.Inbound, action string, msg map[string]any) {
	fail := func(text string) {
		s.sendCommandResult(client, inbound.RequestID, action, false, "failed", text, inbound.PaneID, nil)
	}

	// Frame commands reuse the legacy pane-content machinery so the phone's
	// terminalFrames cache and TerminalView need no hc awareness.
	switch action {
	case "read_pane":
		s.applyPaneReadLease(msg)
		s.stopPaneWatch(client.ID(), inbound.PaneID)
		resp := s.preparePaneResponse(msg, s.hcReadPane(ctx, msg))
		if inbound.Target != nil {
			resp["target"] = *inbound.Target
		}
		if unchanged := unchangedPaneResponse(msg, resp); unchanged != nil {
			s.hub.Send(client, unchanged)
			return
		}
		s.hub.Send(client, resp)
		return
	case "watch_pane":
		s.startPaneWatch(client, msg)
		return
	case "unwatch_pane":
		s.stopPaneWatch(client.ID(), inbound.PaneID)
		return
	case "pane_applied":
		s.handlePaneApplied(client, msg)
		return
	}

	if s.hcC == nil {
		fail("Herdr App is unavailable on this relay")
		return
	}
	address := hcAddress(inbound.PaneID)
	// Attachment references in the composer arrive as "Attachment: <ref>"
	// lines; the staged object must be re-pathed beside the terminal
	// (`hc put` handles remote machines) before the text is sent — the
	// shared expander never runs for hc: panes because they dispatch here.
	if action == "send_input" || action == "send_text" || action == "submit_prompt" {
		if err := s.expandPromptAttachmentReferences(action, msg, inbound.Target); err != nil {
			fail("One or more attachments are no longer available for this agent")
			return
		}
		if text, ok := msg["text"].(string); ok {
			inbound.Text = text
		}
		if prompt, ok := msg["prompt"].(string); ok {
			inbound.Prompt = prompt
		}
	}

	switch action {
	case "send_input", "send_text", "submit_prompt":
		text := inbound.Text
		if text == "" {
			text = inbound.Prompt
		}
		if text == "" {
			text = inbound.Choice
		}
		if text == "" && inbound.OtherText != "" {
			text = inbound.OtherText
		}
		if strings.TrimSpace(text) == "" {
			fail("Nothing to send")
			return
		}
		if err := s.hcC.Send(ctx, address, text); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
	case "send_secret":
		// No-echo prompts read single runes; a paste+enter wraps the
		// secret in bracketed-paste markers a noecho read echoes raw.
		if inbound.Text == "" {
			fail("Nothing to send")
			return
		}
		if err := s.hcC.TypeText(ctx, address, inbound.Text); err != nil {
			fail(hcSendError(err))
			return
		}
		if err := s.hcC.Keys(ctx, address, []string{"enter"}); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
	case "respond":
		// Approval menus are arrow-navigated, not typed. Verify the live
		// screen still shows the approval the phone confirmed (kind,
		// option count, label at index, fingerprint), then send the same
		// Up/Down+Enter sequence the legacy dispatcher derives.
		agent, ok := s.hcAgent(inbound.PaneID)
		if !ok {
			fail("That terminal no longer exists")
			return
		}
		content, err := s.hcC.Read(ctx, address, 80)
		if err != nil {
			fail("Unable to read the Herdr App terminal")
			return
		}
		classification := question.Classify(content, agent.Agent)
		if classification.Kind != question.AttentionApproval || len(classification.Options) == 0 {
			fail("This approval request is no longer current")
			return
		}
		index := 0
		if inbound.Index != nil {
			index = *inbound.Index
		}
		if index < 0 || index >= len(classification.Options) ||
			(inbound.Choice != "" && classification.Options[index] != inbound.Choice) {
			fail("Approval choices are no longer available")
			return
		}
		if inbound.ApprovalFingerprint != "" &&
			question.ApprovalFingerprint(classification) != inbound.ApprovalFingerprint {
			fail("Approval choices are no longer available")
			return
		}
		distance := index - classification.ApprovalFocus
		key := "down"
		if distance < 0 {
			key = "up"
			distance = -distance
		}
		keys := make([]string, 0, distance+1)
		for range distance {
			keys = append(keys, key)
		}
		keys = append(keys, "enter")
		if err := s.hcC.Keys(ctx, address, keys); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "accepted", "", inbound.PaneID, nil)
	case "send_keys":
		keys := hcSendKeyArgs(inbound.Keys)
		if len(keys) == 0 {
			fail("No keys to send")
			return
		}
		if err := s.hcC.Keys(ctx, address, keys); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
	case "agent_clear":
		// ctrl+c interrupts whatever is running; that is the closest hc
		// equivalent to clearing the composer/turn.
		if err := s.hcC.Keys(ctx, address, []string{"ctrl+c"}); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
	case "lease_pane_size":
		// Herdr App panes are resized by the window, not the relay. Report a
		// successful no-op lease carrying the requested columns so the wide
		// view's 240-column canvas renders instead of an error loop.
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "",
			inbound.PaneID, map[string]any{"columns": inbound.Columns, "rows": inbound.Rows, "noop": true})
	case "release_pane_size":
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
	case "copy_agent_response":
		content, err := s.hcC.Read(ctx, address, history.MaxLines)
		if err != nil {
			fail("Unable to read the Herdr App terminal")
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "",
			inbound.PaneID, map[string]any{"text": content})
	case "agent_stop":
		// `hc kill` ends the program on any machine the federation serves.
		if err := s.hcC.Kill(ctx, address); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
	case "agent_rename":
		// The tab name lives in the window (`herdr-app do tab.rename`),
		// not on the terminal — agent.TabID carries the tab id.
		name := inbound.Name
		if name == "" {
			name = inbound.Label
		}
		if strings.TrimSpace(name) == "" {
			fail("Nothing to rename to")
			return
		}
		agent, ok := s.hcAgent(inbound.PaneID)
		if !ok || agent.TabID == "" {
			fail("That terminal is not mounted in the Herdr App window")
			return
		}
		if err := s.hcC.TabRename(ctx, agent.TabID, name); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
	case "workspace_close":
		// Closing an hc space == `herdr-app do space.close --confirmed`
		// (the phone's dialog already confirms; skip the window's own ask).
		// Core-terminal groups have no space object to close — they hold
		// only unmounted terminals, which are closed individually.
		agent, ok := s.hcAgent(inbound.PaneID)
		if !ok {
			fail("That space no longer exists")
			return
		}
		if agent.WorkspaceID == "" || strings.HasPrefix(agent.WorkspaceID, "hc-core:") {
			fail("Core terminals are stopped individually")
			return
		}
		if err := s.hcC.Do(ctx, "space.close", "--id", agent.WorkspaceID, "--confirmed"); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
	case "answer_question", "navigate_question", "clarify_question":
		// Same flow as the legacy lane: read → parse → verify the
		// interaction the phone answered is still on screen → PlanInput
		// keys/text, paced. clarify passes its free text as OtherText.
		agent, ok := s.hcAgent(inbound.PaneID)
		if !ok {
			fail("That terminal no longer exists")
			return
		}
		interaction, err := s.hcQuestionInteraction(ctx, address, agent.Agent)
		if err != nil {
			fail("Unable to read the Herdr App terminal")
			return
		}
		if interaction == nil || (inbound.InteractionID != "" && interaction.ID != inbound.InteractionID) {
			fail("The question changed before the answer was applied")
			return
		}
		intent := question.InputIntent{
			Navigation:    inbound.Direction,
			Selected:      inbound.SelectedIndices,
			OtherSelected: inbound.OtherSelected,
			OtherText:     inbound.OtherText,
		}
		if action == "clarify_question" {
			intent.Clarify = true
			if inbound.OtherText == "" {
				intent.OtherText = inbound.Text
			}
		}
		steps := question.PlanInput(interaction, intent)
		if len(steps) == 0 {
			fail("Nothing to send")
			return
		}
		if err := s.hcQuestionKeys(ctx, address, steps); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "accepted", "", inbound.PaneID, nil)
	case "send_filter_text":
		// Type into a live question's filter without submitting.
		if strings.TrimSpace(inbound.Text) == "" {
			fail("Nothing to send")
			return
		}
		if err := s.hcC.TypeText(ctx, address, inbound.Text); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "accepted", "", inbound.PaneID, nil)
	case "workspace_tree", "workspace_file", "workspace_git_status", "workspace_git_diff", "workspace_file_write":
		// Local-machine terminals share the relay's filesystem, so the
		// same inspector the legacy lane serves applies verbatim.
		// Federation addresses (machine/addr) are remote — hc file/exec
		// could reach them, but the wire shapes differ; answered plainly.
		if strings.Contains(address, "/") {
			fail("Workspace inspection is only available for terminals on this machine")
			return
		}
		agent, ok := s.hcAgent(inbound.PaneID)
		if !ok {
			fail("That terminal no longer exists")
			return
		}
		cwd := strings.TrimSpace(agent.Cwd)
		var data any
		var inspectErr error
		switch action {
		case "workspace_tree":
			data, inspectErr = workspace.TreeFor(cwd)
		case "workspace_file":
			data, inspectErr = workspace.ReadFile(cwd, inbound.Path)
		case "workspace_git_status":
			data, inspectErr = workspace.GitStatusFor(ctx, cwd)
		case "workspace_git_diff":
			data, inspectErr = workspace.GitDiffFor(ctx, cwd, inbound.Path)
		case "workspace_file_write":
			data, inspectErr = workspace.WriteFile(cwd, inbound.Path, inbound.Text)
		}
		if inspectErr != nil {
			var public *workspace.Error
			message := "Workspace inspection failed"
			if errors.As(inspectErr, &public) {
				message = public.Message
			}
			fail(message)
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, data)
	case "list_slash_commands":
		// The catalog is agent-name + cwd driven; a hc terminal's agent
		// resolves through the reported-agent fallback (ResolvePane keys
		// on legacy panes only). Runtime pi commands need the herdr
		// socket, which does not exist on this lane — the static
		// filesystem catalog still matches.
		agent, ok := s.hcAgent(inbound.PaneID)
		if !ok {
			fail("That terminal no longer exists")
			return
		}
		agentName, cwd := agent.Agent, agent.Cwd
		home, _ := os.UserHomeDir()
		skillDirs, commandFormat, suppressNative := s.profiles.CommandDiscovery("")
		catalog := slashcmd.CatalogForProfileWithSuppression(
			"", agentName, cwd, home, skillDirs, commandFormat, "", "", suppressNative,
		)
		catalog = fitSlashCommandCatalog(slashcmd.Revise(catalog), inbound.RequestID, "list_slash_commands", inbound.PaneID)
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, catalog)
	default:
		fail(fmt.Sprintf("%s is not supported for Herdr App terminals", action))
	}
	// Unreachable: every case above returns or answers via sendCommandResult.
}

// hcSendError normalizes hc send/keys failures into phone-readable text. A
// context deadline means hc waited out the program's busy pause.
func hcSendError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "timed out") {
		return "The terminal is busy right now"
	}
	if idx := strings.LastIndex(msg, ": "); idx >= 0 {
		return strings.TrimSpace(msg[idx+2:])
	}
	return msg
}

// handleHcLaunch mounts a new tab + terminal in the open Herdr window.
// profile_id resolves to the profile's argv (same catalog as agent_start);
// name/folder/prompt map to tab.new's name/folder/task. Requires the window:
// when herdr-app is closed the phone gets a readable failure instead of a
// terminal that lives invisibly under "Core terminals".
func (s *Server) handleHcLaunch(ctx context.Context, client *transport.ClientConn, inbound protocol.Inbound, msg map[string]any) {
	fail := func(text string) {
		s.sendCommandResult(client, inbound.RequestID, "hc_launch", false, "failed", text, "", nil)
	}
	if s.hcC == nil {
		fail("Herdr App is unavailable on this relay")
		return
	}
	profileID, _ := msg["profile_id"].(string)
	name, _ := msg["name"].(string)
	folder, _ := msg["cwd"].(string)
	task, _ := msg["prompt"].(string)
	if profileID == "" || folder == "" {
		fail("Profile and working directory are required")
		return
	}
	profile, exists := s.profiles.Profile(profileID)
	if !exists || len(profile.Argv) == 0 {
		fail("That agent profile is unavailable on this relay")
		return
	}
	spaceID := hc.LocalSpaceID(s.hcSnapshot().Spaces)
	if err := s.hcC.TabNew(ctx, spaceID, name, folder, profile.Argv, task); err != nil {
		fail(hcSendError(err))
		return
	}
	s.sendCommandResult(client, inbound.RequestID, "hc_launch", true, "completed", "", "",
		map[string]any{"name": name, "cwd": folder})
}
