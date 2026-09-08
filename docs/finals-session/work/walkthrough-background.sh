set -eu
cd /opt/cleanup/repo
python3 - <<'PY'
import datetime,json,pathlib,subprocess
def command(args):
    p=subprocess.run(args,capture_output=True,text=True,timeout=30)
    if p.returncode:raise RuntimeError(p.stderr)
    return p.stdout
print('Checked at',datetime.datetime.now(datetime.timezone.utc).isoformat())
print(command(['docker','events','--since','2026-09-08T10:24:00Z','--until',datetime.datetime.now(datetime.timezone.utc).isoformat(),'--filter','type=container','--filter','event=start','--filter','event=die','--format','{{.Time}} {{.Action}} {{.Actor.Attributes.name}}']))
print(command(['docker','exec','--user','postgres','cleanup-receipt-db-1','psql','--no-psqlrc','-d','proof','-c','SELECT run_id,run_attempt,verdict FROM evidence.receipts ORDER BY run_id DESC,run_attempt DESC LIMIT 20;']))
rows=json.loads(command(['kubectl','--kubeconfig','.build/kubeconfig','get','namespaces','-o','json']))['items']
assert not [r for r in rows if r['metadata']['name'].startswith('proof-')]
print('No ephemeral namespaces remain')
PY
