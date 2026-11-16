#!/usr/bin/env bash
set -euo pipefail
YSQL_PASSWORD=$(cat /run/secrets/YUGABYTE_PASSWORD)
export YSQL_PASSWORD
exec "$@"
