set -eu
test -f /opt/cleanup/bootstrap-ready
cd /opt/cleanup
curl -fsSL https://go.dev/dl/?mode=json > go-downloads.json
python3 - <<'PY'
import json,subprocess,hashlib
for release in json.load(open('go-downloads.json')):
 if release['version']=='go1.27.1':
  f=next(f for f in release['files'] if f['os']=='linux' and f['arch']=='amd64' and f['kind']=='archive')
  subprocess.run(['curl','-fL','--retry','3','https://go.dev/dl/'+f['filename'],'-o','go.tar.gz'],check=True)
  assert hashlib.sha256(open('go.tar.gz','rb').read()).hexdigest()==f['sha256']
  subprocess.run(['tar','-C','/usr/local','-xzf','go.tar.gz'],check=True)
  break
else: raise SystemExit('Pinned Go release unavailable')
PY
curl -fL --retry 3 https://dl.k8s.io/release/v1.36.1/bin/linux/amd64/kubectl -o kubectl
curl -fsSL https://dl.k8s.io/release/v1.36.1/bin/linux/amd64/kubectl.sha256 -o kubectl.sha256
echo "$(cat kubectl.sha256)  kubectl" | sha256sum -c -
install -m 0755 kubectl /usr/local/bin/kubectl
/usr/local/go/bin/go version
docker version --format '{{.Server.Version}}'
df -h /opt/cleanup
