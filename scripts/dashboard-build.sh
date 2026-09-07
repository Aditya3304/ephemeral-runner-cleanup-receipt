#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export PATH="$PWD/.build/node-v24.20.0-linux-x64/bin:/usr/local/bin:/usr/bin:/bin"
test -f dashboard/node_modules/.package-lock.json || { echo 'Run bash dev dashboard-prepare once to cache the UI dependencies.' >&2; exit 1; }
(cd dashboard && npm run build --workspaces=false --offline)
mkdir -p .build/api-image/ui
python3 - <<'PY'
from pathlib import Path
import shutil
root = Path.cwd().resolve()
staging = root / '.build/api-image/ui'
if staging.is_symlink() or staging.resolve() != root / '.build/api-image/ui':
    raise SystemExit('Refusing unexpected UI build staging path')
shutil.rmtree(staging)
shutil.copytree(root / 'dashboard/dist', staging)
PY
