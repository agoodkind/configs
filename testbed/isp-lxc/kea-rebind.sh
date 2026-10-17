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
# kea-dhcp6 can also start before eth0 comes up, log DHCPSRV_NO_SOCKETS_OPEN,
# and stay active without a UDP 547 socket. Kea retries the socket open on its
# own. The watcher restarts kea-dhcp6 when eth0 is up and the socket stays
# missing for MISSING_SOCKET_POLLS polls, which is longer than the Kea retry
# wait.
set -euo pipefail

readonly IFACE="eth0"
readonly IFINDEX_PATH="/sys/class/net/${IFACE}/ifindex"
readonly OPERSTATE_PATH="/sys/class/net/${IFACE}/operstate"
readonly KEA_UNITS=(kea-dhcp4-server kea-dhcp6-server)
readonly DHCP6_UNIT="kea-dhcp6-server"
readonly DHCP6_PORT=547
readonly MISSING_SOCKET_POLLS=15
readonly ROUTE_UNIT="pd-route"
readonly POLL_INTERVAL=2

interrupted=0
missing_socket_polls=0

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

check_dhcp6_socket() {
    local operstate=""
    local sockets
    if ! systemctl is-active --quiet "$DHCP6_UNIT"; then
        missing_socket_polls=0
        return 0
    fi
    if [[ -r "$OPERSTATE_PATH" ]]; then
        operstate="$(cat "$OPERSTATE_PATH")"
    fi
    if [[ "$operstate" != "up" ]]; then
        missing_socket_polls=0
        return 0
    fi
    if ! sockets="$(ss -H -u -l -n "sport = :${DHCP6_PORT}")"; then
        echo "kea-rebind: ss could not list the UDP ${DHCP6_PORT} sockets" >&2
        return 0
    fi
    if [[ "$sockets" == *"%${IFACE}:${DHCP6_PORT}"* ]]; then
        missing_socket_polls=0
        return 0
    fi
    missing_socket_polls=$((missing_socket_polls + 1))
    if [[ "$missing_socket_polls" -ge "$MISSING_SOCKET_POLLS" ]]; then
        echo "kea-rebind: ${DHCP6_UNIT} has no UDP ${DHCP6_PORT} socket on ${IFACE}; restarting it" >&2
        missing_socket_polls=0
        restart_unit "$DHCP6_UNIT" try-restart
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
    check_dhcp6_socket
done
