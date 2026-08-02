#!/usr/bin/env bash
# Build a Chrome Web Store upload from extension/.
#
# The store needs a differently-shaped package than the one this project serves
# at /extension.zip, in two ways:
#
#   1. No "key" field. The store assigns the extension its identity and rejects
#      a manifest that pins one — "key field is not allowed in manifest".
#   2. manifest.json at the root of the archive. The served zip nests
#      everything under tome-extension/ so that unzipping gives you a folder to
#      point "Load unpacked" at, which is the opposite of what the store wants.
#
# The key stays in the repo on purpose. Chrome derives an unpacked extension's
# ID from that key, and chrome.storage is scoped per ID — so dropping it from
# the served zip would silently sign out everyone who installed that way and
# lose their settings. The store copy is a separate extension with its own ID
# regardless, so it loses nothing by not having one.
#
# Usage: extension/pack-store.sh [outfile]     (default: tome-extension-store.zip)

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
out="${1:-$PWD/../tome-extension-store.zip}"
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac

command -v zip >/dev/null || { echo "error: zip not found" >&2; exit 1; }
command -v python3 >/dev/null || { echo "error: python3 not found" >&2; exit 1; }

stage=$(mktemp -d "${TMPDIR:-/tmp}/tome-store.XXXXXX")
trap 'rm -rf "$stage"' EXIT

# Copy everything the served bundle contains, minus this script and the noise
# that should never ship.
tar --exclude='.DS_Store' --exclude='pack-store.sh' -cf - . | (cd "$stage" && tar -xf -)

python3 - "$stage/manifest.json" <<'PY'
import json, sys
path = sys.argv[1]
with open(path) as f:
    m = json.load(f)
if m.pop("key", None) is None:
    print("note: manifest had no key field", file=sys.stderr)
with open(path, "w") as f:
    json.dump(m, f, indent=2, ensure_ascii=False)
    f.write("\n")
print(f"packing {m['name']} {m['version']}")
PY

rm -f "$out"
(cd "$stage" && zip -qr "$out" . -x '*.DS_Store')

python3 - "$out" <<'PY'
import json, sys, zipfile
z = zipfile.ZipFile(sys.argv[1])
names = z.namelist()
# Fail loudly rather than let a bad archive reach the upload form, where the
# error arrives with no indication of which of the two rules was broken.
if "manifest.json" not in names:
    sys.exit("error: manifest.json is not at the archive root")
if "key" in json.loads(z.read("manifest.json")):
    sys.exit("error: key survived into the archive")
print(f"ok: {len(names)} entries, manifest at root, no key")
PY

echo "wrote $out"
