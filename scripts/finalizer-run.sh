#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
run=${1:-}
[[ "$run" =~ ^[0-9a-f]{32}$ ]] || { echo 'Usage: bash scripts/finalizer-run.sh <local-run-id>' >&2; exit 1; }
[[ -f .build/coordinator/$run/run.json ]] || { echo 'Local coordinator run not found.' >&2; exit 1; }
export FINALIZER_IMAGE=$(cat .build/finalizer-image-id)
[[ "$FINALIZER_IMAGE" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'Build the finalizer first.' >&2; exit 1; }
[[ "$(docker image inspect --format '{{.Id}}' "$FINALIZER_IMAGE")" == "$FINALIZER_IMAGE" ]]
base=postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0
mkdir -p .build/finalizer-trust .build/finalized
# Only public trust leaves the operator volume. Identity/archive secrets are
# mounted into the finalizer container and never copied to the checkout.
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
  --user 65532:65532 --memory 64m --entrypoint bash \
  -v cleanup-receipt-sigstore_sigstore-trust:/trust:ro \
  -v "$PWD/.build/finalizer-trust:/export" "$base" -ec \
  'cp /trust/trusted-root.json /trust/signing-config.json /trust/issuer-ca.crt /export/'
python3 scripts/finalizer-config.py
dc=(docker compose -f compose.finalizer.yaml)
"${dc[@]}" run --rm --no-deps init-state
"${dc[@]}" run --rm --no-deps finalizer --config /config/finalizer.json --run "$run"
# Export only completed published receipts, never the private retry snapshots.
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
  --user 65532:65532 --memory 128m --entrypoint bash \
  -v cleanup-receipt-finalizer_output:/output:ro \
  -v "$PWD/.build/finalized:/export" "$base" -ec \
  'for dir in /output/*; do [[ -f "$dir/result.json" ]] || continue; name=${dir##*/}; mkdir -p "/export/$name"; cp "$dir/receipt.json" "$dir/receipt.bundle.json" "$dir/result.json" "/export/$name/"; done'
echo "Finalization complete for $run. Published local copies: .build/finalized/"
