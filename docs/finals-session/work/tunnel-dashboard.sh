#!/usr/bin/env bash
set -euo pipefail
cd /mnt/c/Users/Aditya/Documents/Codex/2026-09-08/i-have-a-hackathon-finals-round
export PATH="/tmp/cleanup-ssm/usr/local/sessionmanagerplugin/bin:$PATH"
bash work/aws.sh ssm start-session --target i-0f21b25d3a04d8541 --document-name AWS-StartPortForwardingSession --parameters '{"portNumber":["8080"],"localPortNumber":["18080"]}'
