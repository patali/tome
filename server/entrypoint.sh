#!/bin/sh
# Starts as root only to fix /data ownership (named volumes are mounted
# root-owned by some runtimes, e.g. Apple's container), then drops to the
# unprivileged "tome" user.
set -e
if [ "$(id -u)" = "0" ]; then
    mkdir -p "${TOME_DATA_DIR:-/data}"
    chown tome:tome "${TOME_DATA_DIR:-/data}"
    exec su-exec tome tome "$@"
fi
exec tome "$@"
