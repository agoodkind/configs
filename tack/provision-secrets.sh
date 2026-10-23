#!/usr/bin/env bash
# Generate missing credentials and adopt existing SQL credentials during conversion.
set -euo pipefail
umask 077

readonly SECRET_DIR="${1:?Specify the secret directory}"
readonly LEGACY_ENV="${2:?Specify the previous environment file}"
readonly APP_PASSWORD_FILE="${3:?Specify the previous application password file}"
readonly PASSWORD_NAMES=(
    YUGABYTE_PASSWORD AUDIT_WRITER_PASSWORD AUDIT_READER_PASSWORD
    AUDIT_REDACTOR_PASSWORD AUDIT_OPERATOR_PASSWORD TACK_APP_PASSWORD
    TACK_MIGRATOR_PASSWORD
)
readonly SEARCH_NAMES=(OPENSEARCH_PASSWORD OPENSEARCH_MAINTENANCE_PASSWORD OPENSEARCH_HEALTH_PASSWORD)
CHANGED=false

require_secret() {
    local name="$1"
    if [[ ! -s "${SECRET_DIR}/${name}" ]]; then
        printf 'Credential file %s is empty; restore its previous value before retrying.\n' "${SECRET_DIR}/${name}" >&2
        exit 1
    fi
    chmod 0600 "${SECRET_DIR}/${name}"
}

adopt_previous() {
    local name="$1"
    local line
    local key
    local value
    local lines=()
    if [[ "$name" == TACK_APP_PASSWORD && -f "$APP_PASSWORD_FILE" ]]; then
        cp "$APP_PASSWORD_FILE" "${SECRET_DIR}/${name}"
        return
    fi
    if [[ ! -f "$LEGACY_ENV" ]]; then
        return
    fi
    mapfile -t lines <"$LEGACY_ENV"
    for line in "${lines[@]}"; do
        key="${line%%=*}"
        value="${line#*=}"
        if [[ "$key" == "$name" && -n "$value" ]]; then
            printf '%s\n' "$value" >"${SECRET_DIR}/${name}"
            return
        fi
    done
}

provision_password() {
    local name="$1"
    local adopt="$2"
    if [[ -e "${SECRET_DIR}/${name}" ]]; then
        require_secret "$name"
        return
    fi
    if [[ "$adopt" == true ]]; then
        adopt_previous "$name"
    fi
    if [[ ! -e "${SECRET_DIR}/${name}" ]]; then
        openssl rand -hex -out "${SECRET_DIR}/${name}" 32
    fi
    require_secret "$name"
    CHANGED=true
}

install -d -m 0700 "$SECRET_DIR"
exec 9>"${SECRET_DIR}/.provision.lock"
flock -x 9
for name in "${PASSWORD_NAMES[@]}"; do
    provision_password "$name" true
done
for name in "${SEARCH_NAMES[@]}"; do
    provision_password "$name" false
done
provision_password CLICKHOUSE_PASSWORD false
if [[ ! -e "${SECRET_DIR}/OPENSEARCH_CURSOR_KEY" ]]; then
    adopt_previous OPENSEARCH_CURSOR_KEY
    if [[ ! -e "${SECRET_DIR}/OPENSEARCH_CURSOR_KEY" ]]; then
        openssl rand -base64 -out "${SECRET_DIR}/OPENSEARCH_CURSOR_KEY" 32
    fi
    CHANGED=true
fi
require_secret OPENSEARCH_CURSOR_KEY
for name in "${SEARCH_NAMES[@]}"; do
    hash_file="${SECRET_DIR}/${name}.hash"
    if [[ ! -e "$hash_file" ]] || ! htpasswd -vi "$hash_file" "$name" <"${SECRET_DIR}/${name}"; then
        htpasswd -niBC 12 "$name" <"${SECRET_DIR}/${name}" >"$hash_file"
        CHANGED=true
    fi
    require_secret "${name}.hash"
done
if [[ "$CHANGED" == true ]]; then
    printf 'changed\n'
fi
