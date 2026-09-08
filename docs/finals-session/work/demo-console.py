"""One-command scenario console. Calls the existing validated orchestration."""
import datetime,json,pathlib,subprocess,sys
from evidence_view import verify
ROOT=pathlib.Path(__file__).resolve().parent.parent
ledger=ROOT/'outputs/walkthrough-execution/runs.json'
for scenario in ['normal','test-failure','cleanup-blocked','normal']:
    before=len(json.loads(ledger.read_text())) if ledger.exists() else 0
    print('\n'+'='*60+'\nSCENARIO: '+scenario+'\n'+'='*60,flush=True)
    subprocess.run([sys.executable,'-u',str(ROOT/'work/run-walkthrough.py'),scenario],cwd=ROOT,check=True)
    rows=json.loads(ledger.read_text());assert len(rows)==before+1
    row=rows[-1];run=row['run_id']
    live=json.loads((ledger.parent/(run+'-live.json')).read_text())
    print('\nCAPTURED WHILE THE POD WAS RUNNING:',json.dumps(live,indent=2),flush=True)
    jobs=json.loads((ledger.parent/(run+'-jobs.json')).read_text())
    print('\nGITHUB STEPS:',flush=True)
    for step in jobs['jobs'][0]['steps']:print(step['conclusion'],':',step['name'],flush=True)
    verify(row['receipt']['id'])
print('\nALL FOUR SCENARIOS COMPLETED AND VERIFIED.\nThe last normal receipt should show Clean. The cleanup-blocked receipt should show Needs attention.',flush=True)
