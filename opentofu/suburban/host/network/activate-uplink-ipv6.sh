#!/usr/bin/env bash
set -euo pipefail

UPLINK_INTERFACE=vmbr0
ROUTE_PROBE_ADDRESS=2606:4700:4700::1111

sysctl --load=/etc/sysctl.d/99-suburban-uplink-ipv6.conf
rdisc6 -1 "$UPLINK_INTERFACE"
ip -6 route get "$ROUTE_PROBE_ADDRESS" dev "$UPLINK_INTERFACE"
