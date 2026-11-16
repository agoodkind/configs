#!/usr/bin/env bash
set -euo pipefail
readonly READINESS_ATTEMPTS=60
readonly READINESS_DELAY_SECONDS=2
readonly INITIAL_YSQL_PASSWORD=yugabyte
readonly INITIALIZATION_MARKER=/home/yugabyte/var/.tack-qa-credentials-initialized
export PGSSLMODE=verify-full
export PGSSLROOTCERT=/etc/tack/ledger-certs/ca.crt
export PGCONNECT_TIMEOUT=5
export LC_ALL=C

ysql() {
    /home/yugabyte/bin/ysqlsh -X -w -v ON_ERROR_STOP=1 -h yugabyte -U yugabyte -d yugabyte "$@"
}

for ((attempt_count = 1; attempt_count <= READINESS_ATTEMPTS; attempt_count++)); do
    if /home/yugabyte/postgres/bin/pg_isready -h yugabyte -U yugabyte -d yugabyte -t 2; then
        break
    fi
    printf 'YSQL readiness attempt %s failed.\n' "$attempt_count" >&2
    if [[ "$attempt_count" == "$READINESS_ATTEMPTS" ]]; then
        printf 'YSQL did not accept connections within the bootstrap deadline.\n' >&2
        exit 1
    fi
    sleep "$READINESS_DELAY_SECONDS"
done

PGPASSWORD=$(cat /run/secrets/YUGABYTE_PASSWORD)
if [[ -z "$PGPASSWORD" ]]; then
    printf 'The required QA credential file is empty. Credential reconciliation stopped.\n' >&2
    exit 1
fi
export PGPASSWORD
if connection_result=$(ysql -f /opt/tack/ready.sql 2>&1); then
    printf 'YSQL authenticated with the required QA credential.\n'
else
    printf '%s\n' "$connection_result" >&2
    if [[ -e "$INITIALIZATION_MARKER" ]]; then
        printf 'QA engine initialization already completed. The initializer requires the configured QA credential.\n' >&2
        exit 1
    fi
    if [[ "$connection_result" != *'password authentication failed for user "yugabyte"'* ]]; then
        printf 'YSQL could not verify the required QA credential. Credential reconciliation stopped.\n' >&2
        exit 1
    fi
    printf 'YSQL rejected the required QA credential. The initializer will authenticate with the initial engine credential.\n' >&2
    export PGPASSWORD="$INITIAL_YSQL_PASSWORD"
    if ! ysql -f /opt/tack/ready.sql; then
        printf 'YSQL rejected both QA bootstrap credentials. Credential reconciliation stopped.\n' >&2
        exit 1
    fi
    ysql -f /opt/tack/password.sql
    PGPASSWORD=$(cat /run/secrets/YUGABYTE_PASSWORD)
    export PGPASSWORD
fi
ysql -f /opt/tack/ready.sql
ysql -f /opt/tack/database.sql
/home/yugabyte/bin/ysqlsh -X -w -v ON_ERROR_STOP=1 -h yugabyte -U yugabyte -d tack -f /opt/tack/ready.sql
if [[ ! -e "$INITIALIZATION_MARKER" ]]; then
    install -o root -g root -m 0600 /dev/null "$INITIALIZATION_MARKER"
fi
