#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
image=postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0
# Test process uses the API's actual network namespace, without its credentials.
docker run --rm --pull=never --network container:cleanup-receipt-api-api-1 --user 65532:65532 \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  -e PROOF_API_NETWORK=1 -v "$PWD/.build/api.test:/test:ro" \
  --entrypoint /test "$image" -test.v -test.run '^TestAPINetworkIsolation$' -test.timeout 30s
# Authenticated delivery client sees only the delivery network and its own token.
docker run --rm --pull=never --network cleanup-receipt-api_delivery --user 65532:65532 \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  -e PROOF_API_HTTP=1 -v "$PWD/.build/api.test:/test:ro" \
  -v "$PWD/.build/finalized:/fixtures:ro" \
  -v cleanup-receipt-api_auth:/run/auth:ro \
  --entrypoint /test "$image" -test.v -test.run '^TestLiveHTTP$' -test.timeout 120s
