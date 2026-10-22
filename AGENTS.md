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

## Agent lane liveness

- `agentLiveness` (`frontend/src/lib/agents.ts`) splits working lanes by
  `last_active_at`: active within `AGENT_QUIET_AFTER_MS` (5 min) reads
  live, older or missing reads quiet, non-working reads settled.
- Live lanes pulse green around the status dot and take a green card
  border; quiet lanes take an amber border; dots never move. Threshold is
  far above the 15s push cadence so genuine runners never flicker.
- Liveness rides the 60s `relativeNow` ticker already in AgentList — no
  new timers, no extra polling, no protocol change.
