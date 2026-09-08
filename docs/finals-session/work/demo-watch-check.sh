set -eu
cd /opt/cleanup/repo
python3 scripts/aws/watch-demo.py --once
docker exec --user postgres cleanup-receipt-db-1 psql --no-psqlrc -d proof -c 'SELECT run_id,run_attempt,verdict FROM evidence.receipts ORDER BY run_id DESC,run_attempt DESC LIMIT 10;'
systemctl list-timers cleanup-demo-stop.timer --all --no-pager
python3 - <<'PY'
import json,pathlib,subprocess
rows=[json.loads(p.read_text()) for p in pathlib.Path('.build/coordinator').glob('*/run.json')]
r=max(rows,key=lambda x:x['created_at'])
if r['phase']=='running':
    cmd=['kubectl','--kubeconfig','.build/kubeconfig','exec','-n',r['runner_namespace'],r['pod_name'],'-c','guard','--']
    for command in [['ls','-la','/data/proof-'+r['token']+'/workspace'],['cat','/data/proof-'+r['token']+'/workspace/export-summary.json']]:
        subprocess.run(cmd+command,check=False)
PY
