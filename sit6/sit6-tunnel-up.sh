#!/usr/bin/env bash
# Bring up the 6in4 tunnel to Berylax. Every step replaces existing state, so a
# re-run converges. sit6-tunnel.service supplies the SIT6_* settings.
set -euo pipefail

main() {
    if [[ -e "/sys/class/net/$SIT6_TUNNEL_IFACE" ]]; then
        ip tunnel change "$SIT6_TUNNEL_IFACE" mode sit \
            local "$SIT6_TUNNEL_LOCAL" remote "$SIT6_TUNNEL_REMOTE" ttl "$SIT6_TUNNEL_TTL"
    else
        ip tunnel add "$SIT6_TUNNEL_IFACE" mode sit \
            local "$SIT6_TUNNEL_LOCAL" remote "$SIT6_TUNNEL_REMOTE" ttl "$SIT6_TUNNEL_TTL"
    fi
    ip link set dev "$SIT6_TUNNEL_IFACE" up
    ip -6 address replace "$SIT6_TUNNEL_LINK_ADDRESS" dev "$SIT6_TUNNEL_IFACE"
    ip -6 route replace "$SIT6_TUNNEL_PREFIX" dev "$SIT6_TUNNEL_IFACE"

    echo "sit6-tunnel: $SIT6_TUNNEL_IFACE up toward $SIT6_TUNNEL_REMOTE, routing $SIT6_TUNNEL_PREFIX"
}

main "$@"
