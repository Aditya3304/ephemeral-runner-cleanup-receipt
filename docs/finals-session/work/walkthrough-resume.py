import datetime,json,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parent.parent
out=root/'outputs/walkthrough-execution'
cli=['bash',str(root/'work/aws.sh')]
host='i-0f21b25d3a04d8541'
def aws(*a):
    s=subprocess.check_output(cli+list(a),text=True)
    return json.loads(s) if s.strip() else {}
def remote(script):
    p=root/'work/resume-command.json'
    p.write_text(json.dumps({'DocumentName':'AWS-RunShellScript','InstanceIds':[host],'Parameters':{'commands':[script],'executionTimeout':['300']}}))
    command=aws('ssm','send-command','--cli-input-json','file://'+str(p))['Command']['CommandId']
    for _ in range(50):
        time.sleep(5)
        r=aws('ssm','get-command-invocation','--command-id',command,'--instance-id',host)
        if r['Status'] in ['Pending','InProgress','Delayed']:continue
        assert r['Status']=='Success',r.get('StandardErrorContent')
        return r['StandardOutputContent']
    raise RuntimeError('SSM command deadline')
runs=json.loads(subprocess.check_output(['gh','api','repos/Aditya3304/ephemeral-runner-cleanup-receipt/actions/workflows/cleanup-aws.yml/runs?per_page=20'],text=True))['workflow_runs']
assert not [r for r in runs if r['status']!='completed'],'Wait for active jobs before maintenance'
backup=remote('set -eu\nsystemctl stop cleanup-github.service\ncd /opt/cleanup/repo\npython3 scripts/metadata-backup.py create')
(out/'metadata-backup-restore.json').write_text(backup)
events=[]
def mark(state):
    item={'time':datetime.datetime.now(datetime.timezone.utc).isoformat(),'state':state};events.append(item)
    (out/'host-stop-start.json').write_text(json.dumps(events,indent=2));print(state,flush=True)
aws('ec2','stop-instances','--instance-ids',host);mark('stop requested')
subprocess.run(cli+['ec2','wait','instance-stopped','--instance-ids',host],check=True);mark('stopped confirmed')
aws('ec2','start-instances','--instance-ids',host);mark('start requested')
subprocess.run(cli+['ec2','wait','instance-running','--instance-ids',host],check=True);mark('running confirmed')
for _ in range(60):
    listing=aws('ssm','describe-instance-information','--filters',json.dumps([{'Key':'InstanceIds','Values':[host]}]))['InstanceInformationList']
    if listing and listing[0]['PingStatus']=='Online':break
    time.sleep(5)
else:raise RuntimeError('Host did not reconnect to SSM')
script='''set -eu
for attempt in $(seq 1 40); do
  if systemctl is-active --quiet cleanup-stack.service && curl -fsS http://127.0.0.1:8080/readyz; then break; fi
  sleep 3
done
systemctl is-active cleanup-stack cleanup-github cleanup-github-proxy cleanup-github-broker cleanup-metadata-block
curl -fsS http://127.0.0.1:8080/readyz
systemctl list-timers cleanup-demo-stop.timer --all --no-pager
uptime -s
'''
(out/'host-resume.txt').write_text(remote(script));mark('prepared services and API resumed')
