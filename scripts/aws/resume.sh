#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
# Reuse prepared binaries, images, database and private trust; no rebuild/download.
export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
bash dev up
bash scripts/kind-up.sh
bash scripts/policy-up.sh
python3 scripts/aws/sessions.py
bash scripts/sigstore-up.sh
bash scripts/api-up.sh
