package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// agentRuntimeMeta carries the OMP runtime identity shown on the phone's agent
// cards: which role and model the session is actually running, plus the
// thinking effort. Herdr's pane payload does not expose them; the OMP session
// JSONL records them on model_change / thinking_level_change entries.
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
}

// sessionMetaEntry is the subset of a session entry needed to track what the
// pane is running. role is absent on the opening model_change of older
// sessions — that means the default role.
type sessionMetaEntry struct {
	Type  string `json:"type"`
	Role  string `json:"role"`
	Model string `json:"model"`
	// thinking_level_change carries effort under thinkingLevel.
	ThinkingLevel string `json:"thinkingLevel"`
}

const agentMetaReadLimit = 64 << 20

// resolveAgentRuntimeMeta fills agent_role/agent_model/agent_effort for OMP
// panes. Results are cached per pane keyed on (path, mtime); unchanged files
// never get re-read, changed files only parse appended bytes.
func (s *Server) resolveAgentRuntimeMeta(agentAgent string, paneID, sessionPath string) agentRuntimeMeta {
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
	role, model, effort := "", "", ""
	if ok && cached.sessionPath == sessionPath && info.Size() >= cached.offset {
		// Same session, new bytes: resume where the last full line ended.
		offset, role, model, effort = cached.offset, cached.role, cached.model, cached.effort
	}
	newOffset, gotRole, gotModel, gotEffort, okRead := readSessionMetaDelta(sessionPath, offset)
	if !okRead {
		return agentRuntimeMeta{}
	}
	if gotRole != "" {
		role = gotRole
	}
	if gotModel != "" {
		model = gotModel
	}
	if gotEffort != "" {
		effort = gotEffort
	}
	meta := agentRuntimeMeta{
		sessionPath: sessionPath, modTime: info.ModTime(), size: info.Size(),
		offset: newOffset, role: role, model: model, effort: effort,
	}
	if meta.role == "" {
		meta.role = "default"
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
