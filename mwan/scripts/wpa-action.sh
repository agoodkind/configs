#!/usr/bin/env bash
# wpa_supplicant action script - writes auth status file
# wpa_cli -a passes "CONNECTED" or "DISCONNECTED" as $2
#
# What it does (symptoms):
# - Provides a simple “auth complete” signal so AT&T VLAN DHCP can be triggered only after 802.1X succeeds.
#
# What it does (technical):
# - On CONNECTED: writes a journal line and `touch`es `/run/wpa_supplicant-mwan.authenticated`.
# - On DISCONNECTED: logs and removes that file.
#
# Dependency graph:
# - Called by: `wpa-cli-action.service` (runs `wpa_cli -a /usr/local/bin/wpa-action.sh`).
# - Triggers: `wpa-authenticated.path` which starts the AT&T VLAN bringup service/script.

AUTH_FILE="/run/wpa_supplicant-mwan.authenticated"
TRACE_FILE="${MWAN_TRACE_FILE:-/run/mwan-trace-id}"
MWAN_TRACE_ID="${MWAN_TRACE_ID:-}"
if [ -z "${MWAN_TRACE_ID:-}" ] && [ -r "$TRACE_FILE" ]; then
    MWAN_TRACE_ID="$(cat "$TRACE_FILE")"
fi

log() {
    local msg="$1"
    local prefix=""
    [ -n "${MWAN_TRACE_ID:-}" ] && prefix="traceId=${MWAN_TRACE_ID} "
    echo "$(date '+%Y-%m-%d %H:%M:%S') ${prefix}${msg}" | systemd-cat -t wpa-action
}

case "$2" in
    CONNECTED)
        log "CONNECTED"
        touch "$AUTH_FILE"
        ;;
    DISCONNECTED)
        log "DISCONNECTED"
        rm -f "$AUTH_FILE"
        ;;
esac

