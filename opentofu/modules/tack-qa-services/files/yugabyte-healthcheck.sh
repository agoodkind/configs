#!/usr/bin/env bash
set -euo pipefail
PGPASSWORD=$(cat /run/secrets/YUGABYTE_PASSWORD)
export PGPASSWORD
export PGSSLMODE=verify-full
export PGSSLROOTCERT=/etc/tack/ledger-certs/ca.crt
exec /home/yugabyte/bin/ysqlsh -h yugabyte -U yugabyte -d tack -c 'SELECT 1'
