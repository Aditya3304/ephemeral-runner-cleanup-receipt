#!/usr/bin/env bash
set -euo pipefail
umask 077
for file in password ca.crt; do
  test -s "/source/$file"
  if [[ -e /database/$file ]]; then
    cmp -s "/source/$file" "/database/$file" || { echo 'Database credential changed; inspect before replacing runtime identity.' >&2; exit 1; }
  else
    cp "/source/$file" "/database/$file"
  fi
  chown 65532:65532 "/database/$file"
  chmod 600 "/database/$file"
done
if [[ ! -e /auth/token ]]; then openssl rand -hex 32 > /auth/token; fi
test "$(wc -c < /auth/token)" -eq 65
chown 65532:65532 /database /auth /auth/token /delivery
chmod 700 /database /auth /delivery
chmod 600 /auth/token
echo 'Restricted API credentials prepared; existing identities preserved.'
