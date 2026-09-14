#!/usr/bin/env bash
# Bring up or refresh the 6in4 tunnel to Berylax. The remote is the A record of
# SIT6_TUNNEL_REMOTE_NAME; AAAA records are ignored. sit6-tunnel.service runs
# this at boot and sit6-tunnel-refresh.timer re-runs it, so every step
# converges: the remote changes only when DNS moves, and a failed lookup keeps
# the current tunnel instead of tearing it down. The systemd units supply the
# SIT6_* settings from /etc/sit6/sit6.env.
set -euo pipefail

readonly LOG_PREFIX="sit6-tunnel:"

# Prints each distinct IPv4 address of the remote name, one per line. getent
# prints every address once per socket type, so repeats are dropped.
resolve_ipv4_addresses() {
    local lookup_output
    local address
    local -A seen_addresses=()

    lookup_output=$(getent ahostsv4 "$SIT6_TUNNEL_REMOTE_NAME") || return $?
    while read -r address _; do
        if [[ -z "$address" ]]; then
            continue
        fi
        if [[ -z "${seen_addresses[$address]:-}" ]]; then
            seen_addresses[$address]=1
            printf '%s\n' "$address"
        fi
    done <<<"$lookup_output"
}

current_remote() {
    local tunnel_line

    tunnel_line=$(ip tunnel show "$SIT6_TUNNEL_IFACE")
    if [[ "$tunnel_line" =~ remote\ ([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+) ]]; then
        printf '%s\n' "${BASH_REMATCH[1]}"
    fi
}

# Keeps the current remote while DNS still lists it, so a name with several A
# records does not flap the tunnel between them.
choose_remote() {
    local previous_remote="$1"
    local resolved_addresses="$2"
    local address
    local first_address=""

    while read -r address; do
        if [[ "$address" == "$previous_remote" ]]; then
            printf '%s\n' "$address"
            return 0
        fi
        if [[ -z "$first_address" ]]; then
            first_address="$address"
        fi
    done <<<"$resolved_addresses"
    printf '%s\n' "$first_address"
}

apply_tunnel_routing() {
    ip link set dev "$SIT6_TUNNEL_IFACE" up
    ip -6 address replace "$SIT6_TUNNEL_LINK_ADDRESS" dev "$SIT6_TUNNEL_IFACE"
    ip -6 route replace "$SIT6_TUNNEL_PREFIX" dev "$SIT6_TUNNEL_IFACE"
}

main() {
    local tunnel_exists=false
    local previous_remote=""
    local resolved_addresses=""
    local lookup_status=0
    local chosen_remote

    if [[ -e "/sys/class/net/$SIT6_TUNNEL_IFACE" ]]; then
        tunnel_exists=true
        previous_remote=$(current_remote)
    fi

    resolved_addresses=$(resolve_ipv4_addresses) || lookup_status=$?
    if [[ $lookup_status -ne 0 || -z "$resolved_addresses" ]]; then
        if [[ "$tunnel_exists" == true ]]; then
            echo "$LOG_PREFIX resolving the A record of $SIT6_TUNNEL_REMOTE_NAME failed (getent exit $lookup_status); keeping remote $previous_remote" >&2
            apply_tunnel_routing
            return 0
        fi
        echo "$LOG_PREFIX resolving the A record of $SIT6_TUNNEL_REMOTE_NAME failed (getent exit $lookup_status) and no tunnel exists yet, so there is no remote to keep" >&2
        return 1
    fi
    chosen_remote=$(choose_remote "$previous_remote" "$resolved_addresses")

    if [[ "$tunnel_exists" == false ]]; then
        ip tunnel add "$SIT6_TUNNEL_IFACE" mode sit \
            local "$SIT6_TUNNEL_LOCAL" remote "$chosen_remote" ttl "$SIT6_TUNNEL_TTL"
        echo "$LOG_PREFIX created $SIT6_TUNNEL_IFACE toward $chosen_remote ($SIT6_TUNNEL_REMOTE_NAME)"
    elif [[ "$chosen_remote" != "$previous_remote" ]]; then
        ip tunnel change "$SIT6_TUNNEL_IFACE" mode sit \
            local "$SIT6_TUNNEL_LOCAL" remote "$chosen_remote" ttl "$SIT6_TUNNEL_TTL"
        echo "$LOG_PREFIX remote changed from ${previous_remote:-none} to $chosen_remote ($SIT6_TUNNEL_REMOTE_NAME)"
    fi
    apply_tunnel_routing
}

main "$@"
