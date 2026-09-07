#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export PATH="/usr/local/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0 GOPROXY=off GOSUMDB=off
go test -p 2 -count=1 ./internal/api ./internal/delivery ./internal/finalizer ./cmd/api ./cmd/deliver
go test -p 2 -c -o .build/api.test ./internal/api
# Only this isolated validation process gets admin credentials, solely to create
# and remove its randomly named test DB. The API service never mounts them.
docker run --rm --pull=never --network cleanup-receipt_metadata --network cleanup-receipt-archive \
  --read-only --cap-drop ALL --cap-add DAC_OVERRIDE --security-opt no-new-privileges --tmpfs /tmp:rw,size=512m \
  -e PROOF_API_REAL=1 -e GOMEMLIMIT=512MiB -e GOMAXPROCS=2 --memory 768m \
  -v "$PWD/.build/api.test:/test:ro" \
  -v "$PWD/.build/cosign:/usr/local/bin/cosign:ro" \
  -v "$PWD/.build/api-config.json:/config/api.json:ro" \
  -v "$PWD/.build/finalized:/fixtures:ro" \
  -v cleanup-receipt_admin-secrets:/run/admin:ro \
  -v cleanup-receipt_api-secrets:/run/api:ro \
  -v cleanup-receipt-archive_verifier:/run/archive-verifier:ro \
  -v cleanup-receipt-archive_public:/run/archive-public:ro \
  -v cleanup-receipt-sigstore_sigstore-trust:/trust:ro \
  --entrypoint /test postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0 \
  -test.v -test.run TestRealIngestion -test.timeout 240s
