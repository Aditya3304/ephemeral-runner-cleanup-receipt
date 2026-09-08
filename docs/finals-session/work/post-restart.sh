systemctl is-active cleanup-stack cleanup-github cleanup-github-proxy cleanup-github-broker cleanup-metadata-block
systemctl show cleanup-demo-stop.timer -p NextElapseUSecMonotonic -p ActiveState
uptime -s
journalctl -u cleanup-stack -b --no-pager | tail -18
curl -fsS http://127.0.0.1:8080/readyz
