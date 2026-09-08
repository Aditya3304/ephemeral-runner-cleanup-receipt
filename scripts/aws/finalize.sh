#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
[[ "${1:-}" =~ ^[0-9a-f]{32}$ ]]
# Permit only the isolated read-only finalizer UID to traverse published input.
chgrp 65532 .build/coordinator
chmod 0710 .build/coordinator
chgrp -R 65532 ".build/coordinator/$1"
find ".build/coordinator/$1" -type d -exec chmod 0750 {} +
find ".build/coordinator/$1" -type f -exec chmod 0640 {} +
bash scripts/finalizer-run.sh "$1"
export API_IMAGE=$(cat .build/api-image-id)
docker compose -f compose.api.yaml run --rm --no-deps deliver /usr/local/bin/deliver all
