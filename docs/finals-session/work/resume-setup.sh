set -eu
runuser -l root -c 'cd /opt/cleanup/repo && bash scripts/aws/up.sh > /opt/cleanup/setup.log 2>&1'
