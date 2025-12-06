#!/bin/sh
# Update policy routing tables for multi-WAN
# Called by dhcpcd.exit-hook and health-check.sh

set -e

log() {
    logger -t update-routes "$1"
    echo "[update-routes] $1"
}

log "Updating policy routing tables"

# Get WAN interface names and gateways
# Note: Interface detection is heuristic - update if your setup differs
MGMT_IFACE="eth0"  # Management interface (typically first, on vmbr0)
ATT_IFACE="eth1"  # AT&T (from attauth VM via "att" bridge)
WEBPASS_IFACE="eth2"  # Webpass
INTERNAL_IFACE="eth3"  # To OPNsense

# Get IPv4 gateways
if [ -n "$ATT_IFACE" ]; then
    ATT_GW4="$(ip -4 route show dev "$ATT_IFACE" | grep default | awk '{print $3}' | head -1)"
fi

if [ -n "$WEBPASS_IFACE" ]; then
    WEBPASS_GW4="$(ip -4 route show dev "$WEBPASS_IFACE" | grep default | awk '{print $3}' | head -1)"
fi

# Get IPv6 gateways
if [ -n "$ATT_IFACE" ]; then
    ATT_GW6="$(ip -6 route show dev "$ATT_IFACE" | grep default | awk '{print $3}' | head -1)"
fi

if [ -n "$WEBPASS_IFACE" ]; then
    WEBPASS_GW6="$(ip -6 route show dev "$WEBPASS_IFACE" | grep default | awk '{print $3}' | head -1)"
fi

log "AT&T: $ATT_IFACE (v6: $ATT_GW6, v4: $ATT_GW4)"
log "Webpass: $WEBPASS_IFACE (v6: $WEBPASS_GW6, v4: $WEBPASS_GW4)"

# Clear existing policy routing rules (except main table)
ip rule del table att 2>/dev/null || true
ip rule del table webpass 2>/dev/null || true
ip rule del table monkeybrains 2>/dev/null || true
ip -6 rule del table att 2>/dev/null || true
ip -6 rule del table webpass 2>/dev/null || true
ip -6 rule del table monkeybrains 2>/dev/null || true

# Flush routing tables
ip route flush table att 2>/dev/null || true
ip route flush table webpass 2>/dev/null || true
ip route flush table monkeybrains 2>/dev/null || true
ip -6 route flush table att 2>/dev/null || true
ip -6 route flush table webpass 2>/dev/null || true
ip -6 route flush table monkeybrains 2>/dev/null || true

# Set up AT&T routing table (table 100) - IPv6 FIRST (P0 priority)
if [ -n "$ATT_IFACE" ] && [ -n "$ATT_GW6" ]; then
    log "Setting up AT&T IPv6 routing table (priority)"
    ip -6 route add default via "$ATT_GW6" dev "$ATT_IFACE" table att
    ip -6 rule add fwmark 1 table att priority 100
fi

if [ -n "$ATT_IFACE" ] && [ -n "$ATT_GW4" ]; then
    log "Setting up AT&T IPv4 routing table"
    ip route add default via "$ATT_GW4" dev "$ATT_IFACE" table att
    ip rule add fwmark 1 table att priority 100
fi

# Set up Webpass routing table (table 200) - IPv6 FIRST
if [ -n "$WEBPASS_IFACE" ] && [ -n "$WEBPASS_GW6" ]; then
    log "Setting up Webpass IPv6 routing table (priority)"
    ip -6 route add default via "$WEBPASS_GW6" dev "$WEBPASS_IFACE" table webpass
    ip -6 rule add fwmark 2 table webpass priority 200
fi

if [ -n "$WEBPASS_IFACE" ] && [ -n "$WEBPASS_GW4" ]; then
    log "Setting up Webpass IPv4 routing table"
    ip route add default via "$WEBPASS_GW4" dev "$WEBPASS_IFACE" table webpass
    ip rule add fwmark 2 table webpass priority 200
fi

# Set main table default route - IPv6 FIRST (P0 priority), prefer AT&T
if [ -n "$ATT_GW6" ]; then
    log "Setting main table IPv6 default to AT&T (priority)"
    ip -6 route replace default via "$ATT_GW6" dev "$ATT_IFACE" metric 10
elif [ -n "$WEBPASS_GW6" ]; then
    log "AT&T IPv6 down, using Webpass"
    ip -6 route replace default via "$WEBPASS_GW6" dev "$WEBPASS_IFACE" metric 10
fi

if [ -n "$ATT_GW4" ]; then
    log "Setting main table IPv4 default to AT&T"
    ip route replace default via "$ATT_GW4" dev "$ATT_IFACE" metric 10
elif [ -n "$WEBPASS_GW4" ]; then
    log "AT&T IPv4 down, using Webpass"
    ip route replace default via "$WEBPASS_GW4" dev "$WEBPASS_IFACE" metric 10
fi

# Add local network routes to all tables (so WANs can reach internal subnet)
for table in att webpass; do
    ip route add 10.250.250.0/29 dev "$INTERNAL_IFACE" table $table 2>/dev/null || true
    ip -6 route add 3d06:bad:b01:fe::/64 dev "$INTERNAL_IFACE" table $table 2>/dev/null || true
done

# Management interface should use main routing table only (not policy routed)
# This ensures SSH access works regardless of WAN state

log "Policy routing update complete"

