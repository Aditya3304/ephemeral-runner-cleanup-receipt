import json,pathlib,subprocess,urllib.request,shutil
root=pathlib.Path(__file__).resolve().parent.parent
out=root/'outputs/aws-validation'
cli=['bash',str(root/'work/aws.sh')]
for name,command in [('aws-boundary-checks.json','d3364340-d073-495b-8931-7ac24f8a75d5'),('metadata-backup-restore.json','885b2d59-46be-4f27-91af-a399d3c3ffc8'),('host-restart.txt','66a68edb-7ee1-4b0a-902d-12942dc41eba')]:
    result=json.loads(subprocess.check_output(cli+['ssm','get-command-invocation','--command-id',command,'--instance-id','i-0f21b25d3a04d8541'],text=True))
    assert result['Status']=='Success'
    (out/name).write_text(result['StandardOutputContent'])
for name,path in [('github-run.json','actions/runs/34200085381'),('github-jobs.json','actions/runs/34200085381/attempts/2/jobs'),('github-runners.json','actions/runners'),('github-status.json','commits/1f48c97ea421f8814a0222f66449b227779afa75/status')]:
    result=json.loads(subprocess.check_output(['gh','api','repos/Aditya3304/ephemeral-runner-cleanup-receipt/'+path],text=True))
    (out/name).write_text(json.dumps(result,indent=2))
run=json.loads((out/'github-run.json').read_text());assert run['conclusion']=='success' and run['run_attempt']==2
assert json.loads((out/'github-runners.json').read_text())['total_count']==0
statuses=json.loads((out/'github-status.json').read_text())['statuses']
assert next(s for s in statuses if s['context']=='cleanup/signed-receipt')['state']=='success'
with urllib.request.urlopen('http://127.0.0.1:18080/readyz',timeout=10) as response: assert response.status==200
print('Final checks: workflow success, cleanup status success, zero registered runners, dashboard ready.')
