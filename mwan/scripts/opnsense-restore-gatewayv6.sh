#!/usr/bin/env bash
# Restore <gatewayv6> to OPNsense config.xml from the pre-BGP backup.
#
# Called by: cutover2 unfuck (via SSH to OPNsense)
# Prerequisite: opnsense-remove-gatewayv6.sh created the backup

set -euo pipefail

CONFIG="/conf/config.xml"
BACKUP="${CONFIG}.pre-bgp"

if [[ -f "$BACKUP" ]]; then
    cp "$BACKUP" "$CONFIG"
    echo "restored"
else
    echo "no-backup-found"
    exit 1
fi
