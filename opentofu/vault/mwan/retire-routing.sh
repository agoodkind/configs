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
        ip:nat | ip6:nat) ;;
        *)
            printf 'Cannot stop nftables: table %s %s does not belong to MWAN failover.\n' \
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
    if [[ ! -f /etc/sysctl.d/99-client-network.conf ]]; then
        printf 'Install /etc/sysctl.d/99-client-network.conf before retiring MWAN routing.\n' >&2
        return 1
    fi

    # nftables.service flushes every table during stop.
    process_owned_tables check
    stop_legacy_unit mwan-agent.service
    stop_legacy_unit mwan-ifmgr.service
    stop_legacy_unit mwan-failover-dhcp6.service
    stop_legacy_unit nftables.service

    if [[ -e /run/dhclient.eth0.pid ]]; then
        /sbin/dhclient -4 -r -pf /run/dhclient.eth0.pid \
            -lf /var/lib/dhcp/dhclient.eth0.leases eth0
    fi
    process_owned_tables delete

    # Proxmox assigns the replacement static IPv6 address to eth0.
    ip -4 address flush dev eth0 scope global
    ip -4 route flush dev eth0
    ip -6 address flush dev eth0 scope global dynamic
    ip -6 route flush dev eth0 proto ra
    ip -6 route flush default via fe80::f61e:57ff:fe06:4983 dev eth0

    if [[ -e /sys/class/net/eth1 ]]; then
        ip -4 address flush dev eth1 scope global
        ip -6 address flush dev eth1 scope global
        ip -4 route flush dev eth1
        ip -6 route flush dev eth1
        ip link set dev eth1 down
    fi

    rm -f /etc/systemd/system/mwan-agent.service \
        /etc/systemd/system/mwan-ifmgr.service \
        /etc/systemd/system/mwan-ifmgr.service.d/lxc-failover.conf \
        /etc/systemd/system/mwan-failover-dhcp6.service \
        /etc/mwan/config.toml \
        /etc/sysctl.d/99-mwan-failover.conf \
        /etc/network/if-up.d/mwan-failover \
        /etc/nftables.conf \
        /run/mwan-failover-dhcp6.pid \
        /var/lib/dhcp/mwan-failover-dhcp6.leases \
        /var/lib/dhcp/dhclient.eth0.leases
    systemctl daemon-reload
    sysctl -p /etc/sysctl.d/99-client-network.conf
    sysctl -w net.ipv6.conf.eth0.accept_ra=1
}

main "$@"
