#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p outputs/demo-transcripts
transcript="outputs/demo-transcripts/$(date -u +%Y%m%dT%H%M%SZ)-$$.txt"
printf 'Full console transcript: %s\n' "$transcript"
python3 -u work/demo-console.py 2>&1 | tee "$transcript"
