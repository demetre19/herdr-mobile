# Agent Instructions

## Guidelines

- Read `README.md` and `QUICKSTART.md` before making changes
- Run the project's build/test commands before committing (check `Makefile`,
  `go.mod`, and `frontend/package.json`)
- Keep changes minimal and focused on the task
- Prefer early returns over nested conditionals
- Handle error states explicitly
- Use semantic HTML and ARIA attributes for accessibility in frontend code
- Follow existing code style and conventions in the repo
- Do not introduce new dependencies without justification

## Verification

- Run linting and type checks before committing
- Run tests relevant to changed code
- Verify the build passes

## Git

- Write clear, concise commit messages
- Stage only files related to the current task
- Do not push to main/master without explicit permission

## Local deployment (this checkout)

- The hosted app at `herdr-app.seotimemachines.com` is deployed manually, NOT via
  `make web-deploy` or the relay's `deploy_app_update` command (both publish the
  upstream bundle and would overwrite the patched deploy — `deploy_app_update`
  is refused server-side and `install_update` runs with `deployAppFirst=false`).
- Frontend deploy: `cd frontend && bun run build && bun scripts/release.mjs`,
  then `cp -R ../web /tmp/herdr-app-deploy` and
  `bun scripts-local/patch-deploy.mjs /tmp/herdr-app-deploy` (verifier + hash
  rewrite only — all former CSS/viewport patches are baked into source), then
  `rsync -az --delete /tmp/herdr-app-deploy/ jitsi-meet:/opt/jitsi/config/web/herdr-app-new/`
  and swap `herdr-app`/`herdr-app-new` on the server. Verify every
  `release.json` file hash against the deployed copy.
- Relay binary: `scripts/build.sh`, stage into
  `~/.local/share/herdr-mobile-relay/releases/<version>-<hash>-darwin-arm64`
  (binary + `relay/` + `web/` + `release-manifest.json`), repoint the
  `current` symlink, `launchctl kickstart -k` the service.
- The service wrapper `~/.config/herdr/plugins/config/herdr-mobile-relay.events/relay-gateway-service.sh`
  exports a real PATH (`~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`)
  because launchd hands services a bare `/usr/bin:/bin` and agent profiles
  resolve executables via `exec.LookPath` (`internal/profiles/resolver.go`
  `binaryPath` also probes those dirs as a fallback).
- Self-update reads `demetre19/herdr-mobile` releases by default
  (`internal/update` canonical bases); `HERDR_UPDATE_REPO` overrides the feed
  (`owner/repo`) and the service wrapper exports it explicitly. Never point it
  at upstream `0cv/herdr-mobile-relay` — an upstream install would overwrite
  this checkout's patches. Local builds stamp `herdr-plugin.toml`'s version
  (`scripts/build.sh`), not `git describe`, so the semver check stays valid.
- App update state is driven only by the same-origin `/version.json` check;
  upstream relay versions never trigger `deployment-required`.

## Terminal rendering

- Keep fixed-height virtual rows: regular view wraps text into physical rows
  before rendering; never restore CSS multiline wrapping or browser anchoring.
- Wrapping breaks at word boundaries (after the last space/tab that fits),
  measured in grapheme display columns; overlong words still split by
  grapheme, and active ANSI styles carry across continuation rows.
  Mode, interface-size, and width changes must repaint the cached frame.
- The outer terminal owns horizontal scrolling. Row boxes must not clip text;
  wide view reserves the measured 240-column canvas even for short snapshots.
- Verify compact/regular/large with styled ANSI and Unicode, touch-pan to the
  last character, and streaming while unpinned. Plain text overflow alone is
  not sufficient evidence of device correctness.
- Normal mode must bound ALL content rows, including box-decorated prose and
  stale-width tables from existing lanes. Only wide mode may preserve their

## Agent lane status

- Card tone mirrors Herdr's own status and nothing else: `agentStatusTone`
  (`frontend/src/lib/agents.ts`) maps working → green pulse + card border,
  blocked → red, attention → amber, everything else muted. Dots never move.
- The event stream is the live leg: `SessionCache` (`internal/herdr/events.go`)
  handles Herdr's dedicated `pane.agent_status_changed` event (alias existed
  but no case — transitions were silently dropped) and treats a status-only
  `pane.updated` as a topology commit (the change filter excluded Status, so
  the commit never fired and the phone kept the stale state to the next poll).
- The relay's poller trusts `tab.list` (`agent_status`) over the
  `agent.list` pane snapshot: the pane keeps its last-known status after a
  session dies quietly, while the tab record settles. Verified live
  2026-10-22 (Herdr `api snapshot` showed L25/L28/L32 idle while the phone
  showed them green — the pane snapshot was stale, the tab was truth).

## Herdr App ("hc") lane (v1.0.32)

- `internal/hc` watches the new Herdr app. **Terminal records come from
  the `hc events --json` stream** (v1.0.39): the connect frame carries
  FULL records (labels, foreground argv, status object) and JSON-RPC
  events patch them — `title` patches in place, everything else marks
  the address dirty for a one-shot `hc get` resync in the next poll.
  While `streamLive`, polls never shell out to `hc get`; on drop the
  stream restarts (2s backoff) and `hc ls --all --json` + bounded `hc
  get` is the downgrade path. Event wakes re-poll with a 400ms debounce
  floored at 2s (`pollMu` serializes polls, `wakeMu` guards the
  AfterFunc timer). `herdr-app ls` (window topology) supplies spaces →
  tabs; `hc ls` supplies `machines[]` only (remote names also derive
  from `machine/addr` prefixes mid-stream). `hc ls` counts detached
  workers and finished shells as terminals — merging them while the
  window is open renders dead processes as cards; with the window
  closed, `regroupLabeledTerminals` rebuilds real spaces from
  `herdr.space`/`herdr.tab`/`ofm.tab_label`/`where` labels
  (`ofm.space`/`ofm.tab` when the herdr.* ids are absent — lanes mount
  via ofm only). Unlabeled terminals never land in `hc-core:` synthetic
  spaces at all (v1.0.40): `hc`'s detached `add-*` workers and stray
  shells are invisible in the app, so the flat fallback skips them
  rather than minting cards; the synthetic-space shape stays as a
  contract but normally never ships. Never `hc get` a terminal just to
  decide whether to show it — remote `machine/addr` Gets cost a
  federation round-trip each.
- `hc_launch` mounts a tab+terminal in the open window: `handleHcLaunch`
  resolves `profile_id` through `profiles.Resolver.Profile().Argv` (same
  catalog as `agent_start`), then `Client.TabNew`. **`tab.new` lands in the
  app's SELECTED space, which can live on a remote machine** — always pass
  `hc.LocalSpaceID` (a `Machine == nil` space, active first); empty →
  `space.new` which defaults to the local machine (`--tab` names it, not
  `--name`). Capability `hc_launch` rides the same `hcW != nil` gate as
  `hc_view`.
- hc pane ids are `hc:<address>`; `server.go` intercepts them before the
  action switch and `dispatchHcCommand` (`internal/app/hc.go`) routes
  reads/watches/sends/keys through the hc CLI. hc panes carry
  `server_session_id=primary` + `generation=1` for target validation.
- The snapshot rides `push_config.hc` (never a standalone handshake
  message — handshake reads are positionally pinned by tests) plus
  `hc_inventory` broadcasts on change.
- hc pane watches skip `HandleProbePane` (no hc counterpart — it errors
  every tick and freezes the watch); `pollHcPaneWatch` reads each tick
  and `paneWatchUpdate` dedupes.
- Federation: `hc ls machines[]` rides the inventory so the frontend can
  fold `<machine>/<suffix>` mirror aliases onto the owning relay's cards.
  `applyHcInventory` must copy every new message field it adds.
  **`ID_PATTERN` (resource-id.ts) MUST accept `/`** — federated
  `machine/addr` pane/terminal ids built a null target tuple client-side
  before v1.0.39: reads silently no-opped and leases failed "no exact
  terminal identity". Remote `machine/addr` pane reads always use plain
  `hc read` (attach replay over federation is seconds per frame).
- **Styled hc frames are RENDERED, not raw** (v1.0.41): `hcStyledContent`
  used to forward the `hc attach --observe` byte stream verbatim — a full
  VT replay of cursor addressing, mode sets (`?25h`/`?2026l`/`?2004h`),
  OSC-8 links, and device probes. The phone's ansi parser renders SGR
  only, so everything else painted as literal `[34;4H` text and repaints
  stacked. `internal/hc/styled.go` `RenderVT` replays the stream through a
  small cell-grid emulator (DECSTBM margins, scroll/IL/DL, cursor
  addressing, full SGR incl. 8/256/truecolor + ITU colon subparams, wide
  runes) and serializes screen+scrollback as SGR-annotated rows — the
  `pane read --format ansi` contract. Dimensions come from `hc get` (the
  replay carries no size report). Two pitfalls permanently covered by
  `styled_test.go`: row-shifting ops must assign FRESH row slices after
  the memmove (clearing the displaced header erases the moved copy — one
  row stamps across the region), and the SGR operand loop must stay an
  indexed loop so `38;2;r;g;b`/`48` skip their operands (range-int loops
  can't seek).
- **Styled hc panes stream from persistent attach observers** (v1.0.42):
  `hcStyledContent` spawns ONE `hc attach --observe` per terminal
  (`Client.Observe`/`AttachObserver`, `internal/hc/observer.go`) feeding
  the VT emulator continuously; reads serialize the live grid (~10ms
  first paint, ~0ms after) — a genuine live stream. Collect-per-read
  died on busy panes: output never went quiet, every read burned the 8s
  deadline → ~9.5s-stale frames → phone timeout → "send buffer full,
  evicting client" churn. Dead children respawn lazily (5s floor); idle
  observers reap at 90s; mid-stream `ESC[8;r;c t` reports rebuild the
  grid; a paint-less attach death falls back to plain `hc read`. The
  old `ReadStyled`/lockedBuffer/quiet-detection path is gone.
- **Coherent hc frames: sync-buffered repaint + quiet-gated snapshot**
  (v1.0.43): the phone was jumping because snapshots serialized the grid
  mid-repaint. Two layers, both in `internal/hc`:
  - `styled.go` `feed()` buffers bytes inside a DEC `?2026l`/`?2026h`
    sync bracket into `syncBuf` and applies them atomically at `?2026h`;
    chunk-split escape sequences park in `pending[]` until the rest of
    the sequence arrives (`escape`/`csi` return `len(data)+1` to mark
    truncation). `RenderVT` flushes `syncBuf` at end-of-capture; the
    observer leaves brackets open and keeps buffering. `eraseDisplay`
    mode 3 also clears `scrollback` (xterm parity).
  - `observer.go` `snapshot()` only serializes after `observerQuiet`
    (40ms) of feed silence — repaint bursts land as back-to-back chunks,
    so silence = repaint finished. Between quiet windows it serves the
    last coherent frame (`snap`), so a read never ships a torn
    half-old/half-new grid. `pump` stamps `lastFeed` under `mu` after
    each chunk.
  - `TerminalView.svelte` `spliceScrollback` anchors on the first
    NON-EMPTY row (fence/border lines render as `text:''` separator
    tokens — every blank row matched them and spliced at a random blank
    deep in retained history, dropping the leading chunk each tick) and
    scores candidates by match length so a true boundary (matches all
    retained rows) beats a coincidental repeat. Both fixes together end
    the 127↔150-row oscillation measured in `tests/scroll-harness/`.
- **Unclosed `?2026` brackets must not starve the grid** (v1.0.44):
  sync buffering trusted the bracket to close; some herdr-core attaches
  emit `?2026l` and never `?2026h`, so the whole stream buffered and
  `serialize()` returned empty → plain-read fallback → monochrome.
  `snapshot()` now calls `emu.flushSync()` unconditionally on the quiet
  path (quiet = repaint done, drain held sync) and on the deadline path.
  Rule: any content-hold keyed on an input marker needs a reader-driven
  flush, because the close marker may simply never arrive.
