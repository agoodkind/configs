#!/bin/sh
# Update nftables NPT (IPv6 prefix translation) rules
# Called by dhcpcd.exit-hook when prefix delegation changes

set -e

WAN_NAME="$1"
DELEGATED_PREFIX="$2"
WAN_ADDRESS="$3"

if [ -z "$WAN_NAME" ] || [ -z "$DELEGATED_PREFIX" ] || [ -z "$WAN_ADDRESS" ]; then
    echo "Usage: $0 <wan_name> <delegated_prefix> <wan_address>"
    echo "Example: $0 att 2600:1700:2f71:c80::/60 2600:1700:2f71:c8a::1/64"
    exit 1
fi

INTERNAL_PREFIX="3d06:bad:b01::/56"

log() {
    logger -t update-npt "$1"
    echo "[update-npt] $1"
}

log "Updating NPT rules for $WAN_NAME"
log "  Delegated prefix: $DELEGATED_PREFIX"
log "  WAN address: $WAN_ADDRESS"

# Determine interface based on WAN name
case "$WAN_NAME" in
    att)
        WAN_IFACE="$(ip -6 addr show | grep -B2 "$WAN_ADDRESS" | grep -o 'eth[0-9].*[0-9]' | head -1)"
        ;;
    webpass)
        WAN_IFACE="$(ip -6 addr show | grep -B2 "$WAN_ADDRESS" | grep -o 'eth[0-9]' | head -1)"
        ;;
    monkeybrains)
        WAN_IFACE="eth3"
        ;;
    *)
        log "Unknown WAN name: $WAN_NAME"
        exit 1
        ;;
esac

if [ -z "$WAN_IFACE" ]; then
    log "ERROR: Could not determine interface for $WAN_NAME"
    exit 1
fi

log "  Interface: $WAN_IFACE"

# Extract just the address part (without /64)
WAN_ADDR_ONLY="${WAN_ADDRESS%/*}"

# Create temporary nft script to update rules
NFT_SCRIPT="/tmp/update-npt-${WAN_NAME}.nft"

cat > "$NFT_SCRIPT" << EOF
# Update NPT rules for $WAN_NAME
# Interface: $WAN_IFACE
# Delegated: $DELEGATED_PREFIX
# WAN Addr: $WAN_ADDR_ONLY

# Delete existing rules for this WAN (if any)
delete rule ip6 nat prerouting iif $WAN_IFACE 2>/dev/null || true
delete rule ip6 nat postrouting oif $WAN_IFACE 2>/dev/null || true

# Add new NPT rules

# Postrouting (outbound): Don't NPT the container's own WAN address (1:1 fix)
add rule ip6 nat postrouting oif $WAN_IFACE ip6 saddr $WAN_ADDR_ONLY/128 accept

# Postrouting (outbound): NPT internal prefix to ISP prefix
add rule ip6 nat postrouting oif $WAN_IFACE ip6 saddr $INTERNAL_PREFIX snat ip6 prefix to $DELEGATED_PREFIX

# Prerouting (inbound): NPT ISP prefix back to internal prefix
add rule ip6 nat prerouting iif $WAN_IFACE ip6 daddr $DELEGATED_PREFIX dnat ip6 prefix to $INTERNAL_PREFIX
EOF

# Apply the rules
if nft -f "$NFT_SCRIPT"; then
    log "NPT rules updated successfully for $WAN_NAME"
else
    log "ERROR: Failed to update NPT rules for $WAN_NAME"
    cat "$NFT_SCRIPT"
    exit 1
fi

rm -f "$NFT_SCRIPT"

# Save current nftables config
nft list ruleset > /etc/nftables.conf.dynamic

log "NPT update complete for $WAN_NAME"

