#!/bin/bash
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl git unzip python3 docker.io docker-compose-v2 conntrack jq make
curl -fL --retry 3 https://awscli.amazonaws.com/awscli-exe-linux-x86_64-2.36.40.zip -o /tmp/aws.zip
echo 'a904e314340ad4c4b62beb50e9259d4becb2acf3dffc86c96c553c834d7fdf7a  /tmp/aws.zip' | sha256sum -c -
unzip -q /tmp/aws.zip -d /tmp/cleanup-cli
/tmp/cleanup-cli/aws/install
systemctl enable --now docker
mkdir -p /opt/cleanup
chmod 700 /opt/cleanup
# Stop each boot after eight hours. EBS, retained S3 and KMS still incur small charges.
cat >/etc/systemd/system/cleanup-demo-stop.service <<'EOF'
[Unit]
Description=Stop demonstration host after bounded session
[Service]
Type=oneshot
ExecStart=/sbin/shutdown -h now
EOF
cat >/etc/systemd/system/cleanup-demo-stop.timer <<'EOF'
[Unit]
Description=Eight hour demonstration cost guard
[Timer]
OnBootSec=8h
[Install]
WantedBy=timers.target
EOF
systemctl daemon-reload
systemctl enable --now cleanup-demo-stop.timer
# Containers cannot retrieve host credentials, including Docker bridge traffic.
iptables -I DOCKER-USER -d 169.254.169.254/32 -j REJECT
touch /opt/cleanup/bootstrap-ready
