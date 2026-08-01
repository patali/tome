#!/usr/bin/env bash
# Pin a tome image tag on the host and roll the stack onto it.
#
# Lives here rather than in the host config repo because it is the other half
# of .github/workflows/deploy.yml — the workflow calls it by path and hands it
# a tag, so the two have to change together.
#
# Invoked over Tailscale SSH by that workflow:
#
#     ssh patali@flintbox /home/patali/bin/tome-deploy.sh <sha>
#
# Also reads SSH_ORIGINAL_COMMAND, so it works unchanged behind a restricted
# `command=` entry in authorized_keys.
#
# The stack it drives (compose.yaml, .env) is defined in the host config repo.
# The four constants below are that contract; override via the environment if
# the stack moves.
#
# Guarded three ways, each for a real failure mode:
#
#   flock       - two merges landing together would interleave a sed and a
#                 `compose up`, leaving the stack on a tag matching neither.
#   docker pull - runs BEFORE .env is touched, so a bad tag fails with the
#                 stack untouched rather than pointing at a missing image.
#   rollback    - if the new container never reports healthy, the previous tag
#                 is restored. A deploy that leaves the service down is worse
#                 than one that leaves it on the old image.

set -euo pipefail

STACK_DIR="${TOME_STACK_DIR:-/home/patali/stacks/tome}"
IMAGE="${TOME_IMAGE:-ghcr.io/patali/tome}"
SERVICE="${TOME_SERVICE:-app}"
CONTAINER="${TOME_CONTAINER:-tome-app-1}"

ENV_FILE="$STACK_DIR/.env"
LOCK=/tmp/tome-deploy.lock
HEALTH_TRIES=24          # x5s = 120s; compose start_period alone is 30s
HEALTH_SLEEP=5

log() { echo "[deploy] $*"; }
die() { echo "[deploy] error: $*" >&2; exit 1; }

# ---------------------------------------------------------------- tag input --
tag="${1:-}"
if [ -z "$tag" ] && [ -n "${SSH_ORIGINAL_COMMAND:-}" ]; then
    # Restricted-key mode: sshd swapped in this script, the caller's real
    # command line survives here. The tag is the last word of it.
    tag="${SSH_ORIGINAL_COMMAND##* }"
fi

# This value reaches `docker pull`, so constrain it to what the workflow can
# actually produce: a git short SHA.
[[ "$tag" =~ ^[0-9a-f]{7,40}$ ]] || die "invalid tag: '${tag}' (want a hex commit SHA)"

# ------------------------------------------------------------------- deploy --
exec 9>"$LOCK"
flock -w 300 9 || die "another deploy is holding $LOCK"

cd "$STACK_DIR" || die "no stack dir at $STACK_DIR"
[ -f "$ENV_FILE" ] || die "no .env at $ENV_FILE"

previous=$(sed -n 's/^TOME_TAG=//p' "$ENV_FILE")
[ -n "$previous" ] || die "could not read current TOME_TAG from $ENV_FILE"

if [ "$previous" = "$tag" ]; then
    log "already on $tag — nothing to do"
    exit 0
fi

log "$previous -> $tag"
log "pulling $IMAGE:$tag"
docker pull "$IMAGE:$tag" >/dev/null || die "pull failed — is $tag pushed to GHCR?"

set_tag() {
    sed -i "s/^TOME_TAG=.*/TOME_TAG=$1/" "$ENV_FILE"
}

wait_healthy() {
    local i status
    for ((i = 1; i <= HEALTH_TRIES; i++)); do
        status=$(docker inspect -f '{{.State.Health.Status}}' "$CONTAINER" 2>/dev/null || echo missing)
        [ "$status" = healthy ] && return 0
        sleep "$HEALTH_SLEEP"
    done
    return 1
}

set_tag "$tag"
log "recreating $SERVICE"
docker compose up -d "$SERVICE"

if wait_healthy; then
    log "healthy on $tag"
    exit 0
fi

# ----------------------------------------------------------------- rollback --
log "NOT healthy after $((HEALTH_TRIES * HEALTH_SLEEP))s — rolling back to $previous"
docker logs --tail 40 "$CONTAINER" 2>&1 | sed 's/^/[deploy]   /' || true

set_tag "$previous"
docker compose up -d "$SERVICE"

if wait_healthy; then
    die "deploy of $tag failed; rolled back to $previous (stack is healthy)"
fi
die "deploy of $tag failed AND rollback to $previous is unhealthy — needs a human"
