#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export PATH="$PWD/.build/node-v24.20.0-linux-x64/bin:/usr/local/bin:/usr/bin:/bin"
export PLAYWRIGHT_BROWSERS_PATH="$PWD/.build/playwright"
cd dashboard
npm test --workspaces=false --offline -- "$@"
