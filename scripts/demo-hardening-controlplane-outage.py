#!/usr/bin/env python3
"""Stop the exact kind node after a command starts, then recover without rerunning it."""
import datetime, json, subprocess, time
from hardening_lib import ROOT, command, receipt_for, utc_now, verify_receipt, wait_due, watchdog_once, watchdog_state

if command(['systemctl','--user','is-active','--quiet','cleanup-receipt-watchdog.service'],check=False).returncode==0:
    raise SystemExit('Stop the watchdog service before this controlled fault; restart it afterward.')

before=set((ROOT/'.build/coordinator').glob('*/run.json'))
revision=command(['git','rev-parse','HEAD']).stdout.strip()
process=subprocess.Popen([str(ROOT/'.build/localci'),'run','--revision',revision,'--job','hardening-controlplane','--timeout','10s','--','/usr/local/bin/node','/opt/examples/local-job.mjs','wait'],cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
path=None
try:
    deadline=time.monotonic()+120
    while time.monotonic()<deadline:
        created=set((ROOT/'.build/coordinator').glob('*/run.json'))-before
        if created:
            path=next(iter(created)); ledger=json.loads(path.read_text())
            if ledger.get('pod_uid'):
                stage=command(['kubectl','--kubeconfig',str(ROOT/'.build/kubeconfig'),'-n',ledger['runner_namespace'],'get','configmap','guard-stage','-o','json'],check=False)
                if stage.returncode==0:
                    status=json.loads(json.loads(stage.stdout).get('data',{}).get('guard.json','{}'))
                    if status.get('main_completed'): break
        if process.poll() is not None: raise RuntimeError('Coordinator exited before fault injection')
        time.sleep(.25)
    else: raise RuntimeError('Job command did not begin')
    process.kill(); process.communicate(timeout=15)
    run_id=ledger['identity']['run_id']
    eligible=datetime.datetime.fromisoformat(ledger['created_at'].replace('Z','+00:00')).timestamp()+ledger['timeout_seconds']+61
    command(['docker','stop','cleanup-receipt-control-plane'])
    try:
        while time.time()<eligible: time.sleep(min(3,max(.1,eligible-time.time())))
        assert watchdog_once(run_id).returncode==0
        failed=watchdog_state(ledger['identity'])
        assert failed['current']['result']=='retry' and failed['current']['stage']=='collect'
    finally:
        command(['docker','start','cleanup-receipt-control-plane'])
    for _ in range(60):
        probe=command(['kubectl','--kubeconfig',str(ROOT/'.build/kubeconfig'),'get','--raw=/readyz'],check=False,timeout=10)
        if probe.returncode==0: break
        time.sleep(1)
    else: raise RuntimeError('Local Kubernetes API did not recover')
    command(['bash','dev','ci-up'],timeout=240)
    assert watchdog_once(run_id).returncode==0
    assert watchdog_state(ledger['identity'])['current']['attempt_number']==1
    wait_due(failed['current']['next_retry_at'])
    assert watchdog_once(run_id).returncode==0
    final=watchdog_state(ledger['identity']); assert final['current']['result']=='succeeded' and final['current']['attempt_number']==2
    completed=json.loads(path.read_text())
    assert completed['resource_absent'] and completed['runner_absent']
    assert (path.parent/'job.log').read_text().count('LOCAL_JOB_STARTED wait')==1
    row=receipt_for(run_id); assert row['verdict']!='pass'
    assert row['incident'] is not None and row['incident']['state']=='open'
    verification=verify_receipt(row['id'])
    report={'kind':'hardening-controlplane-outage/v1','checked_at':utc_now(),'run_id':run_id,'receipt_id':row['id'],'incident_id':row['incident']['id'],'incident_count':1,'first_attempt':{'result':'retry','stage':'collect','error_code':failed['current']['error_code']},'recovered_attempt':2,'command_runs':1,'resources_absent':True,'runner_absent':True,'verdict':row['verdict'],'signature':verification['signature'],'artifacts':verification['artifacts']}
    (ROOT/'.build/hardening-controlplane-outage.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2))
finally:
    if process.poll() is None:
        process.kill(); process.communicate(timeout=15)
    if command(['docker','inspect','-f','{{.State.Running}}','cleanup-receipt-control-plane'],check=False).stdout.strip()!='true':
        command(['docker','start','cleanup-receipt-control-plane'])
