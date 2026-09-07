#!/usr/bin/env python3
"""Real MinIO, signing and PostgreSQL outages with durable watchdog recovery."""
import json, subprocess
from hardening_lib import ROOT, command, get, new_run, receipt_for, utc_now, verify_receipt, wait_api, wait_due, watchdog_once, watchdog_state

if command(['systemctl','--user','is-active','--quiet','cleanup-receipt-watchdog.service'],check=False).returncode==0:
    raise SystemExit('Stop the watchdog service before controlled faults; restart it afterward.')

results=[]
def recover_after_service_fault(name, stop, restore, expected_stage):
    run=new_run('pass'); run_id=run['run_id']
    command(stop)
    try:
        failed=watchdog_once(run_id)
        assert failed.returncode==0
        state=watchdog_state(json.loads((ROOT/'.build/coordinator'/run_id/'run.json').read_text())['identity'])
        assert state['current']['result']=='retry' and state['current']['stage']==expected_stage
        assert get('/v1/receipts?run_id='+run_id)[1]['items']==[]
    finally: command(restore)
    wait_api()
    early=watchdog_once(run_id); assert early.returncode==0
    unchanged=watchdog_state(state['identity']); assert unchanged['current']['attempt_number']==1
    wait_due(state['current']['next_retry_at'])
    recovered=watchdog_once(run_id); assert recovered.returncode==0
    final=watchdog_state(state['identity']); assert final['current']['result']=='succeeded' and final['current']['attempt_number']==2
    row=receipt_for(run_id); verification=verify_receipt(row['id'])
    results.append({'fault':name,'run_id':run_id,'receipt_id':row['id'],'failed_stage':expected_stage,'first_attempt':'retry','early_replay_spent_attempt':False,'recovered_attempt':2,'verdict':row['verdict'],'signature':verification['signature'],'artifacts':verification['artifacts']})

recover_after_service_fault('object_store_unavailable',['docker','compose','-f','compose.archive.yaml','stop','minio'],['bash','scripts/archive-up.sh'],'archive')
recover_after_service_fault('signing_issuer_unavailable',['docker','compose','-f','compose.sigstore.yaml','stop','issuer'],['docker','compose','-f','compose.sigstore.yaml','start','issuer'],'sign')

run=new_run('pass'); run_id=run['run_id']
command(['bash','scripts/finalizer-run.sh',run_id],timeout=240)
command(['python3','scripts/finalizer-verify.py',run_id],timeout=240)
command(['docker','compose','stop','db'])
try:
    assert get('/readyz')[0]==503
    failed=watchdog_once(run_id); assert failed.returncode==0
    identity=json.loads((ROOT/'.build/coordinator'/run_id/'run.json').read_text())['identity']
    state=watchdog_state(identity)
    assert state['current']['result']=='retry' and state['current']['stage']=='ingest'
finally:
    command(['docker','compose','up','-d','--wait','db'],timeout=180)
    command(['docker','compose','-f','compose.api.yaml','restart','api'],timeout=180)
wait_api()
early=watchdog_once(run_id); assert early.returncode==0 and watchdog_state(identity)['current']['attempt_number']==1
wait_due(state['current']['next_retry_at'])
assert watchdog_once(run_id).returncode==0
final=watchdog_state(identity); assert final['current']['result']=='succeeded' and final['current']['attempt_number']==2
row=receipt_for(run_id); verification=verify_receipt(row['id'])
results.append({'fault':'postgresql_unavailable','run_id':run_id,'receipt_id':row['id'],'failed_stage':'ingest','signed_objects_preserved':True,'early_replay_spent_attempt':False,'recovered_attempt':2,'verdict':row['verdict'],'signature':verification['signature'],'artifacts':verification['artifacts']})

report={'kind':'hardening-service-faults/v1','checked_at':utc_now(),'results':results}
(ROOT/'.build/hardening-service-faults.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
