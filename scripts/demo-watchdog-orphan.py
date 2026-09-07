#!/usr/bin/env python3
"""Kill this harness's coordinator, then let watchdog recover its actual run."""
from datetime import datetime,timezone
import json,pathlib,subprocess,time,urllib.request
root=pathlib.Path(__file__).resolve().parent.parent
if subprocess.run(['systemctl','--user','is-active','--quiet','cleanup-receipt-watchdog.service']).returncode==0:raise SystemExit('Stop the watchdog service before this controlled fault demo; restart it afterward.')
ledger=root/'.build/coordinator'
before=set(ledger.glob('*/run.json'))
revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
process=subprocess.Popen([str(root/'.build/localci'),'run','--revision',revision,'--job','watchdog-orphan','--timeout','10s','--','/usr/local/bin/node','/opt/examples/local-job.mjs','wait'],cwd=root,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
path=None
try:
 deadline=time.monotonic()+90
 while time.monotonic()<deadline:
  new=set(ledger.glob('*/run.json'))-before
  if new:
   path=next(iter(new));value=json.loads(path.read_text())
   if value.get('pod_uid'):
    cmd=['kubectl','--kubeconfig',str(root/'.build/kubeconfig'),'-n',value['runner_namespace'],'get','configmap','guard-stage','-o','json']
    stage=subprocess.run(cmd,capture_output=True,text=True)
    if stage.returncode==0 and json.loads(json.loads(stage.stdout).get('data',{}).get('guard.json','{}')).get('main_completed'):break
  if process.poll() is not None:raise RuntimeError('Coordinator exited before injection')
  time.sleep(.2)
 else:raise RuntimeError('Guard never reached main')
 process.kill();process.communicate(timeout=15)
 runid=value['identity']['run_id']
 print(json.dumps({'event':'coordinator_killed','run_id':runid,'pod_uid':value['pod_uid']}),flush=True)
 eligible=datetime.fromisoformat(value['created_at'].replace('Z','+00:00')).timestamp()+71
 while time.time()<eligible:time.sleep(min(5,max(.1,eligible-time.time())))
 recovered=subprocess.run(['python3','scripts/watchdog-run.py','--run',runid],cwd=root,check=True,capture_output=True,text=True)
 final=json.loads(path.read_text())
 assert final['phase']=='complete' and final['runner_absent'] and final['resource_absent']
 assert (path.parent/'job.log').read_text().count('LOCAL_JOB_STARTED wait')==1
 with urllib.request.urlopen('http://127.0.0.1:8080/v1/receipts?run_id='+runid) as r:items=json.load(r)['items']
 assert len(items)==1
 with urllib.request.urlopen('http://127.0.0.1:8080/v1/receipts/'+items[0]['id']+'/verification',timeout=50) as r:verified=json.load(r)
 assert verified['signature']=='verified' and verified['artifacts']=='verified'
 report={'kind':'watchdog-coordinator-crash/v1','run_id':runid,'receipt_id':items[0]['id'],'verdict':items[0]['verdict'],'runner_absent':final['runner_absent'],'resource_absent':final['resource_absent'],'command_runs':1,'verification':verified}
 (root/'.build/watchdog-orphan-result.json').write_text(json.dumps(report,indent=2)+'\n')
 print(json.dumps(report),flush=True)
finally:
 if process.poll() is None:process.kill();process.communicate(timeout=15)
