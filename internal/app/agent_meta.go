package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// agentRuntimeMeta carries the OMP runtime identity shown on the phone's agent
// cards: which role and model the session is actually running, plus the
// thinking effort. Herdr's pane payload does not expose them; the OMP session
// JSONL records model/effort on model_change / thinking_level_change entries,
// and the role only when the model is switched mid-session (Ctrl+P) — a
// pane running its launch role has no role entry at all. That gap is filled
// from the agent process's own launch argv (`--model pi/<role>`, the only
// form the prd-herdr spawn contract allows), passed in as launchArgv.
//
// Those entries are sparse — an 8MB transcript may hold a single model_change
// near the head — so instead of tailing a fixed window the resolver scans each
// file once, then incrementally reads only appended bytes (keyed on the last
// complete-line offset). Fresh sessions resolve immediately; long ones cost
// one full scan per relay lifetime plus a few KB per poll.
type agentRuntimeMeta struct {
	sessionPath string
	modTime     time.Time
	size        int64
	offset      int64
	role        string
	model       string
	effort      string
	// launchRole is the `--model pi/<role>` the pane's agent process
	// started with. It only fills the display role while the JSONL
	// records none; a mid-session model_change role always wins.
	launchRole string
	// roleLive marks role as JSONL-recorded (not the fallback), so a
	// resumed cache entry knows whether role is authoritative or must
	// be recomputed from launchRole on the next file change.
	roleLive bool
}

// sessionMetaEntry is the subset of a session entry needed to track what the
// pane is running. role is present on model_change only when the model is
// switched mid-session; the opening launch entry never carries it.
type sessionMetaEntry struct {
	Type  string `json:"type"`
	Role  string `json:"role"`
	Model string `json:"model"`
	// thinking_level_change carries effort under thinkingLevel.
	ThinkingLevel string `json:"thinkingLevel"`
}

const agentMetaReadLimit = 64 << 20

// launchRoleFromArgv recovers the role from an omp launch command line
// (`--model pi/<role>`, the only form the prd-herdr spawn contract allows).
// A value without the pi/ role-selector prefix is a model literal, not a
// role, and is ignored. A bare `omp` / `--resume` / `--continue` launch
// carries no selector, so the profile default stands.
func launchRoleFromArgv(argv []string) string {
	for i, arg := range argv {
		value := ""
		switch {
		case arg == "--model" && i+1 < len(argv):
			value = argv[i+1]
		case strings.HasPrefix(arg, "--model="):
			value = strings.TrimPrefix(arg, "--model=")
		default:
			continue
		}
		// First --model wins: omp takes the flag once, repeats are noise.
		if role, ok := strings.CutPrefix(value, "pi/"); ok && role != "" {
			return role
		}
		return ""
	}
	return ""
}

// launchArgvForPane returns the argv of the pane's omp agent process, or
// nil when the pane has no live omp foreground process (finished pane,
// non-omp agent, process-info unavailable). It mirrors the panesize
// manager's resolution order: the foreground group leader first, then any
// omp-named foreground process.
func launchArgvForPane(ctx context.Context, reader paneProcessReader, paneID string) []string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	info, err := reader.PaneProcessInfo(ctx, paneID)
	if err != nil || info == nil || info.PaneID != paneID {
		return nil
	}
	var fallback []string
	for _, process := range info.ForegroundProcesses {
		if process.Name != "omp" && process.Name != "pi" {
			continue
		}
		if len(process.Argv) == 0 {
			continue
		}
		if process.PID == info.ForegroundProcessGroupID {
			return process.Argv
		}
		if fallback == nil {
			fallback = process.Argv
		}
	}
	return fallback
}

// resolveAgentRuntimeMeta fills agent_role/agent_model/agent_effort for OMP
// panes. Results are cached per pane keyed on (path, mtime); unchanged files
// never get re-read, changed files only parse appended bytes. A JSONL
// model_change role (mid-session switch) always wins; when the JSONL
// records no role the pane's launch argv (`--model pi/<role>`) fills it,
// defaulting to "default" for bare/resumed launches.
//
// ctx carries the poll deadline; process-info is only spawned while the
// JSONL shows a session with no recorded role yet, so panes that already
// resolved (or switched mid-session) cost no subprocess.
func (s *Server) resolveAgentRuntimeMeta(ctx context.Context, agentAgent string, paneID, sessionPath string) agentRuntimeMeta {
	if agentAgent != "omp" || !strings.HasSuffix(sessionPath, ".jsonl") {
		return agentRuntimeMeta{}
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		return agentRuntimeMeta{}
	}
	s.metaMu.Lock()
	cached, ok := s.metaCache[paneID]
	s.metaMu.Unlock()
	if ok && cached.sessionPath == sessionPath && cached.modTime.Equal(info.ModTime()) {
		return cached
	}
	offset := int64(0)
	role, model, effort, launchRole := "", "", "", ""
	roleLive := false
	if ok && cached.sessionPath == sessionPath && info.Size() >= cached.offset {
		// Same session, new bytes: resume where the last full line ended.
		// Only the JSONL-recorded role resumes; a cached fallback display
		// is recomputed so a failed launch lookup retries instead of
		// wedging.
		offset, model, effort = cached.offset, cached.model, cached.effort
		launchRole = cached.launchRole
		if cached.roleLive {
			role, roleLive = cached.role, true
		}
	}
	newOffset, gotRole, gotModel, gotEffort, okRead := readSessionMetaDelta(sessionPath, offset)
	if !okRead {
		return agentRuntimeMeta{}
	}
	if gotRole != "" {
		role, roleLive = gotRole, true
	}
	if gotModel != "" {
		model = gotModel
	}
	if gotEffort != "" {
		effort = gotEffort
	}
	// The launch role is process state, not file state: look it up only
	// while the JSONL records no role, and cache the answer alongside
	// the file offset so a later model_change still overrides it.
	if !roleLive && launchRole == "" && ctx != nil && s.herdrC != nil {
		launchRole = launchRoleFromArgv(launchArgvForPane(ctx, s.herdrC, paneID))
	}
	displayRole := role
	if displayRole == "" {
		displayRole = launchRole
	}
	if displayRole == "" {
		displayRole = "default"
	}
	meta := agentRuntimeMeta{
		sessionPath: sessionPath, modTime: info.ModTime(), size: info.Size(),
		offset: newOffset, role: displayRole, model: model, effort: effort,
		launchRole: launchRole, roleLive: roleLive,
	}
	s.metaMu.Lock()
	if s.metaCache == nil {
		s.metaCache = make(map[string]agentRuntimeMeta)
	}
	s.metaCache[paneID] = meta
	s.metaMu.Unlock()
	return meta
}

// readSessionMetaDelta scans the session file from `offset` and returns the
// latest meta values found after it plus the end offset of the last complete
// line (so the next read starts on a line boundary). offset==0 scans the
// whole file once. A partial trailing line is left for the next pass.
func readSessionMetaDelta(path string, offset int64) (endOffset int64, role, model, effort string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", "", "", false
	}
	defer f.Close()
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return 0, "", "", "", false
	}
	reader := bufio.NewReaderSize(f, 256<<10)
	endOffset = offset
	for endOffset < agentMetaReadLimit {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Only count the line once it's newline-terminated; a partial tail
			// line belongs to the next mtime cycle.
			if line[len(line)-1] == '\n' {
				endOffset += int64(len(line))
				var entry sessionMetaEntry
				if json.Unmarshal(bytes.TrimSpace(line), &entry) == nil {
					switch entry.Type {
					case "model_change":
						role, model = entry.Role, entry.Model
					case "thinking_level_change":
						effort = entry.ThinkingLevel
					}
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return endOffset, role, model, effort, true
}
