#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
bash scripts/node-prepare.sh
export PATH="$PWD/.build/node-v24.20.0-linux-x64/bin:/usr/local/go/bin:$PATH"
export GOTOOLCHAIN=local CGO_ENABLED=0
# Install only this milestone's workspace; no lifecycle scripts from dependencies.
npm --prefix action ci --workspaces=false --ignore-scripts --no-audit --no-fund --cache "$PWD/.cache/npm"
go mod download
docker pull node:24.20.0-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e
bash scripts/kind-up.sh
bash scripts/policy-prepare.sh
bash scripts/runner-build.sh
