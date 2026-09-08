set -eu
systemctl stop cleanup-github.service
runuser -l root -c 'cd /opt/cleanup/repo && python3 scripts/metadata-backup.py create'
