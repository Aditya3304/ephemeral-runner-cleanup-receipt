tail -25 /opt/cleanup/api-update.log; journalctl -u cleanup-github.service -n 12 --no-pager; ls /opt/cleanup/repo/.build/github-delivered 2>/dev/null || true
