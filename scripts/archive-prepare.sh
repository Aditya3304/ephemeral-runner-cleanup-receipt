#!/usr/bin/env bash
# Setup only: downloads are explicit here. Runtime scripts never pull.
set -euo pipefail
cd "$(dirname "$0")/.."
for image in \
  postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0 \
  quay.io/minio/minio@sha256:a1ea29fa28355559ef137d71fc570e508a214ec84ff8083e39bc5428980b015e \
  quay.io/minio/mc@sha256:aead63c77f9db9107f1696fb08ecb0faeda23729cde94b0f663edf4fe09728e3 \
  quay.io/minio/kes@sha256:bb97b121b03b0acd04eecea63a5909ec2e56eab1a48e0cc418036b67502a64b0; do
  docker pull "$image"
done
export PATH=/usr/local/go/bin:$PATH
go mod download
echo 'Archive images and Go dependencies cached for offline startup and checks.'
