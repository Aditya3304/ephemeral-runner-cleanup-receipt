#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export PATH="$PWD/.build/node-v24.20.0-linux-x64/bin:/usr/local/go/bin:$PATH"
export GOTOOLCHAIN=local CGO_ENABLED=0 GOPROXY=off GOSUMDB=off
go build -p 2 -trimpath -o .build/proofctl ./cmd/proofctl
go build -p 2 -trimpath -o .build/localci ./cmd/localci
go build -p 2 -trimpath -o .build/netgate ./cmd/netgate
go build -p 2 -trimpath -o .build/observe ./cmd/observe
# Install the trusted helper only in this project's kind node. It pins and
# configures a verified runner network namespace; it never changes host rules.
docker cp .build/netgate cleanup-receipt-control-plane:/usr/local/bin/proof-netgate
docker exec cleanup-receipt-control-plane chmod 0555 /usr/local/bin/proof-netgate
docker cp .build/observe cleanup-receipt-control-plane:/usr/local/bin/proof-observe
docker exec cleanup-receipt-control-plane chmod 0555 /usr/local/bin/proof-observe
npm run build --workspace action --offline
digest=$(find action/dist examples -type f -print0 | sort -z | xargs -0 sha256sum; sha256sum .build/proofctl infra/runner.Dockerfile)
tag="cleanup-receipt/runner:$(printf '%s' "$digest" | sha256sum | cut -c1-32)"
docker image inspect node:24.20.0-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e >/dev/null
BUILDX_GIT_INFO=false docker build --network=none --pull=false --provenance=false -f infra/runner.Dockerfile -t "$tag" .
.build/kind load docker-image "$tag" --name cleanup-receipt
manifest=$(docker exec cleanup-receipt-control-plane ctr --namespace k8s.io images list | awk -v ref="docker.io/$tag" '$1==ref {print $3}')
[[ "$manifest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'Could not resolve preloaded manifest digest' >&2; exit 1; }
reference="cleanup-receipt/runner@$manifest"
docker exec cleanup-receipt-control-plane ctr --namespace k8s.io images tag --force "docker.io/$tag" "docker.io/$reference"
printf '%s\n' "$reference" > .build/runner-image
printf 'Prepared local runner %s\n' "$reference"
