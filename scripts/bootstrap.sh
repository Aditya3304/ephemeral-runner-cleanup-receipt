#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ -f /run/admin/ready ]]; then
  for file in /run/admin/password /run/admin/init-role.sql /run/api/password /run/api/ca.crt /run/tls/server.crt /run/tls/server.key; do
    [[ -s "$file" ]] || { echo 'Incomplete local secrets; refusing to overwrite existing identity.' >&2; exit 1; }
  done
  openssl verify -CAfile /run/api/ca.crt /run/tls/server.crt
  echo 'Existing local credentials and TLS preserved.'
  exit 0
fi
if [[ -e /run/admin/password || -e /run/api/password || -e /run/tls/server.key ]]; then
  echo 'Partial bootstrap detected; existing secrets were preserved. Inspect before recovery.' >&2
  exit 1
fi

openssl rand -hex 32 > /run/admin/password
openssl rand -hex 32 > /run/api/password
openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 3650 \
  -subj '/CN=Cleanup Receipt Local Database CA' \
  -keyout /run/admin/ca.key -out /run/api/ca.crt 2>/dev/null
openssl req -new -newkey rsa:3072 -nodes -sha256 -subj '/CN=db' \
  -keyout /run/tls/server.key -out /run/admin/server.csr 2>/dev/null
printf 'subjectAltName=DNS:db\nbasicConstraints=CA:FALSE\nextendedKeyUsage=serverAuth\nkeyUsage=digitalSignature,keyEncipherment\n' > /run/admin/server.ext
openssl x509 -req -in /run/admin/server.csr -CA /run/api/ca.crt -CAkey /run/admin/ca.key \
  -CAcreateserial -out /run/tls/server.crt -days 825 -sha256 -extfile /run/admin/server.ext 2>/dev/null

api_password=$(cat /run/api/password)
# Passwords are generated hexadecimal, never user-controlled SQL fragments.
cat > /run/admin/init-role.sql <<SQL
CREATE ROLE proof_api LOGIN PASSWORD '$api_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;
ALTER ROLE proof_api SET statement_timeout = '15s';
ALTER ROLE proof_api SET idle_in_transaction_session_timeout = '30s';
REVOKE ALL ON DATABASE proof FROM PUBLIC;
GRANT CONNECT ON DATABASE proof TO proof_api;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
SQL
unset api_password
chown -R postgres:postgres /run/admin /run/api /run/tls
chmod 700 /run/admin /run/api /run/tls
chmod 600 /run/admin/* /run/api/* /run/tls/*
chmod 644 /run/api/ca.crt /run/tls/server.crt
touch /run/admin/ready
echo 'Generated local database credentials and CA inside Docker volumes; no secret files in the repository.'
