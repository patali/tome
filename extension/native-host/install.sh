#!/bin/sh
# Installs the Tome mail helper as a Chrome native-messaging host (macOS).
#
#   ./install.sh [extension-id]
#
# Without an argument, the extension ID is derived from this repo's
# extension/ path (how Chrome IDs unpacked extensions). If you loaded the
# extension from a different path, pass its ID (chrome://extensions,
# Developer mode, "ID: ..." on the Tome card).
set -e

HERE=$(cd "$(dirname "$0")" && pwd)
EXT_DIR=$(dirname "$HERE")
HOST_NAME="com.tome.mailer"

EXT_ID="${1:-}"
if [ -z "$EXT_ID" ]; then
    # Unpacked-extension ID = sha256(absolute path), first 32 hex chars, 0-f -> a-p.
    EXT_ID=$(python3 - "$EXT_DIR" <<'PY'
import hashlib, sys
digest = hashlib.sha256(sys.argv[1].encode("utf-8")).hexdigest()[:32]
print("".join(chr(ord('a') + int(c, 16)) for c in digest))
PY
)
    echo "derived extension id from $EXT_DIR: $EXT_ID"
fi

chmod +x "$HERE/tome_mailer.py"

MANIFEST=$(cat <<JSON
{
  "name": "$HOST_NAME",
  "description": "Tome mail helper: opens Mail.app with a rendered article attached",
  "path": "$HERE/tome_mailer.py",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://$EXT_ID/"]
}
JSON
)

installed=0
for dir in \
    "$HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts" \
    "$HOME/Library/Application Support/Arc/User Data/NativeMessagingHosts" \
    "$HOME/Library/Application Support/Chromium/NativeMessagingHosts" \
    "$HOME/Library/Application Support/BraveSoftware/Brave-Browser/NativeMessagingHosts" \
    "$HOME/Library/Application Support/Microsoft Edge/NativeMessagingHosts"
do
    parent=$(dirname "$dir")
    [ -d "$parent" ] || continue
    mkdir -p "$dir"
    printf '%s\n' "$MANIFEST" > "$dir/$HOST_NAME.json"
    echo "installed: $dir/$HOST_NAME.json"
    installed=1
done

if [ "$installed" = 0 ]; then
    echo "no Chromium-family browser profile found under ~/Library/Application Support" >&2
    exit 1
fi
echo "done — restart the browser (or reload the extension) and try 'Send via email'."
