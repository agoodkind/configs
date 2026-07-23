#!/usr/bin/env bash
# kea-rebind: restart the local kea DHCP servers when eth0 is recreated.
#
# kea binds raw packet sockets to eth0 by ifindex at startup. When the
# container's veth is recreated (host bridge rebuild, `pct set` net change, or
# kea racing an unsettled veth at boot) the socket points at a dead ifindex and
# kea stops answering DISCOVER without logging anything. A restart is the only
# way kea rebinds to the new ifindex. This watcher polls eth0's ifindex so it
# reacts to a genuine veth swap and ignores carrier flaps.
set -euo pipefail

readonly IFACE="eth0"
readonly IFINDEX_PATH="/sys/class/net/${IFACE}/ifindex"
readonly KEA_UNITS=(kea-dhcp4-server kea-dhcp6-server)
readonly POLL_INTERVAL=2

interrupted=0

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

rebind_kea() {
    # Restart only the kea units actually installed on this sim, so one watcher
    # serves v6-only sims and the dynamic-v4 sims that also run kea-dhcp4. A
    # restart failure is logged, not fatal, so a transient hiccup does not kill
    # the watcher.
    local unit
    local -a units=()
    for unit in "${KEA_UNITS[@]}"; do
        if systemctl cat "$unit" >/dev/null 2>&1; then
            units+=("$unit")
        fi
    done
    if [[ "${#units[@]}" -eq 0 ]]; then
        return 0
    fi
    local restart_rc=0
    systemctl try-restart "${units[@]}" || restart_rc=$?
    if [[ "$restart_rc" -ne 0 ]]; then
        echo "kea-rebind: kea try-restart failed (exit ${restart_rc})" >&2
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
        rebind_kea
    fi
done
