#!/usr/bin/env bash
# Build the Tome server image for the Pi (linux/arm64) and push it to GHCR.
#
# The build context is the REPO ROOT — the image bundles extension/ — so this
# script cd's there itself and can be run from anywhere.
#
# Auth needs a GitHub PAT with write:packages. Put it in server/.env (copy
# server/.env.example — gitignored), or export GHCR_PAT; an exported value
# wins. The gh CLI's own token carries repo scopes only and won't work here.
#
#   ./server/push.sh            # tag = short commit SHA
#   ./server/push.sh --latest   # …and move :latest too
#   ./server/push.sh --dirty    # allow an uncommitted tree
#
# Prints the pushed tag; put it in TOME_TAG in the host stack's .env.

set -euo pipefail

IMAGE=ghcr.io/patali/tome
PLATFORM=linux/arm64          # Raspberry Pi 4
REGISTRY=ghcr.io
OWNER=patali

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

push_latest=false
allow_dirty=false
for arg in "$@"; do
    case "$arg" in
        --latest)  push_latest=true ;;
        --dirty)   allow_dirty=true ;;
        # BSD sed (macOS) has no \?, so strip the marker in two plain steps.
        -h|--help) sed -n '2,15p' "$0" | sed -e 's/^# //' -e 's/^#$//'; exit 0 ;;
        *)         echo "unknown option: $arg" >&2; exit 2 ;;
    esac
done

die() { echo "error: $*" >&2; exit 1; }

# Fall back to server/.env so the token lives in one gitignored file instead of
# your shell history. Anything already exported takes precedence, which keeps
# CI working without the file.
ENV_FILE="$SCRIPT_DIR/.env"
if [ -z "${GHCR_PAT:-}" ] && [ -f "$ENV_FILE" ]; then
    mode=$(stat -f '%Lp' "$ENV_FILE" 2>/dev/null || stat -c '%a' "$ENV_FILE")
    [ "$mode" = "600" ] || echo "warning: $ENV_FILE is mode $mode — chmod 600 it" >&2
    set -a; . "$ENV_FILE"; set +a
fi

# Config first, daemon second: a missing token is cheaper to report than a
# missing daemon, and reporting both in one run beats two round trips.
[ -n "${GHCR_PAT:-}" ] || die "GHCR_PAT not set — copy server/.env.example to server/.env and fill it in"
docker info >/dev/null 2>&1 || die "docker daemon not reachable — start Docker Desktop"

# The tag has to identify exactly what shipped, so a dirty tree is refused
# unless you say otherwise.
if [ -n "$(git status --porcelain)" ] && [ "$allow_dirty" = false ]; then
    die "working tree is dirty — commit first, or pass --dirty"
fi

sha=$(git rev-parse --short HEAD)
[ "$allow_dirty" = true ] && sha="${sha}-dirty"

tags=(-t "$IMAGE:$sha")
if [ "$push_latest" = true ]; then
    tags+=(-t "$IMAGE:latest")
fi

echo "==> building $IMAGE:$sha for $PLATFORM"
# Attestations are off: they land in the registry as bogus "unknown/unknown"
# platform rows in the GHCR package listing.
docker build \
    --platform "$PLATFORM" \
    --provenance=false --sbom=false \
    "${tags[@]}" \
    -f server/Dockerfile .

echo "==> logging in to $REGISTRY as $OWNER"
printf '%s' "$GHCR_PAT" | docker login "$REGISTRY" -u "$OWNER" --password-stdin

echo "==> pushing"
docker push "$IMAGE:$sha"
if [ "$push_latest" = true ]; then
    docker push "$IMAGE:latest"
fi

echo
echo "pushed:  $IMAGE:$sha"
echo "next:    set TOME_TAG=$sha in the host stack's .env, then redeploy"
