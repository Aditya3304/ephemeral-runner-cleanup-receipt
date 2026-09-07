#!/usr/bin/env bash
set -euo pipefail
umask 077
public_config() {
  cp /secrets/admin/ca.crt /secrets/public/ca.crt
  cat > /secrets/public/verifier.json <<EOF
{"store":"local-minio","endpoint":"https://minio:9000","bucket":"receipt-archive","prefix":"evidence","region":"us-east-1","ca_file":"ca.crt","access_key_file":"/run/archive-verifier/access-key","secret_key_file":"/run/archive-verifier/secret-key","kms_key":"archive-key","max_object_bytes":104857600}
EOF
  chmod 755 /secrets/public
  chmod 644 /secrets/public/*
  chown -R 65532:65532 /secrets/finalizer /secrets/verifier
  chmod 700 /secrets/finalizer /secrets/verifier
  if ! grep -q /v1/key/create/archive-key /secrets/kes/config.yaml; then
    sed -i '/    allow:/a\      - /v1/key/create/archive-key' /secrets/kes/config.yaml
  fi
}
if [[ -f /secrets/admin/ready ]]; then
  for f in admin/access-key admin/secret-key admin/ca.key tls/private.key tls/public.crt tls/kes-client.key kes/config.yaml kes/server.key finalizer/config.json finalizer/secret-key verifier/config.json verifier/secret-key; do
    [[ -s /secrets/$f ]] || { echo 'Incomplete archive identity; refusing regeneration.' >&2; exit 1; }
  done
  public_config
  echo 'Preserved existing archive identity.'
  exit 0
fi
if find /secrets -type f | read -r _; then
  echo 'Partial archive bootstrap; inspect existing volumes. No identities overwritten.' >&2
  exit 1
fi
mkdir -p /secrets/tls/CAs
openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 3650 -subj '/CN=Local Archive CA' -keyout /secrets/admin/ca.key -out /secrets/admin/ca.crt 2>/dev/null
issue() {
  local name=$1 base=$2 usage=$3 san=$4
  openssl req -new -newkey rsa:3072 -nodes -subj "/CN=$name" -keyout "$base.key" -out /secrets/admin/request.csr 2>/dev/null
  printf 'basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=%s\nsubjectAltName=%s\n' "$usage" "$san" > /secrets/admin/request.ext
  openssl x509 -req -in /secrets/admin/request.csr -CA /secrets/admin/ca.crt -CAkey /secrets/admin/ca.key -CAcreateserial -days 825 -sha256 -extfile /secrets/admin/request.ext -out "$base.crt" 2>/dev/null
}
issue minio /secrets/tls/server serverAuth DNS:minio
mv /secrets/tls/server.key /secrets/tls/private.key
mv /secrets/tls/server.crt /secrets/tls/public.crt
issue kes /secrets/kes/server serverAuth DNS:kes
issue minio-kes /secrets/tls/kes-client clientAuth DNS:minio
identity=$(openssl x509 -in /secrets/tls/kes-client.crt -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1)
cp /secrets/admin/ca.crt /secrets/tls/CAs/ca.crt
cp /secrets/admin/ca.crt /secrets/kes/ca.crt
cat > /secrets/kes/config.yaml <<EOF
version: v1
address: 0.0.0.0:7373
admin:
  identity: disabled
tls:
  key: /secrets/kes/server.key
  cert: /secrets/kes/server.crt
  ca: /secrets/kes/ca.crt
  auth: on
policy:
  minio:
    allow:
      - /v1/key/create/archive-key
      - /v1/key/generate/archive-key
      - /v1/key/decrypt/archive-key
      - /v1/key/bulk/decrypt/archive-key
    identities: [$identity]
keys:
  - name: archive-key
keystore:
  fs:
    path: /keys
EOF
for role in admin finalizer verifier; do
  printf 'archive-%s-' "$role" > /secrets/$role/access-key
  openssl rand -hex 8 >> /secrets/$role/access-key
  openssl rand -hex 32 > /secrets/$role/secret-key
  [[ $role == admin ]] || cp /secrets/admin/ca.crt /secrets/$role/ca.crt
  cat > /secrets/$role/config.json <<EOF
{"store":"local-minio","endpoint":"https://minio:9000","bucket":"receipt-archive","prefix":"evidence","region":"us-east-1","ca_file":"ca.crt","access_key_file":"access-key","secret_key_file":"secret-key","kms_key":"archive-key","max_object_bytes":104857600}
EOF
done
touch /secrets/admin/ready
public_config
echo 'Archive CA, credentials and KES identity generated only in Docker volumes.'
