#!/usr/bin/env bash
set -euo pipefail

# Register the same IPv6 address and interface that Linux uses to contact the server.
route=$(ip -j -6 route get "${1:?supply the k3s server IPv6 address}")
node_ip=$(jq -er '.[0].prefsrc' <<< "$route")
interface=$(jq -er '.[0].dev' <<< "$route")
exec /usr/local/bin/k3s agent --node-ip "$node_ip" --flannel-iface "$interface"
