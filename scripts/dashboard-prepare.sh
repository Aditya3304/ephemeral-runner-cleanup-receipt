#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export PATH="$PWD/.build/node-v24.20.0-linux-x64/bin:/usr/local/bin:/usr/bin:/bin"
cd dashboard
test -f package-lock.json || { echo 'Restore the committed dashboard lockfile before setup.' >&2; exit 1; }
npm ci --workspaces=false --no-audit --no-fund
export PLAYWRIGHT_BROWSERS_PATH="$PWD/../.build/playwright"
./node_modules/.bin/playwright install chromium
