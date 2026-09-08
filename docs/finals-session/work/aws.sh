#!/usr/bin/env bash
set -euo pipefail
base=/mnt/c/Users/Aditya/Documents/Codex/2026-09-08/i-have-a-hackathon-finals-round
export AWS_CONFIG_FILE="$base/work/aws-auth/config"
export AWS_SHARED_CREDENTIALS_FILE="$base/work/aws-auth/credentials"
export AWS_LOGIN_CACHE_DIRECTORY="$base/work/aws-auth/cache"
export AWS_PAGER=""
exec /tmp/cleanup-aws-cli/aws/dist/aws --profile cleanup-finals --region us-east-1 "$@"
