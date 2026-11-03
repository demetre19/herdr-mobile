// Package hc is the relay's client for the new Herdr desktop app
// (herdr-app window + herdr-core `hc` daemon). It shells out to the two CLIs
// installed beside the user's binaries and decodes their JSON output into
// topology records the phone can render next to the legacy herdr inventory.
package hc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// PanePrefix namespaces hc pane ids so they can never collide with legacy
// herdr pane ids on the wire or in client-side pane caches.
const PanePrefix = "hc:"

// ErrUnavailable is returned when neither the app window nor the core daemon
// answered; the watcher surfaces it as an offline inventory rather than an
// empty one.
var ErrUnavailable = errors.New("herdr app is not running")

// Space mirrors `herdr-app ls` spaces[].tabs[].terminals[].
type Space struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Folder  string  `json:"folder"`
	Machine *string `json:"machine"`
	Active  bool    `json:"active"`
	Tabs    []Tab   `json:"tabs"`
}

type Tab struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Active    bool       `json:"active"`
	Settled   bool       `json:"settled"`
	Terminals []Terminal `json:"terminals"`
}

// Terminal is a pane as the app window reports it.
type Terminal struct {
	Address   string   `json:"terminal"`
	Pane      string   `json:"pane"`
	Status    *string  `json:"status"`
	App       *string  `json:"app"`
	Name      *string  `json:"name"`
	SubAgents []string `json:"sub_agents"`
}

// CoreTerminal mirrors `hc ls --all --json` terminals[] plus the richer
// `hc get` record.
type CoreTerminal struct {
	Address    string            `json:"address"`
	Argv       []string          `json:"argv"`
	Agent      string            `json:"agent"`
	Cwd        string            `json:"cwd"`
	Name       string            `json:"name"`
	State      string            `json:"state"`
	Status     TerminalStatus    `json:"status"`
	Title      string            `json:"title"`
	Turn       int               `json:"turn"`
	Cols       int               `json:"cols"`
	Rows       int               `json:"rows"`
	CreatedMs  int64             `json:"created_ms"`
	FinishedMs *int64            `json:"finished_ms"`
	ExitCode   *int              `json:"exit_code"`
	Pid        *int64            `json:"pid"`
	Labels     map[string]string `json:"labels"`
	// Foreground describes the process currently holding the terminal —
	// its argv carries --model/--role/--effort launch flags when the
	// program accepts them (the chip data the phone renders).
	Foreground *struct {
		Argv []string `json:"argv"`
		Name string   `json:"name"`
		Pid  int64    `json:"pid"`
		Cwd  string   `json:"cwd"`
	} `json:"foreground"`
}

// TerminalStatus decodes the `status` field's two wire shapes: `hc ls`
// reports a plain string ("working"), `hc get` reports an object
// ({"app":"omp","state":"done"}). A typed string field silently rejected
// every `hc get` record — no chips, no live status, no title ever made it
// into the snapshot.
type TerminalStatus struct {
	State string
	App   string
}

func (s *TerminalStatus) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		return json.Unmarshal(data, &s.State)
	}
	var obj struct {
		State string `json:"state"`
		App   string `json:"app"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	s.State, s.App = obj.State, obj.App
	return nil
}

// agentArgv returns the most informative argv on the record: the live
// foreground process when present, else the launch argv.
func (t *CoreTerminal) AgentArgv() []string {
	if t.Foreground != nil && len(t.Foreground.Argv) > 0 {
		return t.Foreground.Argv
	}
	return t.Argv
}

// Do runs `herdr-app do <command> [flags]` and returns the app's error
// string when it refuses. Used by commands the core CLI does not cover
// (tab/space renames live on the window, not the daemon).
func (c *Client) Do(ctx context.Context, command string, args ...string) error {
	argv := append([]string{"do", command}, args...)
	out, err := c.run(ctx, c.AppBin, argv...)
	if err != nil {
		return err
	}
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return fmt.Errorf("herdr-app do %s: %w", command, err)
	}
	if payload.Error != "" {
		return errors.New(payload.Error)
	}
	return nil
}

// Kill ends a terminal's program (`hc kill`). Works on remote-machine
// addresses too — the core daemon owns the process.
func (c *Client) Kill(ctx context.Context, address string) error {
	_, err := c.run(ctx, c.CoreBin, "kill", address)
	return err
}

// Put copies a relay-local file beside the terminal on its machine and
// returns the absolute path `hc put` reports (`machine/addr` terminals
// land the file on the remote machine, which is exactly what the agent
// must paste).
func (c *Client) Put(ctx context.Context, address, path string) (string, error) {
	out, err := c.run(ctx, c.CoreBin, "put", "--json", address, path)
	if err != nil {
		return "", err
	}
	var payload struct {
		Path    string `json:"path"`
		Dest    string `json:"destination"`
		Landed  string `json:"landed"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		// --json unsupported or text output: take the last non-empty line.
		lines := strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' })
		for i := len(lines) - 1; i >= 0; i-- {
			if line := strings.TrimSpace(lines[i]); line != "" {
				return line, nil
			}
		}
		return "", fmt.Errorf("hc put %s: empty output", address)
	}
	for _, candidate := range []string{payload.Path, payload.Dest, payload.Landed, payload.Message} {
		if strings.HasPrefix(candidate, "/") {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("hc put %s: no path in output", address)
}

// TabRename renames a window tab (`herdr-app do tab.rename`).
func (c *Client) TabRename(ctx context.Context, tabID, name string) error {
	return c.Do(ctx, "tab.rename", "--id", tabID, "--name", name)
}

// TypeText types text into a terminal WITHOUT submitting — the hc keys
// vocabulary only accepts one key per argument, so each rune becomes its
// own call parameter (" " → space). That is deliberately byte-exact:
// question UIs treat a pasted-and-submitted string as an answer, where
// typing leaves the text editable.
func (c *Client) TypeText(ctx context.Context, address, text string) error {
	keys := make([]string, 0, len(text))
	for _, r := range text {
		if r == ' ' {
			keys = append(keys, "space")
		} else {
			keys = append(keys, string(r))
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return c.Keys(ctx, address, keys)
}

type serverStatus struct {
	Core struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"core"`
	Version string `json:"version"`
}

type Client struct {
	// AppBin is the herdr-app CLI (window topology); CoreBin is herdr-core
	// (`hc`, terminal engine). Both resolve through exec.LookPath so the
	// relay service PATH applies.
	AppBin  string
	CoreBin string
	Timeout time.Duration

	// observers holds persistent `hc attach --observe` sessions — one per
	// local terminal whose styled screen is being read/watched. Keyed by
	// terminal address.
	observersMu sync.Mutex
	observers   map[string]*AttachObserver
}

func NewClient(appBin, coreBin string) *Client {
	if appBin == "" {
		appBin = "herdr-app"
	}
	if coreBin == "" {
		coreBin = "herdr-core"
	}
	return &Client{AppBin: appBin, CoreBin: coreBin, Timeout: 6 * time.Second}
}

func (c *Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 6 * time.Second
	}
	return c.Timeout
}

func (c *Client) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%s %s: timed out", bin, strings.Join(args, " "))
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s %s: %s", bin, strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// Topology returns the window's spaces → tabs → terminals. An error means the
// window is closed or unreachable; callers fall back to the core list.
func (c *Client) Topology(ctx context.Context) ([]Space, error) {
	out, err := c.run(ctx, c.AppBin, "ls")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Spaces []Space `json:"spaces"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("herdr-app ls: %w", err)
	}
	return payload.Spaces, nil
}

// ServerID returns the core daemon's stable id/name for server_session_id
// synthesis, or an error when the daemon is down.
func (c *Client) ServerID(ctx context.Context) (string, error) {
	out, err := c.run(ctx, c.CoreBin, "server", "status")
	if err != nil {
		return "", err
	}
	var status serverStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("hc server status: %w", err)
	}
	if status.Core.ID == "" {
		return "", errors.New("hc server status: empty core id")
	}
	return status.Core.ID, nil
}

// TabNew mounts a new tab + terminal in the open window via `herdr-app do`.
// tab.new lands in the SELECTED space, which can live on a remote machine —
// callers pass the id of a space local to this machine (pick via
// LocalSpaceID); with no local space, space.new creates one (its machine
// defaults to this one, unlike tab.new's selected-space semantics).
// name/folder/argv/task are optional; an error means the window is closed or
// refused the command.
func (c *Client) TabNew(ctx context.Context, spaceID, name, folder string, argv []string, task string) error {
	args := []string{"do", "tab.new", "--space", spaceID}
	if spaceID == "" {
		// space.new names its first tab with --tab, not --name.
		args = []string{"do", "space.new", "--tab", name}
	} else if name != "" {
		args = append(args, "--name", name)
	}
	if folder != "" {
		args = append(args, "--folder", folder)
	}
	if len(argv) > 0 {
		raw, err := json.Marshal(argv)
		if err != nil {
			return fmt.Errorf("tab.new argv: %w", err)
		}
		args = append(args, "--argv", string(raw))
	}
	if task != "" {
		args = append(args, "--task", task)
	}
	out, err := c.run(ctx, c.AppBin, args...)
	if err != nil {
		return err
	}
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return fmt.Errorf("herdr-app do %s: %w", args[1], err)
	}
	if payload.Error != "" {
		return errors.New(payload.Error)
	}
	return nil
}

// LocalSpaceID returns a window space belonging to this machine — remote
// machines' spaces carry a federation machine name, local ones have
// Machine nil. The active local space wins; empty means none is open.
func LocalSpaceID(spaces []Space) string {
	fallback := ""
	for _, space := range spaces {
		if space.Machine != nil {
			continue
		}
		if space.Active {
			return space.ID
		}
		if fallback == "" {
			fallback = space.ID
		}
	}
	return fallback
}

// Terminals lists live + finished terminals from the core daemon. It covers
// machines/terminals the window does not show, so the watcher merges it under
// the app topology.
// Terminals lists live + finished terminals plus the federation's remote
// machine names (a relay publishes remote terminals under machine/address
// aliases, so the phone needs the names to fold those aliases onto the
// owning relay's section).
func (c *Client) Terminals(ctx context.Context) ([]CoreTerminal, []string, error) {
	out, err := c.run(ctx, c.CoreBin, "ls", "--all", "--json")
	if err != nil {
		return nil, nil, err
	}
	var payload struct {
		Machines []struct {
			Name string `json:"name"`
		} `json:"machines"`
		Terminals []CoreTerminal `json:"terminals"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, nil, fmt.Errorf("hc ls: %w", err)
	}
	names := make([]string, 0, len(payload.Machines))
	for _, machine := range payload.Machines {
		if machine.Name != "" {
			names = append(names, machine.Name)
		}
	}
	return payload.Terminals, names, nil
}

// Read returns the terminal screen plus `lines` of scrollback, ANSI stripped
// by hc itself (its read output is plain text).
func (c *Client) Read(ctx context.Context, address string, lines int) (string, error) {
	out, err := c.run(ctx, c.CoreBin, "read", "--json", "--lines",
		fmt.Sprintf("%d", lines), address)
	if err != nil {
		return "", err
	}
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", fmt.Errorf("hc read %s: %w", address, err)
	}
	return payload.Text, nil
}

// Styled screen reads live in observer.go (persistent `hc attach
// --observe` sessions feeding the VT emulator in styled.go).

// Get returns the full record for one terminal (status, title, argv…).
func (c *Client) Get(ctx context.Context, address string) (*CoreTerminal, error) {
	out, err := c.run(ctx, c.CoreBin, "get", address, "--json")
	if err != nil {
		return nil, err
	}
	var term CoreTerminal
	if err := json.Unmarshal(out, &term); err != nil {
		return nil, fmt.Errorf("hc get %s: %w", address, err)
	}
	return &term, nil
}

// BusyTitle reports whether a terminal title carries an active braille
// spinner (omp/pi render ⠋⠙⠹… while working). Used when no explicit
// status is reported.
func BusyTitle(title string) bool {
	return strings.ContainsAny(title, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
}

// Send pastes text into a terminal and presses enter. `hc send` itself waits
// for the program to pause, so a busy agent surfaces as a context timeout;
// the caller reports that as "agent is busy" rather than a hard failure.
func (c *Client) Send(ctx context.Context, address, text string) error {
	_, err := c.run(ctx, c.CoreBin, "send", address, text)
	return err
}

// SendForce pastes text even when the program looks blocked.
func (c *Client) SendForce(ctx context.Context, address, text string) error {
	_, err := c.run(ctx, c.CoreBin, "send", "--force", address, text)
	return err
}

// Keys presses named keys (enter, esc, up, ctrl+c, …).
func (c *Client) Keys(ctx context.Context, address string, keys []string) error {
	args := append([]string{"keys", address}, keys...)
	_, err := c.run(ctx, c.CoreBin, args...)
	return err
}

// CoreEvent is one fact change on the `hc events` stream. A nil Event with
// non-empty Snapshot is the initial full inventory; subsequent lines are
// JSON-RPC notifications (title/state/exit/… deltas per terminal).
type CoreEvent struct {
	// Snapshot carries the full terminal records on the connect frame only.
	Snapshot []CoreTerminal
	// Terminal is the address an incremental event applies to.
	Terminal string
	// Type is the event kind (title, state, exit, …) — opaque to hc itself.
	Type string
	// Title is set for type=="title" events (the only in-place patch).
	Title string
	// Seq is the core's ordering counter.
	Seq int64
}

// Events streams `hc events --json`: one full snapshot frame followed by
// per-change JSON-RPC notifications. The handler runs on the scanner
// goroutine, so it must not block. Events returns when the stream ends or
// ctx is cancelled — callers restart it to keep the feed alive.
func (c *Client) Events(ctx context.Context, handler func(CoreEvent)) error {
	cmd := exec.CommandContext(ctx, c.CoreBin, "events", "--json")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("hc events: %w", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("hc events: %w", err)
	}
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, 256<<10), 8<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		var head struct {
			Method    string            `json:"method"`
			Terminals []json.RawMessage `json:"terminals"`
			Params    *struct {
				Type     string          `json:"type"`
				Terminal string          `json:"terminal"`
				Seq      int64           `json:"seq"`
				Data     json.RawMessage `json:"data"`
			} `json:"params"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			continue
		}
		switch {
		case head.Method == "event" && head.Params != nil:
			event := CoreEvent{Type: head.Params.Type, Terminal: head.Params.Terminal, Seq: head.Params.Seq}
			if event.Type == "title" && head.Params.Data != nil {
				var data struct {
					Title string `json:"title"`
				}
				if err := json.Unmarshal(head.Params.Data, &data); err == nil {
					event.Title = data.Title
				}
			}
			handler(event)
		case head.Terminals != nil:
			event := CoreEvent{Snapshot: make([]CoreTerminal, 0, len(head.Terminals))}
			for _, raw := range head.Terminals {
				var term CoreTerminal
				if err := json.Unmarshal(raw, &term); err == nil && term.Address != "" {
					event.Snapshot = append(event.Snapshot, term)
				}
			}
			handler(event)
		}
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("hc events: %w", err)
	}
	return nil
}
