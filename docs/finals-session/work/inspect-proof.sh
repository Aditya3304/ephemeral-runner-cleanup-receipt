systemctl stop cleanup-github.service
chown -R 65532:65532 /opt/cleanup/repo/.build/finalizer-trust
find /opt/cleanup/repo/.build/coordinator -name run.json -exec cat {} \;
find /opt/cleanup/repo/.build/coordinator -name observations.json -exec cat {} \;
systemctl start cleanup-github.service
