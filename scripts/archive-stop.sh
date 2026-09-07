#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Stop only these services; never delete archive volumes or touch PostgreSQL.
docker compose -f compose.archive.yaml stop minio kes
