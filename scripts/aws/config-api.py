#!/usr/bin/env python3
"""Install the operator-built finalizer policy during an explicit AWS deployment.

Preserve the previous public configuration; never rewrite historical receipts.
"""
import hashlib,json,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parents[2]
path=root/'.build/api-config.json'
finalizer=json.loads((root/'.build/finalizer-config.json').read_text())
revision=(root/'.build/finalizer-revision').read_text().strip()
if finalizer['policy']['revision']!=revision:raise SystemExit('Built finalizer policy mismatch')
if path.exists():
    previous=path.read_bytes();config=json.loads(previous)
    if config['policy']!=finalizer['policy']:
        history=root/'.build/api-policy-history';history.mkdir(exist_ok=True)
        (history/(hashlib.sha256(previous).hexdigest()+'.json')).write_bytes(previous)
        config['policy']=finalizer['policy'];path.write_text(json.dumps(config,indent=2)+'\n')
subprocess.run(['python3',str(root/'scripts/api-config.py')],check=True)
