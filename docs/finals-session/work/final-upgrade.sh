set -eu
systemctl stop cleanup-github.service
runuser -l root -c 'cd /opt/cleanup/repo && git fetch origin aws-github-finals && git checkout --detach FETCH_HEAD && bash scripts/aws/up.sh' > /opt/cleanup/final-upgrade.log 2>&1
