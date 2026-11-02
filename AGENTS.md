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

- `internal/hc` watches the new Herdr app: `herdr-app ls` (window topology)
  is the inventory; `herdr-core`/`hc ls --all --json` only fills in when
  the window is unreachable (`state=="live"` entries get synthetic
  `hc-core:<address>` spaces that MUST be appended to `snap.Spaces` — the
  phone renders core terminals from spaces, not agents). `hc ls` counts
  detached workers and finished shells as terminals — merging them while
  the window is open renders dead processes as cards.
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
