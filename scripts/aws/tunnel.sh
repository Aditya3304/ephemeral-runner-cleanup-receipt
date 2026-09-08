#!/usr/bin/env bash
set -euo pipefail
# Usage: AWS_PROFILE=... bash scripts/aws/tunnel.sh INSTANCE bridge|dashboard
[[ "${1:-}" =~ ^i-[0-9a-f]+$ ]]
case "${2:-}" in
  bridge) parameters='{"portNumber":["8123"],"localPortNumber":["18123"]}' ;;
  dashboard) parameters='{"portNumber":["8080"],"localPortNumber":["18080"]}' ;;
  *) echo 'Choose bridge or dashboard'; exit 2 ;;
esac
aws --region us-east-1 ssm start-session --target "$1" --document-name AWS-StartPortForwardingSession --parameters "$parameters"
