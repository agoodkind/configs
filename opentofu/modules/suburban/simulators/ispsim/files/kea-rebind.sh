#!/usr/bin/env bash
# kea-rebind: re-establish the eth0-bound sim services when eth0 is recreated.
#
# Two services bind to eth0 and break silently when the container's veth is
# recreated (host bridge rebuild, `pct set` net change, or a boot race):
#   - kea binds raw packet sockets to eth0 by ifindex at startup, so after a
#     recreate the socket points at a dead ifindex and kea stops answering
#     DISCOVER with nothing logged.
#   - pd-route installs the delegated-prefix and static /29 routes on eth0 as a
#     oneshot with RemainAfterExit, so a recreate flushes those routes and the
#     unit never re-runs on its own.
# A restart fixes both. This watcher polls eth0's ifindex, so it reacts to a
# genuine veth swap and ignores carrier flaps.
#
# kea-dhcp4 and kea-dhcp6 can also start before eth0 comes up, log
# DHCPSRV_NO_SOCKETS_OPEN, and stay active without a UDP socket on eth0. Kea
# retries the socket open on its own. The watcher restarts a Kea unit when eth0
# is up and the unit's socket stays missing for MISSING_SOCKET_POLLS polls,
# which is longer than the Kea retry wait.
set -euo pipefail

readonly IFACE="eth0"
readonly IFINDEX_PATH="/sys/class/net/${IFACE}/ifindex"
readonly OPERSTATE_PATH="/sys/class/net/${IFACE}/operstate"
readonly KEA_UNITS=(kea-dhcp4-server kea-dhcp6-server)
readonly DHCP4_UNIT="kea-dhcp4-server"
readonly DHCP4_PORT=67
readonly DHCP6_UNIT="kea-dhcp6-server"
readonly DHCP6_PORT=547
readonly MISSING_SOCKET_POLLS=15
readonly ROUTE_UNIT="pd-route"
readonly POLL_INTERVAL=2
# Socket probe results.
readonly SOCKET_OPEN=0
readonly SOCKET_MISSING=1
readonly SOCKET_UNKNOWN=2

interrupted=0
declare -A missing_socket_polls=()

on_signal() {
    interrupted=1
    exit 130
}
trap on_signal INT TERM

read_ifindex() {
    if [[ -r "$IFINDEX_PATH" ]]; then
        cat "$IFINDEX_PATH"
    else
        printf ''
    fi
}

restart_unit() {
    # $1 = unit, $2 = systemctl verb (try-restart or restart). Skips units not
    # installed on this sim, and logs a failure without killing the watcher.
    local unit="$1"
    local verb="$2"
    if ! systemctl cat "$unit" >/dev/null 2>&1; then
        return 0
    fi
    local rc=0
    systemctl "$verb" "$unit" || rc=$?
    if [[ "$rc" -ne 0 ]]; then
        echo "kea-rebind: ${verb} ${unit} failed (exit ${rc})" >&2
    fi
}

rebind_services() {
    local unit
    # kea: try-restart only touches a running unit, which is the stale-socket
    # case (kea stays active with a dead ifindex). One watcher serves the
    # v6-only sim and the dynamic-v4 sims that also run kea-dhcp4.
    for unit in "${KEA_UNITS[@]}"; do
        restart_unit "$unit" try-restart
    done
    # pd-route: RemainAfterExit leaves it "active (exited)", so restart re-runs
    # its ExecStart and re-adds the routes the veth recreate flushed.
    restart_unit "$ROUTE_UNIT" restart
}

list_udp_sockets() {
    # $1 = port. Prints the listening UDP sockets on the port.
    local port="$1"
    if ! ss -H -u -l -n "sport = :${port}"; then
        echo "kea-rebind: ss could not list the UDP ${port} sockets" >&2
        return 1
    fi
}

# kea-dhcp6 binds its UDP 547 sockets to eth0, and ss prints them with %eth0.
probe_dhcp6_socket() {
    local sockets
    if ! sockets="$(list_udp_sockets "$DHCP6_PORT")"; then
        return "$SOCKET_UNKNOWN"
    fi
    if [[ "$sockets" == *"%${IFACE}:${DHCP6_PORT}"* ]]; then
        return "$SOCKET_OPEN"
    fi
    return "$SOCKET_MISSING"
}

# kea-dhcp4 binds its UDP 67 socket to an eth0 IPv4 address, and ss prints the
# socket without a device suffix. The probe matches each eth0 IPv4 address.
probe_dhcp4_socket() {
    local sockets
    local addresses
    local address
    if ! sockets="$(list_udp_sockets "$DHCP4_PORT")"; then
        return "$SOCKET_UNKNOWN"
    fi
    if ! addresses="$(ip -4 -o addr show dev "$IFACE")"; then
        echo "kea-rebind: ip could not list the IPv4 addresses on ${IFACE}" >&2
        return "$SOCKET_UNKNOWN"
    fi
    while read -r _ _ _ address _; do
        address="${address%/*}"
        if [[ -n "$address" && " ${sockets} " == *[[:space:]]"${address}:${DHCP4_PORT}"[[:space:]]* ]]; then
            return "$SOCKET_OPEN"
        fi
    done <<<"$addresses"
    return "$SOCKET_MISSING"
}

check_kea_socket() {
    # $1 = unit, $2 = probe function, $3 = port for the log line.
    local unit="$1"
    local probe="$2"
    local port="$3"
    local operstate=""
    local result=0
    if ! systemctl is-active --quiet "$unit"; then
        missing_socket_polls["$unit"]=0
        return 0
    fi
    if [[ -r "$OPERSTATE_PATH" ]]; then
        operstate="$(cat "$OPERSTATE_PATH")"
    fi
    if [[ "$operstate" != "up" ]]; then
        missing_socket_polls["$unit"]=0
        return 0
    fi
    "$probe" || result=$?
    if [[ "$result" -eq "$SOCKET_UNKNOWN" ]]; then
        return 0
    fi
    if [[ "$result" -eq "$SOCKET_OPEN" ]]; then
        missing_socket_polls["$unit"]=0
        return 0
    fi
    missing_socket_polls["$unit"]=$((${missing_socket_polls["$unit"]:-0} + 1))
    if [[ "${missing_socket_polls["$unit"]}" -ge "$MISSING_SOCKET_POLLS" ]]; then
        echo "kea-rebind: ${unit} has no UDP ${port} socket on ${IFACE}; restarting it" >&2
        missing_socket_polls["$unit"]=0
        restart_unit "$unit" try-restart
    fi
}

last_ifindex="$(read_ifindex)"

while true; do
    if [[ "$interrupted" -eq 1 ]]; then
        break
    fi
    sleep "$POLL_INTERVAL"
    current_ifindex="$(read_ifindex)"
    if [[ -n "$current_ifindex" && "$current_ifindex" != "$last_ifindex" ]]; then
        last_ifindex="$current_ifindex"
        rebind_services
    fi
    check_kea_socket "$DHCP4_UNIT" probe_dhcp4_socket "$DHCP4_PORT"
    check_kea_socket "$DHCP6_UNIT" probe_dhcp6_socket "$DHCP6_PORT"
done
