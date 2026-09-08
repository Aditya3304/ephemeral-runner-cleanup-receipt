tail -18 /opt/cleanup/final-upgrade.log; systemctl is-active cleanup-stack cleanup-github cleanup-github-proxy cleanup-github-broker; journalctl -u cleanup-github -n 6 --no-pager
