#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export PATH="$PWD/.build/node-v24.20.0-linux-x64/bin:/usr/local/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0
mkdir -p .build/actions-runner
curl -fL --retry 3 https://github.com/actions/runner/releases/download/v2.337.0/actions-runner-linux-x64-2.337.0.tar.gz -o .build/actions-runner.tar.gz
echo '70920811a4f8ad4328818682bca5c6469c1c942fab52448868071d0063816613  .build/actions-runner.tar.gz' | sha256sum -c -
tar -xzf .build/actions-runner.tar.gz -C .build/actions-runner
go build -p 2 -trimpath -o .build/githubci ./cmd/githubci
tag="cleanup-receipt/runner:github-$(git rev-parse --short=16 HEAD)"
docker build --provenance=false -f infra/github/runner.Dockerfile -t "$tag" .
.build/kind load docker-image "$tag" --name cleanup-receipt
manifest=$(docker exec cleanup-receipt-control-plane ctr --namespace k8s.io images list | awk -v ref="docker.io/$tag" '$1==ref {print $3}')
[[ "$manifest" =~ ^sha256:[0-9a-f]{64}$ ]]
reference="cleanup-receipt/runner@$manifest"
docker exec cleanup-receipt-control-plane ctr --namespace k8s.io images tag --force "docker.io/$tag" "docker.io/$reference"
printf '%s\n' "$reference" > .build/github-runner-image
