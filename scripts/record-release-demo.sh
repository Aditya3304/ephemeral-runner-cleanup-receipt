#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
[[ $# -eq 2 ]] || { echo 'Usage: bash scripts/record-release-demo.sh PASS_RECEIPT FAILURE_RECEIPT' >&2; exit 1; }
export PATH="$PWD/.build/node-v24.20.0-linux-x64/bin:/usr/local/bin:/usr/bin:/bin"
export PLAYWRIGHT_BROWSERS_PATH="$PWD/.build/playwright"
cd dashboard
node record-release-demo.mjs ../.build/release/demo "$1" "$2"
