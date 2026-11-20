#!/usr/bin/env bash
#
# Runs the lookup acceptance harness with the given options. See README.md
# beside this script, or run it with --help.

set -euo pipefail

HARNESS_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
export HARNESS_CALLER_CWD=$PWD

# Volta's shims, when installed, pick the Node version package.json pins
# for the directory node starts in.
if [ -d "$HOME/.volta/bin" ]; then
    PATH="$HOME/.volta/bin:$PATH"
fi
cd "$HARNESS_DIR"

if ! node -e 'const [major, minor] = process.versions.node.split(".").map(Number); process.exit(major > 22 || (major === 22 && minor >= 12) ? 0 : 1)'; then
    echo "lookup-acceptance: needs Node 22.12 or later, found $(node --version); package.json pins the version for Volta" >&2
    exit 2
fi

# A sleep of the host voids a run, so on macOS the run keeps the host from
# sleeping while idle. Closing the lid on battery sleeps it all the same.
if command -v caffeinate >/dev/null 2>&1; then
    exec caffeinate -i node src/main.mjs "$@"
fi
exec node src/main.mjs "$@"
