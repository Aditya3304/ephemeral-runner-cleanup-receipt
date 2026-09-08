#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
mkdir -p .build/aws-tools
curl -fsSL 'https://go.dev/dl/?mode=json' > .build/aws-tools/go-releases.json
python3 - <<'PY'
import json,hashlib,pathlib,subprocess
base=pathlib.Path('.build/aws-tools')
for release in json.loads((base/'go-releases.json').read_text()):
    if release['version']!='go1.27.1':continue
    f=next(f for f in release['files'] if f['os']=='linux' and f['arch']=='amd64' and f['kind']=='archive')
    archive=base/'go.tar.gz'
    subprocess.run(['curl','-fL','--retry','3','https://go.dev/dl/'+f['filename'],'-o',str(archive)],check=True)
    if hashlib.sha256(archive.read_bytes()).hexdigest()!=f['sha256']:raise SystemExit('Go checksum mismatch')
    subprocess.run(['tar','-C','/usr/local','-xzf',str(archive)],check=True)
    break
else:raise SystemExit('Pinned Go release not in official release metadata; review before changing version')
PY
curl -fL --retry 3 https://dl.k8s.io/release/v1.36.1/bin/linux/amd64/kubectl -o .build/aws-tools/kubectl
curl -fsSL https://dl.k8s.io/release/v1.36.1/bin/linux/amd64/kubectl.sha256 -o .build/aws-tools/kubectl.sha256
printf '%s  %s\n' "$(cat .build/aws-tools/kubectl.sha256)" .build/aws-tools/kubectl | sha256sum -c -
install -m 0755 .build/aws-tools/kubectl /usr/local/bin/kubectl
