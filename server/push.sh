#!/usr/bin/env bash
# Build the Tome server image for the Pi (linux/arm64) and push it to GHCR.
#
# The build context is the REPO ROOT — the image bundles extension/ — so this
# script cd's there itself and can be run from anywhere.
#
# Auth needs a GitHub PAT with write:packages in $GHCR_PAT. The gh CLI's own
# token carries repo scopes only, so it can't be reused for the registry.
#
#   GHCR_PAT=ghp_… ./server/push.sh            # tag = short commit SHA
#   GHCR_PAT=ghp_… ./server/push.sh --latest   # …and move :latest too
#   GHCR_PAT=ghp_… ./server/push.sh --dirty    # allow an uncommitted tree
#
# Prints the pushed tag; put it in TOME_TAG in the host stack's .env.

set -euo pipefail

IMAGE=ghcr.io/patali/tome
PLATFORM=linux/arm64          # Raspberry Pi 4
REGISTRY=ghcr.io
OWNER=patali

cd "$(dirname "${BASH_SOURCE[0]}")/.."

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

docker info >/dev/null 2>&1 || die "docker daemon not reachable — start Docker Desktop"
[ -n "${GHCR_PAT:-}" ] || die "GHCR_PAT is not set (needs a PAT with write:packages)"

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
