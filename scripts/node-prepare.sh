#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
mkdir -p .build
archive=.build/node-v24.20.0-linux-x64.tar.xz
digest=2f2c0da162318f0de47665410c7c8c2ed3d36c8f3105de4bbc61176c70a7cbf2
if ! printf '%s  %s\n' "$digest" "$archive" | sha256sum --check --status 2>/dev/null; then
  curl --fail --location --retry 3 https://nodejs.org/dist/v24.20.0/node-v24.20.0-linux-x64.tar.xz -o "$archive"
fi
printf '%s  %s\n' "$digest" "$archive" | sha256sum --check
tar -xJf "$archive" -C .build
.build/node-v24.20.0-linux-x64/bin/node --version
