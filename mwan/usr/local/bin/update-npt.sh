#!/bin/sh
# Update nftables NPT (IPv6 prefix translation) rules
# Called by dhcpcd.exit-hook when prefix delegation changes

set -e

WAN_IFACE="$1"
DELEGATED_PREFIX="$2"

if [ -z "$WAN_IFACE" ] || [ -z "$DELEGATED_PREFIX" ]; then
    echo "Usage: $(basename "$0") <wan_iface> <delegated_prefix>"
    echo "Example: $(basename "$0") eth123 2600:1700:2f71:c80::/60"
    exit 1
fi

readonly INTERNAL_PREFIX="3d06:bad:b01::/56"

log() {
    logger -t update-npt "$1"
    echo "[update-npt] $1"
}

log "Updating NPT rules for $WAN_IFACE"
log "  Delegated prefix: $DELEGATED_PREFIX"

# Delete existing postrouting rules
for handle in $(nft -a list chain ip6 nat postrouting | grep "oif \"$WAN_IFACE\".*# handle" | sed -e 's/^.* handle //'); do
    nft delete rule ip6 nat postrouting handle "$handle"
done

# Delete existing prerouting rules
for handle in $(nft -a list chain ip6 nat prerouting | grep "iif \"$WAN_IFACE\".*# handle" | sed -e 's/^.* handle //'); do
    nft delete rule ip6 nat prerouting handle "$handle"
done

# Postrouting (outbound): NPT internal prefix to ISP prefix
nft add rule ip6 nat postrouting oif "$WAN_IFACE" ip6 saddr "$INTERNAL_PREFIX" snat ip6 prefix to "$DELEGATED_PREFIX"

# Prerouting (inbound): NPT ISP prefix back to internal prefix
nft add rule ip6 nat prerouting iif "$WAN_IFACE" ip6 daddr "$DELEGATED_PREFIX" dnat ip6 prefix to "$INTERNAL_PREFIX"

# Save current nftables config
nft list ruleset > /etc/nftables.conf.dynamic

log "NPT update complete for $WAN_IFACE"