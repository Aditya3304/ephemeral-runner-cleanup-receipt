#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
mkdir -p .build
exec 9>.build/release-lifecycle.lock
flock -w 30 9 || { echo 'Another release lifecycle command is active.' >&2; exit 1; }

[[ "$(cat VERSION)" == 0.1.0 ]] || { echo 'Unexpected release version.' >&2; exit 1; }
for file in .build/dbtool .build/kind .build/kubeconfig .build/runner-image .build/finalizer-image-id .build/api-image-id .build/watchdog .build/cosign; do
  [[ -s "$file" ]] || { echo "Prepared local asset missing: $file" >&2; exit 1; }
done

# Every command below consumes only prepared local files, images, and volumes.
# Pull policies are disabled and package managers are not invoked at runtime.
bash dev up
bash scripts/kind-up.sh
bash scripts/policy-up.sh
bash scripts/archive-up.sh
bash scripts/sigstore-up.sh
bash scripts/api-up.sh
python3 scripts/watchdog-install.py --service
python3 scripts/release-status.py
echo 'Local release is ready at http://localhost:8080/. No runtime downloads were used.'
