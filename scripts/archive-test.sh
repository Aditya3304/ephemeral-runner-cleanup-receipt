#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH=/usr/local/go/bin:$PATH GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
mkdir -p infra/archive/work
go test -p 2 ./internal/archive ./cmd/archivectl
CGO_ENABLED=0 go test -p 2 -c -o infra/archive/work/archive.test ./internal/archive
CGO_ENABLED=0 go build -p 2 -o infra/archive/work/archivectl ./cmd/archivectl
docker compose -f compose.archive.yaml run --rm --no-deps test
printf 'Archive UID 65532 upload and independent verification probe.\n' > infra/archive/work/smoke.log
image=postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0
docker run --rm --pull never --read-only --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
  --memory 128m --cpus 0.5 --pids-limit 128 --network cleanup-receipt-archive \
  -v "$PWD/infra/archive/work:/work:ro" -v cleanup-receipt-archive_finalizer:/run/archive:ro \
  --entrypoint /work/archivectl "$image" put --config /run/archive/config.json --file /work/smoke.log --name job.log \
  > infra/archive/work/smoke-ref.json
docker run --rm --pull never --read-only --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
  --memory 128m --cpus 0.5 --pids-limit 128 --network cleanup-receipt-archive \
  -v "$PWD/infra/archive/work:/work:ro" -v cleanup-receipt-archive_verifier:/run/archive-verifier:ro \
  -v cleanup-receipt-archive_public:/run/archive-public:ro \
  --entrypoint /work/archivectl "$image" verify --config /run/archive-public/verifier.json --reference /work/smoke-ref.json
echo 'UID 65532 finalizer upload and isolated verifier credential mounts passed.'
