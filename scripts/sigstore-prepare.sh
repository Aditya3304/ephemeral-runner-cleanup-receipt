#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export PATH="/usr/local/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0
mkdir -p .build/sigstore
printf '%s  %s\n' 4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71 .build/cosign | sha256sum --check
while IFS= read -r image; do docker pull "$image"; done < <(docker compose -f compose.sigstore.yaml --profile tools config --images | sort -u)
GOPROXY=off GOSUMDB=off go build -p 2 -trimpath -o .build/sigstore-http ./infra/sigstore/http.go
GOPROXY=off GOSUMDB=off go build -p 2 -trimpath -o .build/localissuer ./cmd/localissuer
echo 'Pinned images and local issuer prepared. Runtime startup requires no downloads.'
