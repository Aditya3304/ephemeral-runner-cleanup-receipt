"""Fetch exact signed artifacts and verify them. Raw logs are saved, not printed."""
import argparse,hashlib,json,pathlib,re,shutil,subprocess,urllib.request
from read_retry import read_retry
ROOT=pathlib.Path(__file__).resolve().parent.parent
COSIGN=pathlib.Path('/mnt/c/Users/Aditya/Documents/Codex/2026-09-07/i-need-you-to-fully-implement/outputs/ephemeral-runner-cleanup-receipt/.build/cosign')
def get_json(path):
    def request():
        with urllib.request.urlopen('http://localhost:18080'+path,timeout=20) as response:return json.load(response)
    return read_retry('Receipt API',request)
def verify(receipt_id):
    if not re.fullmatch('[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}',receipt_id):raise ValueError('Expected receipt UUID')
    row=get_json('/v1/receipts/'+receipt_id)
    folder=ROOT/'outputs/open-evidence'/receipt_id;folder.mkdir(parents=True,exist_ok=True)
    cli=['bash',str(ROOT/'work/aws.sh')]
    print('\nRECEIPT',receipt_id,'GITHUB RUN',row['run_id'],'ATTEMPT',row['run_attempt'],flush=True)
    (folder/'dashboard-metadata.json').write_text(json.dumps(row,indent=2))
    def fetch(name,ref):
        if ref['bucket']!='cleanup-evidence-385020093237-us-east-1' or ref['key']!='evidence/sha256/'+ref['sha256']:raise ValueError('Unexpected archive reference')
        dest=folder/name
        read_retry('Exact S3 download',lambda:subprocess.run(cli+['s3api','get-object','--bucket',ref['bucket'],'--key',ref['key'],'--version-id',ref['version_id'],str(dest)],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,timeout=45))
        data=dest.read_bytes()
        if len(data)!=ref['size_bytes'] or hashlib.sha256(data).hexdigest()!=ref['sha256']:raise ValueError('Archive hash/size mismatch')
        print('HASH MATCH:',name,'| version:',ref['version_id'],flush=True)
        return data
    receipt=json.loads(fetch('receipt.json',row['receipt_object']))
    fetch('bundle.json',row['bundle_object'])
    trust=folder/'trusted-root.json'
    shutil.copyfile(ROOT/'outputs/aws-validation/trusted-root.json',trust)
    if hashlib.sha256(trust.read_bytes()).hexdigest()!=receipt['finalizer']['trust_root_sha256']:raise ValueError('Independent trust root differs')
    if hashlib.sha256(COSIGN.read_bytes()).hexdigest()!=receipt['finalizer']['cosign_sha256']:raise ValueError('Verifier binary pin differs')
    command=[str(COSIGN),'verify-blob','--offline','--trusted-root',str(trust),'--bundle',str(folder/'bundle.json'),'--certificate-identity','finalizer@cleanup-receipt.local','--certificate-oidc-issuer','https://issuer:8443']
    result=subprocess.run(command+[str(folder/'receipt.json')],capture_output=True,text=True,timeout=60)
    if result.returncode:raise RuntimeError(result.stderr)
    print('OFFLINE SIGNATURE:',(result.stdout+result.stderr).strip(),flush=True)
    evidence={}
    for name,ref in receipt['objects'].items():
        if name not in ['run.json','observations.json','cleanup-evidence.json']:raise ValueError('Unknown evidence filename')
        evidence[name]=json.loads(fetch(name,ref))
    for index,ref in enumerate(receipt['log_objects']):fetch(f'job-{index+1}.log',ref)
    altered=ROOT/'work/open-evidence-altered.json';altered.write_bytes((folder/'receipt.json').read_bytes()+b' ')
    bad=subprocess.run(command+[str(altered)],capture_output=True,text=True,timeout=60)
    if bad.returncode==0:raise RuntimeError('Modified bytes were unexpectedly accepted')
    print('ALTERED BYTES: rejected (original receipt untouched)',flush=True)
    reference=row['receipt_object']
    meta=json.loads(read_retry('S3 retention check',lambda:subprocess.check_output(cli+['s3api','head-object','--bucket',reference['bucket'],'--key',reference['key'],'--version-id',reference['version_id']],text=True,stderr=subprocess.PIPE,timeout=45)))
    (folder/'s3-metadata.json').write_text(json.dumps(meta,indent=2))
    current=get_json('/v1/receipts/'+receipt_id+'/verification')
    if current['signature']!='verified' or current['artifacts']!='verified':raise RuntimeError('Online verification failed')
    print('ONLINE RE-CHECK:',json.dumps(current),flush=True)
    print('CLEANUP VERDICT:',receipt['verdict'],flush=True)
    print('SIGNED COVERAGE:',json.dumps(receipt['coverage'],indent=2),flush=True)
    print('EXTERNAL OBSERVER:',json.dumps(evidence['observations.json']['collector']['workspace']),flush=True)
    print('S3:',meta.get('ServerSideEncryption'),meta.get('ObjectLockMode'),meta.get('ObjectLockRetainUntilDate'),flush=True)
    print('DASHBOARD: http://localhost:18080/?receipt='+receipt_id,flush=True)
    print('FILES:',folder,'(raw runtime logs saved here; not printed)',flush=True)
    check={'receipt_id':receipt_id,'run_id':row['run_id'],'verdict':receipt['verdict'],'signature':'verified','all_exported_hashes':'matched signed references','altered_bytes':'rejected','online_verification':current}
    (folder/'verification.json').write_text(json.dumps(check,indent=2))
    return check
if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--receipt',required=True);args=parser.parse_args();verify(args.receipt)
