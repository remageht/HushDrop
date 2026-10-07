#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! command -v node >/dev/null 2>&1; then
    echo "[apply-android-custom.sh] Error: node is required to run apply-android-custom.js" >&2
    exit 1
fi

node "${SCRIPT_DIR}/apply-android-custom.js" "$@"
