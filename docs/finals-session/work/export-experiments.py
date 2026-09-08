import hashlib,json,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parent.parent
out=root/'outputs/aws-validation'
cli=['bash',str(root/'work/aws.sh')]
cases=[('normal',34203311613,2,'success','pass'),('test-failure',34204207125,1,'failure','pass'),('cleanup-blocked',34204363463,1,'failure','fail')]
page=json.loads((out/'verified-api-records.json').read_text())
results=[]
for scenario,run,attempt,job_verdict,cleanup in cases:
    folder=out/f'{run}-{attempt}'
    receipt=json.loads((folder/'receipt.json').read_bytes())
    row=next(x for x in page['items'] if x['run_id']==str(run) and x['run_attempt']==attempt)
    assert row['signature_state']=='verified' and receipt['verdict']==cleanup
    assert receipt['identity']['source_revision']=='9605c536dd49faeb3acf6a80a60ec1136ad604c0'
    ref=receipt['objects']['observations.json']
    subprocess.run(cli+['s3api','get-object','--bucket',ref['bucket'],'--key',ref['key'],'--version-id',ref['version_id'],str(folder/'observations.json')],check=True,stdout=subprocess.DEVNULL)
    assert hashlib.sha256((folder/'observations.json').read_bytes()).hexdigest()==ref['sha256']
    obs=json.loads((folder/'observations.json').read_bytes())
    assert obs['collector']['workspace']['status']==('failed' if scenario=='cleanup-blocked' else 'verified')
    github=json.loads(subprocess.check_output(['gh','api',f'repos/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/{run}/attempts/{attempt}/jobs'],text=True))
    assert len(github['jobs'])==1 and github['jobs'][0]['conclusion']==job_verdict
    (folder/'github-jobs.json').write_text(json.dumps(github,indent=2))
    results.append({'scenario':scenario,'run_id':str(run),'attempt':attempt,'workflow':job_verdict,'signed_cleanup':cleanup,'signature_state':row['signature_state'],'receipt_sha256':row['receipt_object']['sha256'],'external_workspace':obs['collector']['workspace'],'url':f'https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/{run}/attempts/{attempt}'})
snapshot=json.loads(subprocess.check_output(cli+['ssm','get-command-invocation','--command-id','956ff4ee-8ea6-4495-8fb1-be192c6a33e5','--instance-id','i-0f21b25d3a04d8541'],text=True))
assert snapshot['Status']=='Success'
(out/'live-synthetic-files.txt').write_text(snapshot['StandardOutputContent'])
runners=json.loads(subprocess.check_output(['gh','api','repos/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runners'],text=True))
assert runners['total_count']==0
(out/'experiment-runner-registry.json').write_text(json.dumps(runners,indent=2))
(out/'controlled-experiments.json').write_text(json.dumps(results,indent=2))
print(json.dumps(results,indent=2))
