#!/usr/bin/env bash
set -euo pipefail
psql --username postgres --dbname proof --set ON_ERROR_STOP=1 --file /run/admin/init-role.sql
