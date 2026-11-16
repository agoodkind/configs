#!/usr/bin/env bash
set -euo pipefail
cd /opt/tack-qa/data
mkdir -p /var/lib/tack-qa/bootstrap
if [[ ! -f /var/lib/tack-qa/bootstrap/fdb.complete ]]; then
    docker compose exec -T fdb fdbcli -C /etc/foundationdb/fdb.cluster --timeout 60 --exec 'status json' > /var/lib/tack-qa/bootstrap/fdb-status.json
    if jq -e '.client.database_status.available == true' /var/lib/tack-qa/bootstrap/fdb-status.json; then
        printf 'FoundationDB already has a database configuration.\n'
    elif jq -e '.cluster.recovery_state.name == "configuration_never_created"' /var/lib/tack-qa/bootstrap/fdb-status.json; then
        docker compose exec -T fdb fdbcli -C /etc/foundationdb/fdb.cluster --timeout 30 --exec 'configure new single ssd'
    else
        printf 'FoundationDB status does not establish an unconfigured database. The bootstrap script refuses initialization.\n' >&2
        jq '{available: .client.database_status.available, recovery_state: .cluster.recovery_state.name}' /var/lib/tack-qa/bootstrap/fdb-status.json >&2
        exit 1
    fi
    for i in {1..12}; do
        if ! docker compose exec -T fdb fdbcli -C /etc/foundationdb/fdb.cluster --timeout 5 --exec 'status json' > /var/lib/tack-qa/bootstrap/fdb-status.json; then
            printf 'FoundationDB status request failed during readiness verification.\n' >&2
            exit 1
        fi
        if jq -e '.client.database_status.available == true' /var/lib/tack-qa/bootstrap/fdb-status.json; then
            break
        fi
        printf 'FoundationDB readiness attempt %s failed.\n' "$i" >&2
        if [[ "$i" == 12 ]]; then
            exit 1
        fi
        sleep 2
    done
    install -m 0600 /dev/null /var/lib/tack-qa/bootstrap/fdb.complete
fi
if [[ ! -f /var/lib/tack-qa/bootstrap/ledger.complete ]]; then
    docker compose run -T --rm ledger-bootstrap ops provision --execute --operator-service tack-qa-bootstrap
    install -m 0600 /dev/null /var/lib/tack-qa/bootstrap/ledger.complete
fi
docker compose up -d audit-consumer
if [[ ! -f /var/lib/tack-qa/bootstrap/seed.complete ]]; then
    docker compose run -T --rm -e DATABASE_URL_FILE=/run/secrets/MIGRATOR_DATABASE_URL operator seed --execute --operator-service tack-qa-bootstrap > /dev/null
    install -m 0600 /dev/null /var/lib/tack-qa/bootstrap/seed.complete
fi
if [[ ! -f /var/lib/tack-qa/bootstrap/search.complete ]]; then
    docker compose run --rm operator ops search provision --execute --operator-service tack-qa-bootstrap
    install -m 0600 /dev/null /var/lib/tack-qa/bootstrap/search.complete
fi
