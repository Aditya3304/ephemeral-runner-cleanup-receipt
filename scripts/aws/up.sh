#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export PATH="$PWD/.build/node-v24.20.0-linux-x64/bin:/usr/local/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0
[[ $(id -u) == 0 ]] || { echo 'Run on the dedicated AWS operator host as root'; exit 1; }
test -s .build/aws-resources.json
bash dev prepare
bash dev up
bash scripts/prepare-cli.sh
bash scripts/ci-prepare.sh
bash scripts/policy-up.sh
bash scripts/aws/runner-build.sh
bash scripts/sigstore-prepare.sh
bash scripts/sigstore-up.sh
bash scripts/finalizer-build.sh
# AWS profile has no MinIO or KES process. This bridge connects only trusted services to S3.
docker network inspect cleanup-receipt-archive >/dev/null 2>&1 || docker network create cleanup-receipt-archive
python3 scripts/aws/sessions.py
mkdir -p .build/finalizer-trust .build/finalized .build/coordinator
chown -R 65532:65532 .build/finalizer-trust .build/finalized
base=postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0
docker run --rm --network none --user 65532:65532 -v cleanup-receipt-sigstore_sigstore-trust:/trust:ro -v "$PWD/.build/finalizer-trust:/export" --entrypoint bash "$base" -ec 'cp /trust/trusted-root.json /trust/signing-config.json /trust/issuer-ca.crt /export/'
python3 scripts/finalizer-config.py
export FINALIZER_IMAGE=$(cat .build/finalizer-image-id)
docker compose -f compose.finalizer.yaml run --rm --no-deps init-state
(cd dashboard && npm ci --workspaces=false --ignore-scripts --no-audit --no-fund)
bash scripts/api-build.sh
python3 scripts/aws/config-api.py
export API_RECREATE=1
bash scripts/api-up.sh
python3 scripts/aws/install-services.py
echo 'AWS services ready; source-approved GitHub controller enabled.'
