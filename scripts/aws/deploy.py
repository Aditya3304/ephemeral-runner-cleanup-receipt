#!/usr/bin/env python3
"""Provision and deploy committed code only. No GitHub credential is exported.

Run on the operator laptop with an authenticated AWS CLI profile. The default
prints the template; --apply creates the credit-funded resources and deploys.
"""
import argparse,json,pathlib,shlex,subprocess,time
root=pathlib.Path(__file__).resolve().parents[2]
parser=argparse.ArgumentParser();parser.add_argument('--profile',required=True);parser.add_argument('--stack',default='cleanup-finals');parser.add_argument('--apply',action='store_true');a=parser.parse_args()
if a.stack!='cleanup-finals':raise SystemExit('This bounded profile uses the dedicated cleanup-finals stack')
build=root/'.build/aws-deploy';build.mkdir(parents=True,exist_ok=True)
template=subprocess.check_output(['python3',str(root/'infra/aws/template.py')])
(build/'template.json').write_bytes(template)
if not a.apply:
    print('Review',build/'template.json');raise SystemExit(0)
cli=['aws','--profile',a.profile,'--region','us-east-1','--no-cli-pager']
def aws(*args):
    raw=subprocess.check_output(cli+list(args),text=True);return json.loads(raw) if raw.strip() else {}
plan=aws('freetier','get-account-plan-state')
if plan['accountPlanType']!='FREE' or float(plan['accountPlanRemainingCredits']['amount'])<10:raise SystemExit('This demo requires an active Free plan with at least $10 credits; review costs separately for another plan')
if subprocess.check_output(['git','status','--porcelain','--untracked-files=no'],cwd=root,text=True).strip():raise SystemExit('Commit tracked deployment source changes first')
sha=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
subprocess.run(cli+['cloudformation','deploy','--stack-name',a.stack,'--template-file',str(build/'template.json'),'--capabilities','CAPABILITY_NAMED_IAM','--no-fail-on-empty-changeset'],check=True)
outputs=aws('cloudformation','describe-stacks','--stack-name',a.stack)['Stacks'][0]['Outputs']
resources={x['OutputKey']:x['OutputValue'] for x in outputs}
(build/'resources.json').write_text(json.dumps(resources,indent=2))
subprocess.run(['git','bundle','create',str(build/'source.bundle'),'HEAD'],cwd=root,check=True)
for filename in ['source.bundle','resources.json']:
    subprocess.run(cli+['s3','cp',str(build/filename),'s3://'+resources['Assets']+'/'+filename,'--only-show-errors'],check=True)
inner=f'''set -eu
cd /opt/cleanup
aws s3 cp s3://{resources['Assets']}/source.bundle source.bundle --region us-east-1 --only-show-errors
if [ ! -d repo ]; then git clone source.bundle repo; fi
cd repo
test -z "$(git status --porcelain --untracked-files=no)"
git fetch ../source.bundle HEAD
git checkout --detach {sha}
git remote set-url origin https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt.git
mkdir -p .build
aws s3 cp s3://{resources['Assets']}/resources.json .build/aws-resources.json --region us-east-1 --only-show-errors
bash scripts/aws/prepare-tools.sh
bash scripts/aws/up.sh
'''
script='set -eu\ncloud-init status --wait\ntest -f /opt/cleanup/bootstrap-ready\nrunuser -l root -c '+shlex.quote(inner)+' > /opt/cleanup/setup.log 2>&1\n'
payload={'DocumentName':'AWS-RunShellScript','InstanceIds':[resources['Host']],'TimeoutSeconds':60,'Parameters':{'commands':[script],'executionTimeout':['7200']}}
(build/'command.json').write_text(json.dumps(payload))
for attempt in range(60):
    hosts=aws('ssm','describe-instance-information','--filters',json.dumps([{'Key':'InstanceIds','Values':[resources['Host']]}]))['InstanceInformationList']
    if hosts and hosts[0]['PingStatus']=='Online':break
    time.sleep(5)
else:raise SystemExit('SSM registration timed out; stack retained for inspection')
command=aws('ssm','send-command','--cli-input-json','file://'+str(build/'command.json'))['Command']['CommandId']
(build/'deployment.json').write_text(json.dumps({'revision':sha,'command_id':command,'resources':resources},indent=2))
print('Deployment started:',command,'on',resources['Host'])
print('Inspect /opt/cleanup/setup.log through SSM; start the local GitHub bridge pinned to',sha)
