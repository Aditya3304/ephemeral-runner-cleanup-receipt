#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export PATH="/usr/local/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0 GOPROXY=off GOSUMDB=off
revision=$(git rev-parse HEAD)
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || { echo 'A committed source revision is required.' >&2; exit 1; }
if [[ -n "$(git status --porcelain --untracked-files=all -- cmd internal go.mod go.sum infra/finalizer.Dockerfile scripts/finalizer-build.sh)" ]]; then
  echo 'Commit finalizer source and build inputs before building an attesting image.' >&2
  exit 1
fi
printf '%s  %s\n' 4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71 .build/cosign | sha256sum --check
mkdir -p .build/finalizer-image
go build -p 2 -trimpath -ldflags "-X main.buildRevision=$revision" -o .build/finalizer-image/finalizer ./cmd/finalizer
go build -p 2 -trimpath -o .build/finalizer-image/proofctl ./cmd/proofctl
go build -p 2 -trimpath -o .build/finalizer-image/archivectl ./cmd/archivectl
install -m 0555 .build/cosign .build/finalizer-image/cosign
cp infra/finalizer.Dockerfile .build/finalizer-image/Dockerfile
tag="cleanup-receipt/finalizer:$(sha256sum .build/finalizer-image/{finalizer,proofctl,archivectl,cosign,Dockerfile} | sha256sum | cut -c1-32)"
BUILDX_GIT_INFO=false docker build --network=none --pull=false --provenance=false -t "$tag" .build/finalizer-image
docker image inspect --format '{{.Id}}' "$tag" > .build/finalizer-image-id
printf '%s\n' "$tag" > .build/finalizer-image-tag
printf '%s\n' "$revision" > .build/finalizer-revision
echo "Built isolated finalizer for source $revision. No runtime checkout or setup downloads."
