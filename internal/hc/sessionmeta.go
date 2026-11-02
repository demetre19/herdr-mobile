package hc

// Role/model/effort chips for hc terminals mirror the legacy lane's OMP
// session-JSONL scan: window tabs launch `omp` WITHOUT --model/--role flags
// (foreground argv is just `omp --session-dir=…`), so flags alone leave
// every card blank. The session JSONL's model_change /
// thinking_level_change entries are the authoritative record — read them.
// Local terminals (bare tN) resolve on this machine with plain file reads;
// remote federation terminals (machine/tN) resolve through
// `hc exec --on <machine>`.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// sessionMetaEntry is the subset of an OMP session record needed for the
// chips — identical shape to the legacy lane's parser. A model_change role
// only appears on a mid-session switch; thinking_level_change carries
// effort as thinkingLevel.
type sessionMetaEntry struct {
	Type          string `json:"type"`
	Role          string `json:"role"`
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinkingLevel"`
}

// SessionDir extracts the `--session-dir` an omp launch was given — the
// only link between a hc terminal record and its session JSONLs.
func SessionDir(argv []string) string {
	for i, arg := range argv {
		switch {
		case arg == "--session-dir" && i+1 < len(argv):
			return argv[i+1]
		case strings.HasPrefix(arg, "--session-dir="):
			return strings.TrimPrefix(arg, "--session-dir=")
		}
	}
	return ""
}

type hcSessionMeta struct {
	role, model, effort string
	roleLive            bool
}

type sessionMetaState struct {
	dir      string
	file     string
	offset   int64
	modTime  time.Time
	probed   time.Time
	meta     hcSessionMeta
	complete bool // remote fetch delivered the file tail
}

// sessionMetaFor returns the latest meta for one terminal's session dir.
// The watcher caches per address; unchanged files never re-read and remote
// resolves are throttled so a 5s poll can't spawn an hc exec per agent.
func (w *Watcher) sessionMetaFor(ctx context.Context, address, dir string) hcSessionMeta {
	state := w.metaStates[address]
	if state != nil && state.dir != dir {
		state = nil
	}
	remote := strings.Contains(address, "/")
	if remote {
		if state != nil && state.complete && time.Since(state.probed) < hcMetaRemoteRefresh {
			return state.meta
		}
		meta, file, ok := w.remoteSessionMeta(ctx, address, dir)
		if ok {
			state = &sessionMetaState{dir: dir, file: file, probed: time.Now(), meta: meta, complete: true}
			if w.metaStates == nil {
				w.metaStates = make(map[string]*sessionMetaState)
			}
			w.metaStates[address] = state
			return meta
		}
		if state == nil {
			state = &sessionMetaState{dir: dir, probed: time.Now()}
			if w.metaStates == nil {
				w.metaStates = make(map[string]*sessionMetaState)
			}
			w.metaStates[address] = state
		}
		return state.meta
	}
	// Local: stat the newest JSONL; incremental read only past the last
	// complete-line offset, exactly like the legacy resolver.
	file, modTime, err := newestSessionFile(dir)
	if err != nil {
		if state != nil {
			return state.meta
		}
		return hcSessionMeta{}
	}
	if state != nil && state.file == file && state.modTime.Equal(modTime) {
		return state.meta
	}
	offset := int64(0)
	meta := hcSessionMeta{}
	if state != nil && state.file == file {
		offset, meta = state.offset, state.meta
	}
	newOffset, got := scanSessionMeta(file, offset, meta)
	state = &sessionMetaState{
		dir: dir, file: file, offset: newOffset, modTime: modTime,
		probed: time.Now(), meta: got, complete: true,
	}
	if w.metaStates == nil {
		w.metaStates = make(map[string]*sessionMetaState)
	}
	w.metaStates[address] = state
	return got
}

// hcMetaRemoteRefresh bounds remote `hc exec` resolves: model switches are
// rare, so a fresh enough answer stands.
const hcMetaRemoteRefresh = 45 * time.Second

// newestSessionFile picks the most recent .jsonl in an omp --session-dir.
// Names are timestamp-prefixed so lexicographic order is chronological.
func newestSessionFile(dir string) (path string, modTime time.Time, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, err
	}
	newest := ""
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if name > newest {
			newest = name
		}
	}
	if newest == "" {
		return "", time.Time{}, fmt.Errorf("no session files in %s", dir)
	}
	path = dir + "/" + newest
	info, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}, err
	}
	return path, info.ModTime(), nil
}

const sessionMetaReadLimit = 64 << 20

// scanSessionMeta walks the session file from offset and folds
// model_change/thinking_level_change entries into meta; a JSONL-recorded
// role overrides argv-derived role (roleLive).
func scanSessionMeta(path string, offset int64, meta hcSessionMeta) (int64, hcSessionMeta) {
	f, err := os.Open(path)
	if err != nil {
		return offset, meta
	}
	defer f.Close()
	if _, err := f.Seek(offset, 0); err != nil {
		return offset, meta
	}
	reader := bufio.NewReaderSize(f, 256<<10)
	for offset < sessionMetaReadLimit {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			offset += int64(len(line))
			meta = foldSessionMetaLine(line, meta)
		}
		if readErr != nil {
			break
		}
	}
	return offset, meta
}

// foldSessionMetaLine applies one complete JSONL line's meta fields. A
// model_change always carries model; role only on a real switch — an empty
// role is a mid-session switch BACK to launch defaults, not "keep the last
// role", so it overwrites like the legacy parser's semantics.
func foldSessionMetaLine(line []byte, meta hcSessionMeta) hcSessionMeta {
	var entry sessionMetaEntry
	if json.Unmarshal(bytes.TrimSpace(line), &entry) != nil {
		return meta
	}
	switch entry.Type {
	case "model_change":
		meta.role, meta.roleLive, meta.model = entry.Role, true, entry.Model
	case "thinking_level_change":
		meta.effort = entry.ThinkingLevel
	}
	return meta
}

// parseSessionMetaBytes folds a fetched file body (or tail) — drops a
// partial leading line when the fetch began mid-file.
func parseSessionMetaBytes(data []byte, tail bool) hcSessionMeta {
	meta := hcSessionMeta{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	first := true
	for scanner.Scan() {
		line := scanner.Bytes()
		if first && tail && len(data) >= hcRemoteMetaTailBytes {
			first = false
			continue // partial head line of a tail slice
		}
		first = false
		meta = foldSessionMetaLine(append(line, '\n'), meta)
	}
	return meta
}

const hcRemoteMetaTailBytes = 192 << 10

// remoteSessionMeta resolves the session tail on the terminal's own
// machine through `hc exec --on` — one shell round-trip that names the
// newest JSONL and streams its tail.
func (w *Watcher) remoteSessionMeta(ctx context.Context, address, dir string) (hcSessionMeta, string, bool) {
	machine, _, ok := strings.Cut(address, "/")
	if !ok {
		return hcSessionMeta{}, "", false
	}
	script := `f=$(ls -t "$1"/*.jsonl 2>/dev/null | head -n 1); ` +
		`if [ -z "$f" ]; then exit 3; fi; ` +
		`printf '__FILE__ %s\n' "$f"; tail -c ` + fmt.Sprint(hcRemoteMetaTailBytes) + ` "$f"`
	out, err := w.client.execOn(ctx, machine, "/bin/sh", "-c", script, "sh", dir)
	if err != nil {
		return hcSessionMeta{}, "", false
	}
	text := string(out)
	marker := "__FILE__ "
	idx := strings.Index(text, marker)
	if idx < 0 {
		return hcSessionMeta{}, "", false
	}
	rest := text[idx+len(marker):]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return hcSessionMeta{}, "", false
	}
	file := strings.TrimSpace(rest[:nl])
	body := rest[nl+1:]
	return parseSessionMetaBytes([]byte(body), true), file, true
}

// execOn runs `hc exec --on <machine>` — the relay-side counterpart of
// `hc exec`'s remote shell for metadata reads that only exist on the
// target machine.
func (c *Client) execOn(ctx context.Context, machine string, argv ...string) ([]byte, error) {
	args := append([]string{"exec", "--on", machine, "--timeout", "20", "--"}, argv...)
	return c.run(ctx, c.CoreBin, args...)
}

