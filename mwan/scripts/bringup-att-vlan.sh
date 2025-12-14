#!/usr/bin/env bash
# Post-authentication actions for AT&T VLAN interface
# For systemd-networkd - VLAN is created declaratively via .netdev file
# AT&T requires 802.1X authentication on parent before DHCP on VLAN works
# This script waits for authentication, then triggers DHCP on VLAN interface
#
# What it does (symptoms):
# - Fixes the “AT&T never gets DHCP/PD after reboot” class of issues by ensuring DHCP is only triggered
#   after 802.1X authentication is actually complete.
#
# What it does (technical):
# - Polls `wpa_cli status` until the supplicant reaches AUTHENTICATED.
# - Waits for the VLAN netdevice to exist, brings it UP if needed.
# - Triggers `networkctl renew/reconfigure` on the VLAN so systemd-networkd runs DHCP on it.
#
# Dependency graph:
# - Triggered by: `wpa-authenticated.path` → `wpa-authenticated.service` → `bringup-att-vlan.service`.
# - Depends on: `wpa_supplicant-mwan.service` running and systemd-networkd managing the VLAN netdev.

set -euo pipefail

# shellcheck disable=SC1091
. /etc/mwan/mwan.env

ATT_IFACE="${MWAN_ATT_IFACE:-}"
VLAN_IFACE="${MWAN_ATT_VLAN_IFACE:-}"
WPA_CLI="/sbin/wpa_cli"
MAX_WAIT=60
CHECK_INTERVAL=2
TRACE_FILE="${MWAN_TRACE_FILE:-/run/mwan-trace-id}"
MWAN_TRACE_ID="${MWAN_TRACE_ID:-}"
if [ -z "${MWAN_TRACE_ID:-}" ] && [ -r "$TRACE_FILE" ]; then
    MWAN_TRACE_ID="$(cat "$TRACE_FILE")"
fi

[ -n "$ATT_IFACE" ] || { 
	echo "Missing MWAN_ATT_IFACE in /etc/mwan/mwan.env" >&2;
	exit 1; 
}
[ -n "$VLAN_IFACE" ] || { 
	echo "Missing MWAN_ATT_VLAN_IFACE in /etc/mwan/mwan.env" >&2;
	exit 1;
}

log() {
    local prefix=""
    [ -n "${MWAN_TRACE_ID:-}" ] && prefix="traceId=${MWAN_TRACE_ID} "
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ${prefix}$*" | \
        systemd-cat -t bringup-att-vlan
}

log "Starting AT&T VLAN post-authentication script"

# Wait for 802.1X authentication via wpa_supplicant control socket
log "Waiting for 802.1X authentication on ${ATT_IFACE}"
elapsed=0
authenticated=false
while [ $elapsed -lt $MAX_WAIT ]; do
    # Check authentication state via wpa_cli
    if $WPA_CLI -i "${ATT_IFACE}" status 2>/dev/null | \
       grep -q "Supplicant PAE state=AUTHENTICATED"; then
        log "802.1X authentication successful"
        authenticated=true
        break
    fi
    sleep $CHECK_INTERVAL
    elapsed=$((elapsed + CHECK_INTERVAL))
done

if [ "$authenticated" != "true" ]; then
    log "ERROR: 802.1X authentication not completed after ${MAX_WAIT}s"
    exit 1
fi

log "Authentication successful, waiting for VLAN interface"

# Wait for VLAN interface to be created by systemd-networkd
elapsed=0
while [ $elapsed -lt $MAX_WAIT ]; do
    if ip link show "${VLAN_IFACE}" >/dev/null 2>&1; then
        log "VLAN interface ${VLAN_IFACE} detected"
        break
    fi
    sleep $CHECK_INTERVAL
    elapsed=$((elapsed + CHECK_INTERVAL))
done

if ! ip link show "${VLAN_IFACE}" >/dev/null 2>&1; then
    log "ERROR: VLAN interface not found after ${MAX_WAIT}s"
    exit 1
fi

# Ensure VLAN interface is up
if ! ip link show "${VLAN_IFACE}" | grep -q "state UP"; then
    log "Bringing up VLAN interface"
    ip link set "${VLAN_IFACE}" up || {
        log "ERROR: Failed to bring up VLAN interface"
        exit 1
    }
fi

# Trigger systemd-networkd to start DHCP on VLAN interface
# AT&T won't respond to DHCP until parent interface is authenticated
log "Triggering systemd-networkd DHCP on ${VLAN_IFACE}"
networkctl renew "${VLAN_IFACE}" || {
    log "WARNING: networkctl renew failed, trying reconfigure"
    networkctl reconfigure "${VLAN_IFACE}" || {
        log "ERROR: Failed to trigger DHCP on VLAN interface"
        exit 1
    }
}

log "VLAN interface ${VLAN_IFACE} is ready and DHCP triggered"
log "systemd-networkd will now obtain IP address from AT&T"

exit 0

