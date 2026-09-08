#!/usr/bin/env python3
"""Trusted host: rotate separate STS sessions into private Docker volumes."""
import json, os, pathlib, subprocess, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
config=json.loads((root/'.build/aws-resources.json').read_text())
def run(args): return subprocess.check_output(args,text=True)
for role,volume,mount in [('WriterArn','finalizer','/secrets/archive'),('ReaderArn','verifier','/run/archive-verifier')]:
    name='cleanup-receipt-archive_'+volume
    subprocess.run(['docker','volume','create',name],check=True,stdout=subprocess.DEVNULL)
    path=pathlib.Path(json.loads(run(['docker','volume','inspect',name]))[0]['Mountpoint'])
    session=json.loads(run(['aws','sts','assume-role','--region','us-east-1','--role-arn',config[role],'--role-session-name','cleanup-'+volume,'--duration-seconds','3600']))['Credentials']
    archive={'provider':'aws','store':'aws-evidence','endpoint':'https://s3.us-east-1.amazonaws.com','region':'us-east-1','bucket':config['Evidence'],'prefix':'evidence','session_file':mount+'/session.json','kms_key':config['KeyArn'],'max_object_bytes':104857600}
    for filename,data in [('session.json',session),('config.json',archive)]:
        fd,tmp=tempfile.mkstemp(dir=path)
        try:
            os.fchown(fd,65532,65532);os.fchmod(fd,0o600)
            with os.fdopen(fd,'w') as f: json.dump(data,f);f.flush();os.fsync(f.fileno())
            os.replace(tmp,path/filename)
        finally:
            if os.path.exists(tmp):os.unlink(tmp)
    os.chown(path,65532,65532);os.chmod(path,0o700)
    if volume=='verifier':
        public='cleanup-receipt-archive_public'
        subprocess.run(['docker','volume','create',public],check=True,stdout=subprocess.DEVNULL)
        p=pathlib.Path(json.loads(run(['docker','volume','inspect',public]))[0]['Mountpoint'])
        (p/'verifier.json').write_text(json.dumps(archive));os.chmod(p/'verifier.json',0o644)
print('Separate short-lived archive sessions installed; no credentials logged.')
