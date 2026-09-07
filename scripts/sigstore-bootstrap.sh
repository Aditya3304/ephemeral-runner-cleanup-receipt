#!/usr/bin/env bash
# Runs ONLY in the offline bootstrap container. Keys never enter the checkout.
set -euo pipefail
umask 077
# Preserve the key itself while normalizing formats required by the pinned
# servers. Also upgrades the first bootstrap revision without rotating trust.
normalize_keys() {
  cp /run/finalizer/ca.crt /run/finalizer/issuer-ca.crt
  chown 65532:65532 /run/finalizer/issuer-ca.crt
  if ! head -1 /run/ctlog/key.pem | grep -q 'BEGIN EC PRIVATE KEY'; then
    openssl ec -in /run/ctlog/key.pem -out /run/ctlog/key.sec1.pem 2>/dev/null
    mv /run/ctlog/key.sec1.pem /run/ctlog/key.pem
  fi
  if [[ ! -s /run/fulcio/server.yaml ]]; then
    local password
    password=$(openssl rand -hex 32)
    printf '%s' "$password" > /run/admin/fulcio-password
    openssl pkcs8 -topk8 -in /run/fulcio/key.pem -out /run/fulcio/key.encrypted.pem \
      -passout file:/run/admin/fulcio-password
    mv /run/fulcio/key.encrypted.pem /run/fulcio/key.pem
    printf 'fileca-key-passwd: "%s"\n' "$password" > /run/fulcio/server.yaml
    unset password
  fi
  chown 65532:65532 /run/ctlog/key.pem /run/fulcio/key.pem /run/fulcio/server.yaml
}
required=(/run/issuer/signing.key /run/issuer/server.key /run/issuer/server.crt /run/issuer/client-ca.crt
  /run/finalizer/client.key /run/finalizer/client.crt /run/finalizer/ca.crt
  /run/fulcio/key.pem /run/fulcio/cert.pem /run/fulcio/config.yaml
  /run/ctlog/key.pem /run/ctlog/fulcio-root.pem /run/rekor/key.pem
  /run/tsa/key.pem /run/tsa/chain.pem /run/trust/trusted-root.json /run/trust/signing-config.json)
if [[ -f /run/admin/ready ]]; then
  for p in "${required[@]}"; do [[ -s "$p" ]] || { echo "Incomplete existing bootstrap: $p" >&2; exit 1; }; done
  normalize_keys
  openssl verify -CAfile /run/finalizer/ca.crt -purpose sslserver /run/issuer/server.crt
  openssl verify -CAfile /run/issuer/client-ca.crt -purpose sslclient /run/finalizer/client.crt
  echo 'Existing Sigstore keys, roots, credentials and log data preserved.'
  exit 0
fi
for d in /run/admin /run/issuer /run/finalizer /run/fulcio /run/ctlog /run/rekor /run/tsa /run/trust /data/ctlog /data/rekor; do
  if [[ -n "$(find "$d" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
    echo "Partial bootstrap or existing log data in $d; refusing to generate replacement keys." >&2; exit 1
  fi
done
ec_key() { openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$1"; }
ca() {
  ec_key "$1.key"
  openssl req -new -x509 -key "$1.key" -out "$1.crt" -days 3650 -sha256 -subj "/CN=$2" \
    -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign'
}
leaf() {
  local prefix=$1 caprefix=$2 cn=$3 extensions=$4
  ec_key "$prefix.key"
  openssl req -new -key "$prefix.key" -out /run/admin/leaf.csr -subj "/CN=$cn"
  printf '%s\n' "$extensions" > /run/admin/leaf.ext
  openssl x509 -req -in /run/admin/leaf.csr -CA "$caprefix.crt" -CAkey "$caprefix.key" \
    -set_serial "0x$(openssl rand -hex 16)" -out "$prefix.crt" -days 365 -sha256 -extfile /run/admin/leaf.ext
}
ca /run/admin/issuer-ca 'Cleanup Receipt Issuer TLS CA'
leaf /run/issuer/server /run/admin/issuer-ca issuer $'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=serverAuth\nsubjectAltName=DNS:issuer'
ca /run/admin/client-ca 'Cleanup Receipt Finalizer Client CA'
leaf /run/finalizer/client /run/admin/client-ca cleanup-receipt-finalizer $'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=clientAuth\nsubjectAltName=URI:spiffe://cleanup-receipt.local/finalizer'
cp /run/admin/client-ca.crt /run/issuer/client-ca.crt
cp /run/admin/issuer-ca.crt /run/finalizer/ca.crt
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out /run/issuer/signing.key 2>/dev/null

# Fulcio's long-lived CA key signs short-lived certificates; Cosign creates each
# artifact's fresh ephemeral key. None of these service keys signs the receipt.
ca /run/admin/fulcio 'Cleanup Receipt Fulcio Root'
cp /run/admin/fulcio.key /run/fulcio/key.pem
cp /run/admin/fulcio.crt /run/fulcio/cert.pem
cp /run/admin/fulcio.crt /run/ctlog/fulcio-root.pem
ec_key /run/ctlog/key.pem
openssl pkey -in /run/ctlog/key.pem -pubout -out /run/trust/ctlog.pub
openssl genpkey -algorithm ed25519 -out /run/rekor/key.pem
openssl pkey -in /run/rekor/key.pem -pubout -out /run/trust/rekor.pub
ca /run/admin/tsa-root 'Cleanup Receipt Timestamp Root'
leaf /run/tsa/leaf /run/admin/tsa-root 'Cleanup Receipt Timestamp Signer' $'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=critical,timeStamping'
mv /run/tsa/leaf.key /run/tsa/key.pem
cat /run/tsa/leaf.crt /run/admin/tsa-root.crt > /run/tsa/chain.pem
cp /run/admin/fulcio.crt /run/trust/fulcio-root.pem
cp /run/tsa/chain.pem /run/trust/tsa-chain.pem
cp /run/admin/issuer-ca.crt /run/trust/issuer-ca.crt
cat > /run/fulcio/config.yaml <<'YAML'
oidc-issuers:
  https://issuer:8443:
    issuer-url: https://issuer:8443
    client-id: sigstore
    type: email
    ca-cert: |
YAML
sed 's/^/      /' /run/admin/issuer-ca.crt >> /run/fulcio/config.yaml

# Validity starts at bootstrap, never at a made-up historic signing time.
start=$(date -u +%Y-%m-%dT%H:%M:%SZ)
/tools/cosign trusted-root create \
  --fulcio="url=http://fulcio:5555,certificate-chain=/run/trust/fulcio-root.pem,start-time=$start" \
  --ctfe="url=http://ctlog:6962,public-key=/run/trust/ctlog.pub,start-time=$start" \
  --rekor="url=http://rekor:3000,public-key=/run/trust/rekor.pub,origin=rekor,start-time=$start" \
  --tsa="url=http://tsa:3000/api/v1/timestamp,certificate-chain=/run/trust/tsa-chain.pem,start-time=$start" \
  --out /run/trust/trusted-root.json
/tools/cosign signing-config create \
  --fulcio="url=http://fulcio:5555,api-version=1,start-time=$start,operator=cleanup-receipt.local" \
  --rekor="url=http://rekor:3000,api-version=2,start-time=$start,operator=cleanup-receipt.local" --rekor-config=ALL \
  --tsa="url=http://tsa:3000/api/v1/timestamp,api-version=1,start-time=$start,operator=cleanup-receipt.local" --tsa-config=ALL \
  --oidc-provider="url=https://issuer:8443,api-version=1,start-time=$start,operator=cleanup-receipt.local" \
  --out /run/trust/signing-config.json
normalize_keys
for d in /run/issuer /run/finalizer /run/fulcio /run/ctlog /run/rekor /run/tsa /run/trust /data/ctlog /data/rekor; do
  chown -R 65532:65532 "$d"
  chmod 700 "$d"
done
chmod 700 /run/admin
touch /run/admin/ready
echo 'Generated private Sigstore trust and service credentials in isolated Docker volumes.'
