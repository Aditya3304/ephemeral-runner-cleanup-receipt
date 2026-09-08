import datetime,hashlib,json,pathlib,shutil,subprocess
from read_retry import read_retry
root=pathlib.Path(__file__).resolve().parent.parent
out=root/'outputs/walkthrough-execution'
runs=json.loads((out/'runs.json').read_text())
cli=['bash',str(root/'work/aws.sh')]
cosign='/mnt/c/Users/Aditya/Documents/Codex/2026-09-07/i-need-you-to-fully-implement/outputs/ephemeral-runner-cleanup-receipt/.build/cosign'
shutil.copyfile(root/'outputs/aws-validation/trusted-root.json',out/'trusted-root.json')
checks=[]
for run in runs:
    folder=out/run['run_id'];folder.mkdir(exist_ok=True)
    refs=dict(receipt=run['receipt']['receipt_object'],bundle=run['receipt']['bundle_object'])
    def fetch(name,ref):
        dest=folder/(name+'.json')
        read_retry('S3 evidence download',lambda:subprocess.run(cli+['s3api','get-object','--bucket',ref['bucket'],'--key',ref['key'],'--version-id',ref['version_id'],str(dest)],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,timeout=45))
        assert hashlib.sha256(dest.read_bytes()).hexdigest()==ref['sha256']
        return json.loads(dest.read_bytes())
    receipt=fetch('receipt',refs['receipt']);fetch('bundle',refs['bundle'])
    observation=fetch('observations',receipt['objects']['observations.json'])
    assert observation['identity']==receipt['identity']
    assert observation['collector']['workspace']['status']==('failed' if run['scenario']=='cleanup-blocked' else 'verified')
    assert receipt['finalizer']['trust_root_sha256']==hashlib.sha256((out/'trusted-root.json').read_bytes()).hexdigest()
    command=[cosign,'verify-blob','--offline','--trusted-root',str(out/'trusted-root.json'),'--bundle',str(folder/'bundle.json'),'--certificate-identity','finalizer@cleanup-receipt.local','--certificate-oidc-issuer','https://issuer:8443']
    valid=subprocess.run(command+[str(folder/'receipt.json')],capture_output=True,text=True)
    assert valid.returncode==0,valid.stderr
    altered=root/'work/walkthrough-altered.json';altered.write_bytes((folder/'receipt.json').read_bytes()+b' ')
    invalid=subprocess.run(command+[str(altered)],capture_output=True,text=True)
    assert invalid.returncode!=0
    meta=json.loads(read_retry('S3 metadata check',lambda:subprocess.check_output(cli+['s3api','head-object','--bucket',refs['receipt']['bucket'],'--key',refs['receipt']['key'],'--version-id',refs['receipt']['version_id']],text=True,stderr=subprocess.PIPE,timeout=45)))
    assert meta['ServerSideEncryption']=='aws:kms' and meta['ObjectLockMode']=='COMPLIANCE'
    (folder/'s3-metadata.json').write_text(json.dumps(meta,indent=2))
    capture=json.loads((out/(run['run_id']+'-live.json')).read_text())
    assert json.loads(capture['summary']['stdout'])=={'orders':3,'totalCents':4999}
    checks.append({'run_id':run['run_id'],'scenario':run['scenario'],'offline_signature':'verified','altered_bytes':'rejected','observations_hash':'matched signed reference','workspace':observation['collector']['workspace'],'s3_encryption':meta['ServerSideEncryption'],'retention_mode':meta['ObjectLockMode'],'retained_until':meta['ObjectLockRetainUntilDate'],'live_files':'observed in running pod'})
    print('Verified exact S3 bytes, independent observations and signature:',run['run_id'],flush=True)
(out/'verification.json').write_text(json.dumps(checks,indent=2))
lines=['# Completed live walkthrough','', 'Executed against AWS and GitHub on '+datetime.datetime.now(datetime.timezone.utc).isoformat()+'.', '', '| Scenario | GitHub result | Signed cleanup | Dashboard receipt | GitHub run |','| --- | --- | --- | --- | --- |']
for r in runs:lines.append(f"| {r['scenario']} | {r['workflow']} | {r['receipt']['verdict']} | [Open receipt]({r['dashboard']}) | [{r['run_id']}]({r['url']}) |")
lines+=['','Every listed case was newly dispatched and its synthetic files inspected inside the running pod. The API reverified each receipt and its archived artifacts. Exact S3 versions and observation hashes were exported. All signatures verified offline; altered receipt bytes were rejected. S3 confirmed SSE-KMS and Compliance Object Lock.','', 'The blocked case retained a job-reported workspace failure; its signed archive separately binds the trusted collector’s independent residue finding.','', 'See `runs.json` for exact IDs and receipt metadata, `verification.json` for verification results, each run folder for original bytes/S3 metadata, and the `*-live.json` files for live pod observations.','', 'The final normal run follows a tested EC2 stop/start when `host-resume.txt` is present. The host and private tunnels are left available for rehearsal; stopping/teardown is an operator action, and locked evidence is retained.','', 'No repository source, trust roots or historical receipts were rewritten for these runs. No credentials or raw job logs are exported in this packet.']
(out/'RESULTS.md').write_text('\n'.join(lines)+'\n')
