set -eu
export DEBIAN_FRONTEND=noninteractive
apt-get install -y docker-buildx
cd /opt/cleanup
aws s3 cp s3://cleanup-deploy-385020093237-us-east-1/source.bundle source.bundle --region us-east-1 --only-show-errors
git clone source.bundle repo
cd repo
git checkout -B aws-github-finals 059d98828d9edb03ea04c4df7faacbe0a4f61812
git remote set-url origin https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt.git
mkdir -p .build
umask 077
aws s3 cp s3://cleanup-deploy-385020093237-us-east-1/resources.json .build/aws-resources.json --region us-east-1 --only-show-errors
bash scripts/aws/up.sh > /opt/cleanup/setup.log 2>&1
