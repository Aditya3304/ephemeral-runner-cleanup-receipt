#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
image='kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5'
docker image inspect "$image" >/dev/null
if .build/kind get clusters | grep -Fxq cleanup-receipt; then
  echo 'Existing project cluster preserved.'
else
  .build/kind create cluster --name cleanup-receipt --image "$image" --config infra/kind.yaml --kubeconfig .build/kubeconfig --wait 120s
fi
.build/kind export kubeconfig --name cleanup-receipt --kubeconfig .build/kubeconfig
kubectl --kubeconfig .build/kubeconfig get nodes
