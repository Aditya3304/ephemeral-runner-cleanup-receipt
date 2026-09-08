systemctl --no-pager --full status cleanup-github.service cleanup-github-proxy.service cleanup-github-broker.service | tail -55
journalctl -u cleanup-github.service -n 40 --no-pager
docker ps --format '{{.Names}} {{.Status}}'
find /opt/cleanup/repo/.build/coordinator -name run.json -exec cat {} \;
