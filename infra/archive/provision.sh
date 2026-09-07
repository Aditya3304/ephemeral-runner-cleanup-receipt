#!/bin/sh
set -eu
umask 077
export MC_CONFIG_DIR=/tmp/mc
mkdir -p "$MC_CONFIG_DIR/certs/CAs"
cp /secrets/admin/ca.crt "$MC_CONFIG_DIR/certs/CAs/ca.crt"
n=0
until mc alias set local https://minio:9000 "$(cat /secrets/admin/access-key)" "$(cat /secrets/admin/secret-key)" >/dev/null 2>&1; do
  n=$((n+1)); [ "$n" -lt 60 ] || { echo 'MinIO TLS/credentials readiness timeout'; exit 1; }; sleep 2
done
n=0
until mc ready local >/dev/null 2>&1; do
  n=$((n+1)); [ "$n" -lt 60 ] || { echo 'MinIO readiness timeout'; exit 1; }; sleep 2
done
mc mb --ignore-existing --with-lock local/receipt-archive
mc version enable local/receipt-archive
mc retention set --default COMPLIANCE 7d local/receipt-archive
mc admin kms key create local archive-key >/dev/null 2>&1 || mc admin kms key status local archive-key
mc encrypt set sse-kms archive-key local/receipt-archive
for role in finalizer verifier; do
  mc admin user add local "$(cat /secrets/$role/access-key)" "$(cat /secrets/$role/secret-key)" >/dev/null
  mc admin policy create local "archive-$role" "/policies/$role.json"
  mc admin policy attach local "archive-$role" --user "$(cat /secrets/$role/access-key)"
done
mc version info local/receipt-archive
mc retention info --default local/receipt-archive
mc encrypt info local/receipt-archive
