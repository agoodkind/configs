#!/usr/bin/env bash
# Bring up the 6in4 tunnel to Berylax toward the IPv4 address sit6-set-remote
# last recorded. Every step replaces existing state, so a re-run converges.
# sit6-tunnel.service supplies the SIT6_* settings from /etc/sit6/sit6.env.
set -euo pipefail

main() {
    local remote_address

    if [[ ! -s "$SIT6_REMOTE_FILE" ]]; then
        echo "sit6-tunnel: no tunnel remote recorded in $SIT6_REMOTE_FILE; Berylax records it by connecting with the updater SSH key" >&2
        return 1
    fi
    remote_address=$(<"$SIT6_REMOTE_FILE")

    if [[ -e "/sys/class/net/$SIT6_TUNNEL_IFACE" ]]; then
        ip tunnel change "$SIT6_TUNNEL_IFACE" mode sit \
            local "$SIT6_TUNNEL_LOCAL" remote "$remote_address" ttl "$SIT6_TUNNEL_TTL"
    else
        ip tunnel add "$SIT6_TUNNEL_IFACE" mode sit \
            local "$SIT6_TUNNEL_LOCAL" remote "$remote_address" ttl "$SIT6_TUNNEL_TTL"
    fi
    ip link set dev "$SIT6_TUNNEL_IFACE" up
    ip -6 address replace "$SIT6_TUNNEL_LINK_ADDRESS" dev "$SIT6_TUNNEL_IFACE"
    ip -6 route replace "$SIT6_TUNNEL_PREFIX" dev "$SIT6_TUNNEL_IFACE"

    echo "sit6-tunnel: $SIT6_TUNNEL_IFACE up toward $remote_address, routing $SIT6_TUNNEL_PREFIX"
}

main "$@"
