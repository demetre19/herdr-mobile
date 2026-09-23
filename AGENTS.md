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
