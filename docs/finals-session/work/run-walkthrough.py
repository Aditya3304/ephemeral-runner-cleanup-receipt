import datetime,hashlib,json,pathlib,subprocess,time,urllib.request,sys
from read_retry import read_retry
root=pathlib.Path(__file__).resolve().parent.parent
out=root/'outputs/walkthrough-execution';out.mkdir(exist_ok=True)
repo='Aditya3304/ephemeral-runner-cleanup-receipt'
revision='9605c536dd49faeb3acf6a80a60ec1136ad604c0'
awscli=['bash',str(root/'work/aws.sh')]
def aws(*args):
    raw=subprocess.check_output(awscli+list(args),text=True,stderr=subprocess.PIPE,timeout=45)
    return json.loads(raw) if raw.strip() else {}
def gh(path,body=None):
    cmd=['gh','api','repos/'+repo+'/'+path]
    if body is not None:cmd+=['--method','POST','--input','-']
    def call():
        return subprocess.check_output(cmd,input=None if body is None else json.dumps(body),text=True,stderr=subprocess.PIPE,timeout=45)
    # Dispatch is not idempotent. Only GETs may be retried automatically.
    raw=read_retry('GitHub status check',call) if body is None else call()
    return json.loads(raw) if raw.strip() else {}
def http(path):
    def call():
        with urllib.request.urlopen('http://localhost:18080'+path,timeout=20) as r:return json.load(r)
    return read_retry('Receipt API check',call)
def remote(script):
    payload={'DocumentName':'AWS-RunShellScript','InstanceIds':['i-0f21b25d3a04d8541'],'Parameters':{'commands':[script],'executionTimeout':['120']}}
    p=root/'work/walkthrough-command.json';p.write_text(json.dumps(payload))
    return aws('ssm','send-command','--cli-input-json','file://'+str(p))['Command']['CommandId']
def result(command):
    return read_retry('Background capture check',lambda:aws('ssm','get-command-invocation','--command-id',command,'--instance-id','i-0f21b25d3a04d8541'))
def save(name,value):
    (out/name).write_text(json.dumps(value,indent=2))

assert gh('git/ref/heads/aws-github-finals')['object']['sha']==revision
assert http('/readyz')['status']=='ready'
history=json.loads((out/'runs.json').read_text()) if (out/'runs.json').exists() else []
for scenario in sys.argv[1:]:
    assert scenario in ['normal','test-failure','cleanup-blocked']
    before={r['id'] for r in gh('actions/workflows/cleanup-aws.yml/runs?per_page=20')['workflow_runs']}
    gh('actions/workflows/cleanup-aws.yml/dispatches',{'ref':'aws-github-finals','inputs':{'scenario':scenario,'observation_seconds':'45'}})
    deadline=time.monotonic()+600
    run=None
    while time.monotonic()<deadline:
        matches=[r for r in gh('actions/workflows/cleanup-aws.yml/runs?per_page=20')['workflow_runs'] if r['id'] not in before and r['head_sha']==revision and r['event']=='workflow_dispatch']
        if matches:
            assert len(matches)==1,'Ambiguous new execution'
            run=matches[0];break
        time.sleep(3)
    assert run
    runid=str(run['id']);print('Started',scenario,runid,flush=True)
    capture=None
    while time.monotonic()<deadline:
        jobs=gh(f'actions/runs/{runid}/attempts/1/jobs')['jobs']
        assert len(jobs)<=1
        if jobs and jobs[0]['status']=='in_progress':
            time.sleep(8)
            script="""set -eu
cd /opt/cleanup/repo
python3 - <<'PY'
import datetime,json,pathlib,subprocess,time
runid='RUNID'
def call(args):
    r=subprocess.run(args,capture_output=True,text=True,timeout=15)
    return {'exit_code':r.returncode,'stdout':r.stdout,'stderr':r.stderr}
for attempt in range(20):
    rows=[json.loads(p.read_text()) for p in pathlib.Path('.build/coordinator').glob('*/run.json')]
    matching=[r for r in rows if r['identity']['run_id']==runid]
    if matching:
        r=matching[0]
        cmd=['kubectl','--kubeconfig','.build/kubeconfig','exec','-n',r['runner_namespace'],r['pod_name'],'-c','guard','--']
        summary=call(cmd+['cat','/data/proof-'+r['token']+'/workspace/export-summary.json'])
        if summary['exit_code']==0:
            snapshot={'checked_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'ledger':{k:r.get(k) for k in ['identity','token','phase','pod_name','pod_uid','runner_namespace','resource_namespace']},'summary':summary,'workspace_files':call(cmd+['ls','-la','/data/proof-'+r['token']+'/workspace']),'credential_filenames':call(cmd+['ls','-la','/data/proof-'+r['token']+'/credentials']),'pods':call(['kubectl','--kubeconfig','.build/kubeconfig','get','pods','-A']),'containers':call(['docker','ps','--format','{{.Names}} {{.Status}}'])}
            print(json.dumps(snapshot));break
    time.sleep(1)
else:raise SystemExit('Live file inspection window was not captured')
PY
""".replace('RUNID',runid)
            capture=remote(script);break
        time.sleep(3)
    assert capture,'Runner did not start'
    state=None
    while time.monotonic()<deadline:
        state=gh(f'actions/runs/{runid}')
        if state['status']=='completed':break
        time.sleep(5)
    assert state['status']=='completed'
    print('GitHub completed',runid,state['conclusion'],'; waiting for signed receipt',flush=True)
    row=None
    while time.monotonic()<deadline:
        matches=[r for r in http('/v1/receipts?limit=100')['items'] if r['run_id']==runid and r['run_attempt']==1]
        if matches:row=matches[0];break
        time.sleep(4)
    assert row,'Signed receipt did not arrive'
    expected='fail' if scenario=='cleanup-blocked' else 'pass'
    assert row['verdict']==expected and row['signature_state']=='verified'
    assert state['conclusion']==('success' if scenario=='normal' else 'failure')
    verify=http('/v1/receipts/'+row['id']+'/verification')
    assert verify['signature']=='verified' and verify['artifacts']=='verified'
    snapshot=result(capture);assert snapshot['Status']=='Success',snapshot.get('StandardErrorContent')
    save(runid+'-live.json',json.loads(snapshot['StandardOutputContent']))
    save(runid+'-github.json',state)
    save(runid+'-jobs.json',gh(f'actions/runs/{runid}/attempts/1/jobs'))
    save(runid+'-api-verification.json',verify)
    item={'scenario':scenario,'run_id':runid,'attempt':1,'workflow':state['conclusion'],'receipt':row,'url':state['html_url'],'dashboard':'http://localhost:18080/?receipt='+row['id'],'live_capture_command':capture}
    history.append(item);save('runs.json',history)
    print('VERIFIED',scenario,runid,row['verdict'],item['dashboard'],flush=True)
print('Walkthrough jobs complete',flush=True)
