set -eu
systemctl stop cleanup-github.service
runuser -l root -c 'cd /opt/cleanup/repo && git fetch origin aws-github-finals && git checkout --detach 9605c536dd49faeb3acf6a80a60ec1136ad604c0 && bash scripts/aws/up.sh' > /opt/cleanup/experiment-upgrade.log 2>&1
