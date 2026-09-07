#!/usr/bin/env python3
"""Bounded real API outage; restore the project API even if an assertion fails."""
import json,pathlib,subprocess,time,urllib.request
root=pathlib.Path(__file__).resolve().parent.parent
if subprocess.run(['systemctl','--user','is-active','--quiet','cleanup-receipt-watchdog.service']).returncode==0:raise SystemExit('Stop the watchdog service before this controlled fault demo; restart it afterward.')
def run(args):return subprocess.run(args,cwd=root,check=True,capture_output=True,text=True).stdout
output=run(['python3','scripts/demo-ci.py','pass'])
case=next(json.loads(line) for line in output.splitlines() if line.startswith('{"case"'))
runid=case['run_id']
def step():return run(['python3','scripts/watchdog-run.py','--run',runid])
def state():
 for p in (root/'.build/watchdog-state').glob('*.json'):
  value=json.loads(p.read_text())
  if value.get('identity',{}).get('run_id')==runid:return value
 raise AssertionError('Missing durable state')
try:
 run(['docker','stop','cleanup-receipt-api-api-1'])
 step()
 first=state()
 assert first['current']['result']=='retry',first['current']
 assert first['current']['attempt_number']==1
 print(json.dumps({'event':'api_outage_retained','run_id':runid,'state':first['current']}),flush=True)
finally:
 run(['docker','start','cleanup-receipt-api-api-1'])
 for _ in range(45):
  try:
   with urllib.request.urlopen('http://127.0.0.1:8080/readyz',timeout=3) as r:
    if r.status==200:break
  except Exception:time.sleep(1)
 else:raise RuntimeError('API did not recover')
step();assert state()['current']['attempt_number']==1,'Retried before durable deadline'
print('Restarted worker preserved its retry deadline; waiting for eligibility.',flush=True)
from datetime import datetime,timezone
due=datetime.fromisoformat(first['current']['next_retry_at'].replace('Z','+00:00'))
while datetime.now(timezone.utc)<due:time.sleep(min(5,max(.1,(due-datetime.now(timezone.utc)).total_seconds())))
step();last=state();assert last['current']['result']=='succeeded',last['current']
with urllib.request.urlopen('http://127.0.0.1:8080/v1/receipts?run_id='+runid) as r:receipts=json.load(r)['items']
assert len(receipts)==1
with urllib.request.urlopen('http://127.0.0.1:8080/v1/receipts/'+receipts[0]['id']+'/verification',timeout=50) as r:verification=json.load(r)
assert verification['signature']=='verified' and verification['artifacts']=='verified'
report={'kind':'watchdog-outage-demo/v1','run_id':runid,'first':first,'recovered':last,'receipt_id':receipts[0]['id'],'verification':verification}
(root/'.build/watchdog-outage-result.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps({'event':'recovered_after_outage','run_id':runid,'receipt_id':receipts[0]['id'],'attempts':last['current']['attempt_number']}),flush=True)
