#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
mkdir -p .build
fetch() {
  local name=$1 version=$2 digest=$3 url=$4
  if [[ -f .build/$name ]] && printf '%s  %s\n' "$digest" ".build/$name" | sha256sum --check --status; then return; fi
  curl --fail --location --retry 3 --output ".build/$name.download" "$url"
  printf '%s  %s\n' "$digest" ".build/$name.download" | sha256sum --check
  mv -- ".build/$name.download" ".build/$name"
  chmod +x ".build/$name"
  printf 'Prepared %s %s\n' "$name" "$version"
}
fetch kind v0.32.0 50030de23cf40a18505f20426f6a8506bedf13c6e509244bd1fa9463721b0f54 https://github.com/kubernetes-sigs/kind/releases/download/v0.32.0/kind-linux-amd64
fetch cosign v3.1.3 4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71 https://github.com/sigstore/cosign/releases/download/v3.1.3/cosign-linux-amd64
docker pull kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5
export PATH="/usr/local/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0
go mod download
go build -p 2 -trimpath -o .build/proofctl ./cmd/proofctl
