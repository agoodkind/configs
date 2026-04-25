#!/usr/bin/env bash
# Remove <gatewayv6> from the WAN interface in OPNsense config.xml.
# Prevents system_routing_configure from reinstalling the IPv6 static
# route during FRR stop+start (force_down only prevents IPv4).
#
# Called by: cutover2 switch-to-bgp (via SSH to OPNsense)
# Revert: opnsense-restore-gatewayv6.sh

set -euo pipefail

CONFIG="/conf/config.xml"
BACKUP="${CONFIG}.pre-bgp"

cp "$CONFIG" "$BACKUP"

if command -v yq >/dev/null 2>&1; then
    yq -p xml -o xml 'del(.opnsense.interfaces.wan.gatewayv6)' "$CONFIG" > "${CONFIG}.tmp"
    mv "${CONFIG}.tmp" "$CONFIG"
    echo "yq-done"
else
    sed -i '' '/<gatewayv6>.*<\/gatewayv6>/d' "$CONFIG"
    echo "sed-done"
fi
