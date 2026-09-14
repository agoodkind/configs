#!/usr/bin/env bash
# Forced command for the Berylax updater SSH key. Berylax connects whenever its
# IPv4 address changes. The key authenticates the connection, so the address
# the connection comes from becomes the 6in4 tunnel remote.
#
# The updater account runs this unprivileged, and it re-executes itself through
# sudo. The sudoers rule allows exactly this path with no arguments and keeps
# only SSH_CONNECTION, so the root half re-derives the address itself.
set -euo pipefail

readonly SELF="/usr/local/sbin/sit6-set-remote"
readonly ENV_FILE="/etc/sit6/sit6.env"
readonly LOG_TAG="sit6-set-remote"
readonly LOCK_FILE="/run/sit6-set-remote.lock"
readonly TUNNEL_UNIT="sit6-tunnel.service"
readonly IPV4_OCTET="(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])"
readonly IPV4_PATTERN="^${IPV4_OCTET}\.${IPV4_OCTET}\.${IPV4_OCTET}\.${IPV4_OCTET}$"

log_info() {
    local message="$1"
    logger --tag "$LOG_TAG" --priority user.info -- "$message"
    echo "$message"
}

fail() {
    local message="$1"
    logger --tag "$LOG_TAG" --priority user.err -- "$message"
    echo "$message" >&2
    exit 1
}

main() {
    local client_address
    local previous_address
    local remote_file
    local staged_file

    if [[ $EUID -ne 0 ]]; then
        exec sudo --non-interactive "$SELF"
    fi

    remote_file=$(sed -n 's/^SIT6_REMOTE_FILE=//p' "$ENV_FILE")
    if [[ -z "$remote_file" ]]; then
        fail "SIT6_REMOTE_FILE is not set in $ENV_FILE"
    fi

    if [[ -z "${SSH_CONNECTION:-}" ]]; then
        fail "SSH_CONNECTION is empty; this runs only as the updater key's forced command"
    fi
    read -r client_address _ <<<"$SSH_CONNECTION"
    if [[ ! "$client_address" =~ $IPV4_PATTERN ]]; then
        fail "refusing tunnel remote '$client_address': not a single IPv4 address"
    fi

    # Serializes two connections that race, so the file and the tunnel end on
    # the same address.
    exec 9>"$LOCK_FILE"
    flock 9

    if [[ -s "$remote_file" ]]; then
        previous_address=$(<"$remote_file")
    else
        previous_address="none"
    fi

    staged_file=$(mktemp "$remote_file.XXXXXX")
    printf '%s\n' "$client_address" >"$staged_file"
    chmod 0644 "$staged_file"
    mv -f "$staged_file" "$remote_file"

    if ! systemctl restart "$TUNNEL_UNIT"; then
        fail "recorded tunnel remote $client_address (was $previous_address), but $TUNNEL_UNIT failed to apply it"
    fi
    log_info "tunnel remote changed from $previous_address to $client_address"
}

main "$@"
