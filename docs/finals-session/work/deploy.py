"""Stage committed source and public resource identifiers only; no GitHub credential export."""
import json, pathlib, subprocess
root=pathlib.Path(__file__).resolve().parent.parent
repo=root/'outputs/ephemeral-runner-cleanup-receipt-aws'
resources={r['OutputKey']:r['OutputValue'] for r in json.loads((root/'work/stack-outputs.json').read_text())}
aws=['bash',str(root/'work/aws.sh')]
sha=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip()
subprocess.run(['git','bundle','create',str(root/'work/source.bundle'),'HEAD'],cwd=repo,check=True)
subprocess.run(aws+['s3','cp',str(root/'work/source.bundle'),'s3://'+resources['Assets']+'/source.bundle','--only-show-errors'],check=True)
(root/'work/resources.json').write_text(json.dumps(resources))
subprocess.run(aws+['s3','cp',str(root/'work/resources.json'),'s3://'+resources['Assets']+'/resources.json','--only-show-errors'],check=True)
script=f'''set -eu
export DEBIAN_FRONTEND=noninteractive
apt-get install -y docker-buildx
cd /opt/cleanup
aws s3 cp s3://{resources['Assets']}/source.bundle source.bundle --region us-east-1 --only-show-errors
git clone source.bundle repo
cd repo
git checkout -B aws-github-finals {sha}
git remote set-url origin https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt.git
mkdir -p .build
umask 077
aws s3 cp s3://{resources['Assets']}/resources.json .build/aws-resources.json --region us-east-1 --only-show-errors
bash scripts/aws/up.sh > /opt/cleanup/setup.log 2>&1
'''
(root/'work/deploy-host.sh').write_text(script)
print('Committed deployment prepared:',sha)
