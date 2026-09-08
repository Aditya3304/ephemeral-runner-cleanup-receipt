#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export API_IMAGE="$(cat .build/api-image-id)"
python3 scripts/api-config.py
docker compose -f compose.api.yaml run --rm --no-deps init
extra=()
if [[ "${API_RECREATE:-}" == 1 ]]; then extra+=(--force-recreate); fi
docker compose -f compose.api.yaml up -d "${extra[@]}" api gateway
python3 - <<'PY'
import time, urllib.request
for _ in range(30):
    try:
        with urllib.request.urlopen('http://127.0.0.1:8080/readyz', timeout=3) as r:
            if r.status == 200:
                print('Local receipt API ready at http://localhost:8080/v1/receipts')
                break
    except OSError:
        time.sleep(1)
else:
    raise SystemExit('API not ready; credentials and signed artifacts preserved.')
PY
