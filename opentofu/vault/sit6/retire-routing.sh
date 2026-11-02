#!/usr/bin/env bash
set -euo pipefail

stop_legacy_unit() {
    local unit_name="$1"
    local load_state

    load_state=$(systemctl show --property=LoadState --value "$unit_name")
    if [[ "$load_state" == "not-found" ]]; then
        return
    fi
    systemctl disable --now "$unit_name"
}

process_owned_tables() {
    local action="$1"
    local table_listing
    local declaration
    local table_family
    local table_name

    table_listing=$(nft list tables)
    while read -r declaration table_family table_name; do
        if [[ -z "$declaration" ]]; then
            continue
        fi
        case "$table_family:$table_name" in
        inet:filter | ip:mangle | ip6:mangle) ;;
        *)
            printf 'Cannot stop nftables: table %s %s does not belong to sit6.\n' \
                "$table_family" "$table_name" >&2
            return 1
            ;;
        esac
        if [[ "$action" == "delete" ]]; then
            nft delete table "$table_family" "$table_name"
        fi
    done <<<"$table_listing"
}

main() {
    local old_source_address

    if [[ ! -f /etc/sysctl.d/99-client-network.conf ]]; then
        printf 'Install /etc/sysctl.d/99-client-network.conf before retiring sit6 routing.\n' >&2
        return 1
    fi

    # nftables.service flushes every table during stop.
    process_owned_tables check
    stop_legacy_unit sit6-tunnel-refresh.timer
    stop_legacy_unit sit6-tunnel-refresh.service
    stop_legacy_unit sit6-tunnel.service
    stop_legacy_unit frr.service
    stop_legacy_unit nftables.service
    process_owned_tables delete

    # The legacy tunnel service does not remove its device when stopped.
    if [[ -e /sys/class/net/sit6 ]]; then
        ip link delete dev sit6
    fi
    ip -6 route flush exact 3d06:bad:b01:b::/64
    ip -6 route flush default via 3d06:bad:b01:fe::3 dev eth0
    ip -4 route flush default via 10.250.250.3 dev eth0
    old_source_address=$(ip -6 address show dev lo to 3d06:bad:b01:b:ffff::5/128)
    if [[ -n "$old_source_address" ]]; then
        ip -6 address delete 3d06:bad:b01:b:ffff::5/128 dev lo
    fi

    rm -f /etc/systemd/system/sit6-tunnel.service \
        /etc/systemd/system/sit6-tunnel-refresh.service \
        /etc/systemd/system/sit6-tunnel-refresh.timer \
        /usr/local/sbin/sit6-tunnel-up \
        /etc/sit6/sit6.env \
        /etc/frr/frr.conf \
        /etc/frr/daemons \
        /etc/sysctl.d/99-sit6.conf \
        /etc/.pve-ignore.resolv.conf \
        /etc/nftables.conf
    systemctl daemon-reload
    sysctl -p /etc/sysctl.d/99-client-network.conf
}

main "$@"
