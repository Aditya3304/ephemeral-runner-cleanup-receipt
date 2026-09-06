#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
target=internal/proof/testdata/sigstore
mkdir -p "$target"
base=https://raw.githubusercontent.com/sigstore/sigstore-conformance/9a5d9e9c5171eb56df7821e342eda7122f764b43/test/assets/bundle-verify
curl -fsSL "$base/a.txt" -o "$target/artifact.txt"
curl -fsSL "$base/happy-path-v0.3/bundle.sigstore.json" -o "$target/bundle.json"
curl -fsSL https://raw.githubusercontent.com/sigstore/cosign/v3.1.3/pkg/cosign/testdata/trusted_root_pgi.json -o "$target/trusted-root.json"
sha256sum "$target/artifact.txt" "$target/bundle.json" "$target/trusted-root.json"
