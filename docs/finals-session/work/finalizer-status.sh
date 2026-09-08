journalctl -u cleanup-github.service -n 25 --no-pager; find /opt/cleanup/repo/.build/finalized -name receipt.json -exec cat {} \;
