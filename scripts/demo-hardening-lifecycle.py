#!/usr/bin/env python3
"""Fresh end-to-end success, failure, cancellation, deletion, restart and isolation."""
import json, pathlib, subprocess, time
from hardening_lib import ROOT, deliver, get, parse_delivery, receipt_for, utc_now, verify_receipt

cases=['pass','fail','cancel','missing-post','restart','isolation']
before=set((ROOT/'.build').glob('finalizer-validation-*.json'))
process=subprocess.run(['python3','scripts/demo-finalizer.py',*cases],cwd=ROOT,text=True)
if process.returncode: raise SystemExit(process.returncode)
created=set((ROOT/'.build').glob('finalizer-validation-*.json'))-before
if len(created)!=1: raise SystemExit('Expected one finalizer validation report')
source=json.loads(next(iter(created)).read_text())
results=[]
for item in source['results']:
    outcome=parse_delivery(deliver(item['run_id']))
    row=receipt_for(item['run_id'])
    verification=verify_receipt(row['id'])
    ledger=json.loads((ROOT/'.build/coordinator'/item['run_id']/'run.json').read_text())
    assert ledger['resource_absent'] and ledger['runner_absent']
    assert outcome['receipt_id']==row['id']
    assert (row['incident'] is None)==(row['verdict']=='pass')
    if item['case']=='missing-post':
        assert row['verdict']=='fail' and row['incident']['state']=='open'
    else: assert row['verdict']=='pass'
    results.append({'case':item['case'],'run_id':item['run_id'],'receipt_id':row['id'],'verdict':row['verdict'],'incident_id':row['incident']['id'] if row['incident'] else None,'resources_absent':True,'runner_absent':True,'signature':verification['signature'],'artifacts':verification['artifacts']})
report={'kind':'hardening-lifecycle/v1','checked_at':utc_now(),'results':results,'fresh_receipts':len(results),'fresh_incidents':sum(1 for r in results if r['incident_id'])}
(ROOT/'.build/hardening-lifecycle.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
