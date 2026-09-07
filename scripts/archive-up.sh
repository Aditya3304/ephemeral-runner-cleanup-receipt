#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose -f compose.archive.yaml up -d --pull never kes minio
docker compose -f compose.archive.yaml run --rm --no-deps provision
