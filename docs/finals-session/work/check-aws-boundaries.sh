python3 - <<'PY'
import json,os,pathlib,subprocess
root=pathlib.Path('/opt/cleanup/repo')
config=json.loads((root/'.build/aws-resources.json').read_text())
key='evidence/sha256/e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
checks=[]
for volume,operation in [('verifier','put-object'),('finalizer','delete-object')]:
    file=pathlib.Path('/var/lib/docker/volumes/cleanup-receipt-archive_'+volume+'/_data/session.json')
    credentials=json.loads(file.read_text())
    assert file.stat().st_mode&0o777==0o600
    env=dict(os.environ,AWS_ACCESS_KEY_ID=credentials['AccessKeyId'],AWS_SECRET_ACCESS_KEY=credentials['SecretAccessKey'],AWS_SESSION_TOKEN=credentials['SessionToken'],AWS_EC2_METADATA_DISABLED='true')
    result=subprocess.run(['/usr/local/bin/aws','--region','us-east-1','s3api',operation,'--bucket',config['Evidence'],'--key',key],env=env,capture_output=True,text=True)
    assert result.returncode!=0 and 'AccessDenied' in result.stderr,(operation,'expected AccessDenied')
    checks.append({'check':volume+' '+operation,'result':'AccessDenied','session_expires':credentials['Expiration']})
image='node:24.20.0-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e'
code="fetch('http://169.254.169.254/latest/meta-data/iam/info',{signal:AbortSignal.timeout(3000)}).then(()=>process.exit(1)).catch(()=>console.log('metadata network access blocked'))"
result=subprocess.run(['docker','run','--rm','--network','kind','--read-only','--cap-drop','ALL','--user','1000:1000',image,'node','-e',code],capture_output=True,text=True)
assert result.returncode==0,result.stderr
checks.append({'check':'container metadata network access','result':'blocked'})
result=subprocess.check_output(['kubectl','--kubeconfig',str(root/'.build/kubeconfig'),'get','namespaces','-o','json'],text=True)
assert not [n['metadata']['name'] for n in json.loads(result)['items'] if n['metadata']['name'].startswith('proof-')]
checks.append({'check':'ephemeral Kubernetes namespaces','result':'none remain'})
print(json.dumps(checks))
PY
