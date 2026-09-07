#!/usr/bin/env python3
"""Run the complete approved local milestone h matrix and retain its evidence."""
import hashlib, json, pathlib, subprocess, time, uuid
from hardening_lib import ROOT, command, get, utc_now, wait_api

steps=[]
logdir=ROOT/'.build/hardening-logs'; logdir.mkdir(parents=True,exist_ok=True)
canary=ROOT/'.build/hardening-host-canary.txt'
if not canary.exists(): canary.write_text('This unrelated host file must survive every cleanup fault.\n')
canary_hash=hashlib.sha256(canary.read_bytes()).hexdigest()

def run(name,args,timeout=1200):
    started=time.monotonic(); result=command(args,check=False,timeout=timeout)
    (logdir/(name+'.log')).write_text(result.stdout+result.stderr)
    item={'name':name,'status':'passed' if result.returncode==0 else 'failed','seconds':round(time.monotonic()-started,3)}
    steps.append(item); print(json.dumps(item),flush=True)
    if result.returncode: raise RuntimeError(f'{name} failed; see {logdir/(name+".log")}')

was_active=command(['systemctl','--user','is-active','--quiet','cleanup-receipt-watchdog.service'],check=False).returncode==0
report={'kind':'hardening-matrix/v1','started_at':utc_now(),'status':'running','steps':steps}
canary_namespace='proof-canary-h-'+uuid.uuid4().hex[:12]
canary_token=uuid.uuid4().hex
canary_uid=''
try:
    run('local-services',['bash','dev','ci-up'],300)
    run('runner-image-and-trusted-helpers',['bash','dev','ci-build'],900)
    manifest={'apiVersion':'v1','kind':'Namespace','metadata':{'name':canary_namespace,'labels':{'cleanup-receipt.local/hardening-canary':canary_token}}}
    made=command(['kubectl','--kubeconfig',str(ROOT/'.build/kubeconfig'),'create','-f','-'],check=False,timeout=60,input=json.dumps(manifest))
    if made.returncode: raise RuntimeError('Could not create isolated hardening canary namespace')
    created=json.loads(command(['kubectl','--kubeconfig',str(ROOT/'.build/kubeconfig'),'get','namespace',canary_namespace,'-o','json']).stdout)
    canary_uid=created['metadata']['uid']
    run('signing-storage-api',['bash','dev','finalizer-up'],300)
    run('dashboard-api',['bash','dev','dashboard-up'],300)
    wait_api()
    command(['systemctl','--user','stop','cleanup-receipt-watchdog.service'])
    run('cleanup-unit-and-offline-signature',['bash','dev','cli-check'])
    run('cleanup-kind-ownership-workspace-sentinel',['bash','dev','kind-check'],900)
    run('guard-coordinator-boundaries',['bash','dev','guard-check'])
    run('fresh-end-to-end-lifecycle',['python3','scripts/demo-hardening-lifecycle.py'],1800)
    run('archive-encryption-versioning-retention',['bash','scripts/archive-test.sh'],900)
    run('private-keyless-signing',['bash','scripts/sigstore-check.sh'],600)
    run('api-tamper-dedup-rollback',['bash','scripts/api-test.sh'],900)
    run('live-api-and-network-boundary',['bash','scripts/api-live-test.sh'],300)
    run('stored-evidence-read-outage',['python3','scripts/api-archive-fault.py'],300)
    run('minio-signing-postgres-recovery',['python3','scripts/demo-hardening-service-faults.py'],1800)
    run('kubernetes-log-collector-outage',['python3','scripts/demo-hardening-controlplane-outage.py'],900)
    run('dashboard-browser-regression',['bash','scripts/dashboard-test.sh'],900)
    assert hashlib.sha256(canary.read_bytes()).hexdigest()==canary_hash
    assert not command(['docker','ps','-aq','--filter','label=cleanup-receipt.watchdog=operator']).stdout.strip()
    surviving=json.loads(command(['kubectl','--kubeconfig',str(ROOT/'.build/kubeconfig'),'get','namespace',canary_namespace,'-o','json']).stdout)
    assert surviving['metadata']['uid']==canary_uid and surviving['metadata']['labels']['cleanup-receipt.local/hardening-canary']==canary_token
    report.update({'status':'passed','completed_at':utc_now(),'host_canary_sha256':canary_hash,'host_canary_unchanged':True,'unrelated_namespace':canary_namespace,'unrelated_namespace_uid':canary_uid,'unrelated_namespace_survived':True,'watchdog_orphans':0,'external_runtime':'blocked by tested runner/API/archive internal boundaries','paid_services_used':False,'lifecycle':json.loads((ROOT/'.build/hardening-lifecycle.json').read_text()),'service_faults':json.loads((ROOT/'.build/hardening-service-faults.json').read_text()),'controlplane_outage':json.loads((ROOT/'.build/hardening-controlplane-outage.json').read_text()),'archive_read_outage':json.loads((ROOT/'.build/api-archive-fault.json').read_text()),'browser':json.loads((ROOT/'.build/dashboard-test-results.json').read_text())['stats']})
except Exception as error:
    report.update({'status':'failed','completed_at':utc_now(),'failure':str(error)[:1000]})
    raise
finally:
    current=command(['kubectl','--kubeconfig',str(ROOT/'.build/kubeconfig'),'get','namespace',canary_namespace,'-o','json'],check=False,timeout=60)
    if current.returncode==0:
        value=json.loads(current.stdout)
        if canary_uid and value['metadata']['uid']==canary_uid and value['metadata']['labels'].get('cleanup-receipt.local/hardening-canary')==canary_token:
            command(['kubectl','--kubeconfig',str(ROOT/'.build/kubeconfig'),'delete','namespace',canary_namespace,'--wait=true','--timeout=60s'],check=False,timeout=90)
    if was_active: command(['systemctl','--user','start','cleanup-receipt-watchdog.service'],check=False)
    (ROOT/'.build/hardening-results.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps({'status':report['status'],'steps':len(steps)},indent=2))
