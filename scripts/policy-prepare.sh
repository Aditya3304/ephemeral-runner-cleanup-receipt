#!/usr/bin/env bash
# Online setup only: cache the pinned image in Docker and the EXISTING kind cluster.
# Preparation does not certify host isolation; policy-up reports engine exemptions.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."

for tool in docker kubectl; do
  command -v "$tool" >/dev/null || { echo "Missing setup prerequisite: $tool" >&2; exit 1; }
done
[[ -x .build/kind && -s .build/kubeconfig ]] || {
  echo 'Existing .build/kind and .build/kubeconfig are required.' >&2; exit 1;
}
kube=(kubectl --kubeconfig .build/kubeconfig --context kind-cleanup-receipt)
nodes=$(.build/kind get nodes --name cleanup-receipt)
[[ -n "$nodes" ]] || { echo 'Existing cleanup-receipt cluster not found.' >&2; exit 1; }
while IFS= read -r node; do
  [[ "$("${kube[@]}" get node "$node" -o jsonpath='{.status.nodeInfo.architecture}')" == amd64 ]] || {
    echo "The pinned policy image supports only linux/amd64: $node" >&2; exit 1;
  }
done <<< "$nodes"

# The local manifest is the single source of truth for the immutable image.
image=$(awk '$1 == "image:" {print $2}' infra/network-policy.yaml)
[[ "$image" =~ ^docker.io/cloudnativelabs/kube-router:v2\.10\.0@sha256:[0-9a-f]{64}$ ]] || {
  echo 'Expected one digest-pinned kube-router v2.10.0 image in the local manifest.' >&2; exit 1;
}
tag=${image%@*}
digest=${image##*@}
reference=${tag%:*}@$digest
if ! docker image inspect "$reference" >/dev/null 2>&1; then
  docker pull --platform linux/amd64 "$reference"
fi
docker image inspect "$reference" --format '{{range .RepoDigests}}{{println .}}{{end}}' |
  grep -Fx "${reference#docker.io/}" >/dev/null || {
    echo 'Docker cache does not match the pinned repository digest.' >&2; exit 1;
  }
docker tag "$reference" "$tag"
.build/kind load docker-image "$tag" --name cleanup-receipt
while IFS= read -r node; do
  # kind imports the tag; register the verified content under its CRI digest too.
  loaded=$(docker exec "$node" ctr --namespace=k8s.io images list |
    awk -v image="$tag" '$1 == image {print $3}')
  [[ "$loaded" == "$digest" ]] || {
    echo "Unexpected policy image digest in $node: $loaded" >&2; exit 1;
  }
  docker exec "$node" ctr --namespace=k8s.io images tag --force "$tag" "$reference" >/dev/null
  docker exec "$node" crictl inspecti "$image" >/dev/null
done <<< "$nodes"

bash scripts/policy-up.sh
