#!/bin/sh
# Build the Go relay binary for the current platform.
# Usage: scripts/build.sh [output-dir]
# Produces: herdr-mobile-relay (or herdr-mobile-relay.exe on windows)
set -eu

SCRIPT_DIR=${0%/*}
REPO_DIR=$(CDPATH='' cd "$SCRIPT_DIR/.." && pwd)
OUT_DIR="${1:-$REPO_DIR/bin}"

# The product version comes from the plugin manifest, not git describe: an
# untagged or dirty checkout produces strings like "0.21.3-ux2-d449e8d" that
# the update checker's semver parser rejects, which would silently disable
# self-update on locally built relays.
VERSION=$(sed -n 's/^version = "\([0-9.]*\)"$/\1/p' "$REPO_DIR/herdr-plugin.toml")
[ -n "$VERSION" ] || VERSION="dev"
REVISION=$(git -C "$REPO_DIR" rev-parse HEAD 2>/dev/null || echo "unknown")

mkdir -p "$OUT_DIR"

echo "Building herdr-mobile-relay $VERSION ($REVISION)..."
cd "$REPO_DIR"
CGO_ENABLED=0 go build \
    -ldflags "-s -w -X main.version=$VERSION -X main.revision=$REVISION" \
    -o "$OUT_DIR/herdr-mobile-relay" \
    ./cmd/herdr-mobile-relay

echo "Built: $OUT_DIR/herdr-mobile-relay"
