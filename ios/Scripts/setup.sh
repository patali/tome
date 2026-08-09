#!/usr/bin/env bash
# One-step project setup: build the extraction bundle, then generate the Xcode
# project.
#
# The order matters and is easy to get wrong. Resources/Generated is derived and
# gitignored, so on a fresh clone it doesn't exist — and XcodeGen refuses to
# generate against a missing source directory. Running the bundle build first is
# not a nicety; nothing works without it.

set -euo pipefail

IOS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$IOS_DIR"

if [ ! -f Config.xcconfig ]; then
    cp Config.xcconfig.example Config.xcconfig
    echo "Created Config.xcconfig from the example — add your API key before building."
fi

echo "==> Building extraction bundle"
./Scripts/build-extraction-js.sh

if ! command -v xcodegen >/dev/null 2>&1; then
    echo
    echo "xcodegen not found. Install it with:  brew install xcodegen" >&2
    exit 1
fi

echo "==> Generating Xcode project"
xcodegen generate

echo
echo "Done. Next:"
echo "  open $IOS_DIR/Tome.xcodeproj"
echo "  Set the signing team on BOTH targets (Tome, TomeShare), then run on a device."
