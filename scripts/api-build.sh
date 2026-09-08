#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export PATH="/usr/local/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0 GOPROXY=off GOSUMDB=off
mkdir -p .build/api-image
cp /etc/ssl/certs/ca-certificates.crt .build/api-image/ca-certificates.crt
bash scripts/dashboard-build.sh
go build -p 2 -trimpath -o .build/api-image/api ./cmd/api
go build -p 2 -trimpath -o .build/api-image/deliver ./cmd/deliver
go build -p 2 -trimpath -o .build/api-image/recoverybridge ./cmd/recoverybridge
go build -p 2 -trimpath -o .build/api-image/gateway ./cmd/gateway
echo '4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71  .build/cosign' | sha256sum -c -
cp .build/cosign .build/api-image/cosign
docker build --network=none --pull=false -f infra/api.Dockerfile -t cleanup-receipt/api:local .build/api-image
docker image inspect --format '{{.Id}}' cleanup-receipt/api:local > .build/api-image-id
