#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} != inside && ${1:-} != ready ]]; then
  cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
  docker compose -f compose.sigstore.yaml run --rm --no-deps client /scripts/sigstore-check.sh inside
  # Fresh process with no network namespace access. The trust root was exported
  # from the operator's volume, never accepted from an uploaded bundle.
  docker run --rm --network none --user 65532:65532 --read-only --tmpfs /tmp \
    --cap-drop ALL --security-opt no-new-privileges --memory 512m \
    --entrypoint /tools/cosign -e HOME=/tmp \
    -v "$PWD/.build/cosign:/tools/cosign:ro" -v "$PWD/.build/sigstore:/work:ro" \
    postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0 \
    verify-blob --offline --trusted-root /work/trusted-root.json --bundle /work/bundle.json \
    --certificate-identity finalizer@cleanup-receipt.local --certificate-oidc-issuer https://issuer:8443 /work/artifact.txt
  exit 0
fi
export HOME=/tmp
ready() { local count; for count in $(seq 1 40); do if /tools/http "$@" 2>/dev/null; then return; fi; sleep 1; done; /tools/http "$@"; }
ready --ca /run/finalizer/ca.crt https://issuer:8443/.well-known/openid-configuration
ready http://ctlog:6962/ct/v1/get-roots
ready http://fulcio:5555/api/v1/rootCert
ready http://rekor:3000/healthz
ready http://tsa:3000/api/v1/timestamp/certchain
echo 'Private issuer, Fulcio, CT log, Rekor and TSA respond.'
[[ $1 == ready ]] && exit 0
umask 077
tmp=$(mktemp -d)
trap 'rm -f "$tmp/token"; rmdir "$tmp"' EXIT
# This negative request must not mint any token without a client certificate.
/tools/http --ca /run/finalizer/ca.crt --method POST --status 401 https://issuer:8443/token
/tools/http --ca /run/finalizer/ca.crt --cert /run/finalizer/client.crt --key /run/finalizer/client.key \
  --method POST --json-field id_token --out "$tmp/token" https://issuer:8443/token
printf 'Private Sigstore upstream service smoke test\n' > /work/artifact.txt
/tools/cosign sign-blob --yes --oidc-disable-ambient-providers \
  --identity-token "$tmp/token" --signing-config /run/trust/signing-config.json \
  --trusted-root /run/trust/trusted-root.json --bundle /work/bundle.json /work/artifact.txt
cp /run/trust/trusted-root.json /work/trusted-root.json
/tools/cosign verify-blob --offline --trusted-root /run/trust/trusted-root.json --bundle /work/bundle.json \
  --certificate-identity finalizer@cleanup-receipt.local --certificate-oidc-issuer https://issuer:8443 /work/artifact.txt
printf 'Tampered\n' > /work/tampered.txt
if /tools/cosign verify-blob --offline --trusted-root /run/trust/trusted-root.json --bundle /work/bundle.json \
  --certificate-identity finalizer@cleanup-receipt.local --certificate-oidc-issuer https://issuer:8443 /work/tampered.txt >/dev/null 2>&1; then
  echo 'ERROR: tampered artifact was accepted' >&2; exit 1
fi
if /tools/cosign verify-blob --offline --trusted-root /run/trust/trusted-root.json --bundle /work/bundle.json \
  --certificate-identity attacker@cleanup-receipt.local --certificate-oidc-issuer https://issuer:8443 /work/artifact.txt >/dev/null 2>&1; then
  echo 'ERROR: wrong signer identity was accepted' >&2; exit 1
fi
if /tools/cosign verify-blob --offline --trusted-root /run/trust/trusted-root.json --bundle /work/bundle.json \
  --certificate-identity finalizer@cleanup-receipt.local --certificate-oidc-issuer https://wrong-issuer:8443 /work/artifact.txt >/dev/null 2>&1; then
  echo 'ERROR: wrong issuer was accepted' >&2; exit 1
fi
echo 'Real keyless bundle verified; tampering, wrong identity and wrong issuer rejected.'
