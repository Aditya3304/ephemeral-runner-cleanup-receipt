#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
mkdir -p .build
exec 9>.build/release-lifecycle.lock
flock -w 30 9 || { echo 'Another release lifecycle command is active.' >&2; exit 1; }

systemctl --user stop cleanup-receipt-watchdog.service 2>/dev/null || true
systemctl --user stop cleanup-receipt-github-watchdog.service 2>/dev/null || true
if [[ -s .build/api-image-id ]]; then
  export API_IMAGE="$(cat .build/api-image-id)"
  docker compose -f compose.api.yaml stop gateway api
fi
docker compose -f compose.sigstore.yaml stop fulcio tsa rekor ctlog issuer
bash scripts/archive-stop.sh
docker compose stop db

node=cleanup-receipt-control-plane
if docker container inspect "$node" >/dev/null 2>&1; then
  [[ "$(docker container inspect -f '{{index .Config.Labels "io.x-k8s.kind.cluster"}}' "$node")" == cleanup-receipt ]]
  [[ "$(docker container inspect -f '{{index .Config.Labels "io.x-k8s.kind.role"}}' "$node")" == control-plane ]]
  docker stop "$node" >/dev/null
fi
echo 'Local release stopped. Database, archive, signing, cluster, and recovery state were preserved.'
