#!/usr/bin/env bash
# Prepare shared trusted finalizer state without requiring a local CI run.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export FINALIZER_IMAGE=$(cat .build/finalizer-image-id)
[[ "$FINALIZER_IMAGE" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'Build the finalizer first.' >&2; exit 1; }
[[ "$(docker image inspect --format '{{.Id}}' "$FINALIZER_IMAGE")" == "$FINALIZER_IMAGE" ]]
base=postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0
mkdir -p .build/finalizer-trust .build/finalized
# Export only these public files. Host-side tar writes as the operator, so this
# also works on Linux bind mounts where uid 65532 cannot write host-owned dirs.
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
  --user 65532:65532 --memory 64m --entrypoint tar \
  -v cleanup-receipt-sigstore_sigstore-trust:/trust:ro "$base" \
  -C /trust -cf - trusted-root.json signing-config.json issuer-ca.crt | \
  tar -C .build/finalizer-trust -xf -
python3 scripts/finalizer-config.py
docker compose -f compose.finalizer.yaml run --rm --no-deps init-state
