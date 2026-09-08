#!/usr/bin/env python3
"""Explicit staged teardown; retained Object Lock evidence is never bypassed.

Use a saved resources.json from deploy.py. Without --apply this is read-only.
Export receipts/public trust and metadata before choosing delete-compute.
"""
import argparse,datetime,json,pathlib,subprocess
parser=argparse.ArgumentParser();parser.add_argument('--profile',required=True);parser.add_argument('--resources',type=pathlib.Path,required=True);parser.add_argument('--mode',choices=['inspect','stop','delete-compute','delete-evidence'],default='inspect');parser.add_argument('--apply',action='store_true');a=parser.parse_args()
r=json.loads(a.resources.read_text());cli=['aws','--profile',a.profile,'--region','us-east-1','--no-cli-pager']
def aws(*args):
    raw=subprocess.check_output(cli+list(args),text=True);return json.loads(raw) if raw.strip() else {}
account=aws('sts','get-caller-identity')['Account']
if r['KeyArn'].split(':')[4]!=account or r['Evidence']!='cleanup-evidence-'+account+'-us-east-1' or r['Assets']!='cleanup-deploy-'+account+'-us-east-1':raise SystemExit('Account/resource identity mismatch')
print('Target:',a.mode,r['Host'],r['Evidence'],r['KeyArn'])
print('Evidence and KMS are retained when compute is deleted. Object Lock deadlines cannot be shortened.')
if not a.apply or a.mode=='inspect':raise SystemExit(0)
if a.mode=='stop':
    aws('ec2','stop-instances','--instance-ids',r['Host']);print('Stop requested; EBS/S3/KMS remain.');raise SystemExit(0)
if a.mode=='delete-compute':
    stack=aws('cloudformation','describe-stacks','--stack-name','cleanup-finals')['Stacks'][0]
    actual={x['OutputKey']:x['OutputValue'] for x in stack['Outputs']}
    if any(actual.get(k)!=v for k,v in r.items()):raise SystemExit('Saved stack outputs differ; inspect instead')
    subprocess.run(cli+['s3','rm','s3://'+r['Assets']+'/', '--recursive','--only-show-errors'],check=True)
    aws('cloudformation','delete-stack','--stack-name','cleanup-finals')
    print('Compute/network stack deletion requested. Check completion; retain this resource manifest.');raise SystemExit(0)
versions=aws('s3api','list-object-versions','--bucket',r['Evidence'])
now=datetime.datetime.now(datetime.timezone.utc)
objects=[]
for v in versions.get('Versions',[]):
    retention=aws('s3api','get-object-retention','--bucket',r['Evidence'],'--key',v['Key'],'--version-id',v['VersionId'])['Retention']
    until=datetime.datetime.fromisoformat(retention['RetainUntilDate'].replace('Z','+00:00'))
    if until>now:raise SystemExit('Evidence still retained until '+until.isoformat()+'; nothing deleted')
    hold=aws('s3api','get-object-legal-hold','--bucket',r['Evidence'],'--key',v['Key'],'--version-id',v['VersionId']).get('LegalHold',{})
    if hold.get('Status')=='ON':raise SystemExit('Legal hold present; nothing deleted')
    objects.append({'Key':v['Key'],'VersionId':v['VersionId']})
objects.extend({'Key':v['Key'],'VersionId':v['VersionId']} for v in versions.get('DeleteMarkers',[]))
for start in range(0,len(objects),1000):
    result=aws('s3api','delete-objects','--bucket',r['Evidence'],'--delete',json.dumps({'Objects':objects[start:start+1000],'Quiet':True}))
    if result.get('Errors'):raise SystemExit('Some versions were not deleted; key preserved')
aws('s3api','delete-bucket','--bucket',r['Evidence'])
result=aws('kms','schedule-key-deletion','--key-id',r['KeyArn'],'--pending-window-in-days','7')
print('Evidence bucket deleted; KMS deletion scheduled:',result.get('DeletionDate'))
