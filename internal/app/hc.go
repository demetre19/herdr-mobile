package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/hc"
	"github.com/0cv/herdr-mobile-relay/internal/history"
	"github.com/0cv/herdr-mobile-relay/internal/protocol"
	"github.com/0cv/herdr-mobile-relay/internal/transport"
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
	s.hub.Broadcast(hcInventoryMessage(s.hcW.Current()))
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
	content, err := s.hcC.Read(ctx, hcAddress(paneID), lines)
	if err != nil {
		return map[string]any{"type": "pane_content", "pane_id": paneID, "content": "", "format": format, "error": "Unable to read the Herdr App terminal"}
	}
	return map[string]any{
		"type": "pane_content", "pane_id": paneID, "content": content,
		"format": format, "truncated": false, "viewport_only": false,
		"interaction": nil, "question_layout": false,
	}
}

// hcSendKeys maps the phone's semantic key names onto `hc keys` arguments.
// Unknown names are passed through — hc keys understands its own vocabulary
// (enter, esc, up, ctrl+c, …).
func hcSendKeyArgs(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "enter", "return":
			out = append(out, "enter")
		case "escape", "esc":
			out = append(out, "esc")
		case "up", "down", "left", "right", "tab", "space", "backspace":
			out = append(out, strings.ToLower(key))
		default:
			if key != "" {
				out = append(out, key)
			}
		}
	}
	return out
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

	switch action {
	case "send_input", "send_text", "submit_prompt", "respond", "answer_question", "send_secret":
		text := inbound.Text
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
	case "clarify_question":
		if strings.TrimSpace(inbound.Text) == "" {
			fail("Nothing to send")
			return
		}
		if err := s.hcC.Send(ctx, address, inbound.Text); err != nil {
			fail(hcSendError(err))
			return
		}
		s.sendCommandResult(client, inbound.RequestID, action, true, "completed", "", inbound.PaneID, nil)
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
