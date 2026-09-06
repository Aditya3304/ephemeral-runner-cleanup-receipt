#!/usr/bin/env bash
# Offline startup: use only local tools, the local manifest, and cached node images.
# Readiness confirms controller health, not a traffic-level policy conformance test.
# v2.10.0 has no flags to disable the NodePort-range and ICMP exemptions.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."

for tool in docker kubectl; do
  command -v "$tool" >/dev/null || { echo "Missing runtime prerequisite: $tool" >&2; exit 1; }
done
[[ -x .build/kind && -s .build/kubeconfig ]] || {
  echo 'Existing .build/kind and .build/kubeconfig are required.' >&2; exit 1;
}
kube=(kubectl --kubeconfig .build/kubeconfig --context kind-cleanup-receipt)
nodes=$(.build/kind get nodes --name cleanup-receipt)
[[ -n "$nodes" ]] || { echo 'Existing cleanup-receipt cluster not found.' >&2; exit 1; }
image=$(awk '$1 == "image:" {print $2}' infra/network-policy.yaml)
[[ "$image" =~ ^docker.io/cloudnativelabs/kube-router:v2\.10\.0@sha256:[0-9a-f]{64}$ ]] || {
  echo 'Expected one digest-pinned kube-router v2.10.0 image in the local manifest.' >&2; exit 1;
}
while IFS= read -r node; do
  [[ "$("${kube[@]}" get node "$node" -o jsonpath='{.status.nodeInfo.architecture}')" == amd64 ]] || {
    echo "The pinned policy image supports only linux/amd64: $node" >&2; exit 1;
  }
  docker exec "$node" crictl inspecti "$image" >/dev/null 2>&1 || {
    echo "Policy image missing in $node. Run bash scripts/policy-prepare.sh first." >&2; exit 1;
  }
done <<< "$nodes"
# Verify the expected existing network before applying only our four resources.
"${kube[@]}" -n kube-system get daemonset kindnet kube-proxy >/dev/null
"${kube[@]}" apply -f infra/network-policy.yaml
"${kube[@]}" apply -f infra/guard-admission.yaml
"${kube[@]}" -n kube-system rollout status daemonset/proof-network-policy-controller --timeout=180s
echo 'Firewall controller ready; kindnet and kube-proxy preserved. No runtime downloads.'
echo 'LIMITATION: kube-router v2.10.0 exempts node-local ports 30000-32767 and some ICMP traffic from pod policy checks.' >&2
echo 'API-only runner isolation requires an additional isolation profile; controller readiness is not proof of host isolation.' >&2
