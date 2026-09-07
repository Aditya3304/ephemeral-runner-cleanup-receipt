#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
image='kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5'
docker image inspect "$image" >/dev/null
node=cleanup-receipt-control-plane
if docker container inspect "$node" >/dev/null 2>&1; then
  [[ "$(docker container inspect -f '{{index .Config.Labels "io.x-k8s.kind.cluster"}}' "$node")" == cleanup-receipt ]]
  [[ "$(docker container inspect -f '{{index .Config.Labels "io.x-k8s.kind.role"}}' "$node")" == control-plane ]]
  if [[ "$(docker container inspect -f '{{.State.Running}}' "$node")" != true ]]; then
    docker start "$node" >/dev/null
  fi
  echo 'Existing project cluster preserved.'
else
  .build/kind create cluster --name cleanup-receipt --image "$image" --config infra/kind.yaml --kubeconfig .build/kubeconfig --wait 120s
fi
.build/kind export kubeconfig --name cleanup-receipt --kubeconfig .build/kubeconfig
for _ in {1..60}; do
  if kubectl --kubeconfig .build/kubeconfig get --raw=/readyz >/dev/null 2>&1; then break; fi
  sleep 1
done
kubectl --kubeconfig .build/kubeconfig get --raw=/readyz >/dev/null
kubectl --kubeconfig .build/kubeconfig get nodes
