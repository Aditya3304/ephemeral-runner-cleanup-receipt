#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
for binary in cosign localissuer sigstore-http; do [[ -x .build/$binary ]] || { echo "Run bash scripts/sigstore-prepare.sh first: $binary missing" >&2; exit 1; }; done
printf '%s  %s\n' 4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71 .build/cosign | sha256sum --check
dc=(docker compose -f compose.sigstore.yaml)
"${dc[@]}" run --rm --no-deps bootstrap
"${dc[@]}" up -d --pull never issuer ctlog rekor tsa fulcio
"${dc[@]}" run --rm --no-deps client /scripts/sigstore-check.sh ready
