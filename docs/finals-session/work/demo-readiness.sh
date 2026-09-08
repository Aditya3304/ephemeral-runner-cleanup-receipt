set -eu
cd /opt/cleanup/repo
git rev-parse HEAD
systemctl is-active cleanup-stack cleanup-github cleanup-github-proxy cleanup-github-broker cleanup-metadata-block
docker ps --format '{{.Names}} {{.Status}}'
kubectl --kubeconfig .build/kubeconfig get nodes
kubectl --kubeconfig .build/kubeconfig get pods -A
curl -fsS http://127.0.0.1:8080/readyz
python3 - <<'PY'
import pathlib,json
for p in sorted(pathlib.Path('.build/coordinator').glob('*/run.json')):
    d=json.loads(p.read_text())
    print(json.dumps({k:d.get(k) for k in ['identity','token','phase','resource_absent','runner_absent','github_assignment_confirmed','github_runner_absent']}))
PY
