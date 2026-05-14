#!/bin/sh
# Configure an ISP LXC as a simulated ISP gateway for MWAN testbed.
# Run inside the LXC: sh /tmp/isp-lxc-setup.sh <isp-name> <pd-prefix> <v4-subnet>
#
# eth0 = simulated ISP link (facing VM 950)
# eth1 = real uplink (Comcast/vmbr0)
set -eu

ISP_NAME="${1:?Usage: $0 <isp-name> <pd-prefix/60> <v4-subnet/24>}"
PD_PREFIX="${2:?}"
V4_SUBNET="${3:?}"

echo "=== Configuring ISP LXC: ${ISP_NAME} ==="

# 1. Forwarding + sysctl
sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1
sysctl -w net.ipv6.conf.eth0.accept_ra=0
sysctl -w net.ipv6.conf.eth1.accept_ra=2
cat > /etc/sysctl.d/99-isp.conf << EOF
net.ipv4.ip_forward=1
net.ipv6.conf.all.forwarding=1
net.ipv6.conf.eth0.accept_ra=0
net.ipv6.conf.eth1.accept_ra=2
EOF

# 2. Masquerade (nft)
nft -f - << NFT
flush ruleset
table ip nat {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        oifname "eth1" ip saddr ${V4_SUBNET} masquerade
    }
}
table ip6 nat {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        oifname "eth1" masquerade
    }
}
table inet filter {
    chain forward {
        type filter hook forward priority filter; policy accept;
    }
}
NFT
echo "  Masquerade: ${V4_SUBNET} + IPv6 -> eth1"

# Persist nftables
nft list ruleset > /etc/nftables.conf
systemctl enable nftables 2>/dev/null || true

# 3. Route PD /60 to VM 950 via link-local on eth0
VM950_LL=$(ip -6 neigh show dev eth0 | grep -v FAILED | head -1 | awk '{print $1}')
if [ -z "$VM950_LL" ]; then
    ping6 -c1 -W2 ff02::1%eth0 >/dev/null 2>&1 || true
    sleep 1
    VM950_LL=$(ip -6 neigh show dev eth0 | grep -v FAILED | head -1 | awk '{print $1}')
fi
if [ -n "$VM950_LL" ]; then
    ip -6 route replace ${PD_PREFIX} via ${VM950_LL} dev eth0
    echo "  Route: ${PD_PREFIX} via ${VM950_LL} dev eth0"
    # Persist route
    mkdir -p /etc/network/if-up.d
    cat > /etc/network/if-up.d/isp-route << IFUP
#!/bin/sh
sleep 3
VM950_LL=\$(ip -6 neigh show dev eth0 | grep -v FAILED | head -1 | awk '{print \$1}')
[ -z "\$VM950_LL" ] && ping6 -c1 -W2 ff02::1%eth0 >/dev/null 2>&1 && sleep 1 && VM950_LL=\$(ip -6 neigh show dev eth0 | grep -v FAILED | head -1 | awk '{print \$1}')
[ -n "\$VM950_LL" ] && ip -6 route replace ${PD_PREFIX} via \$VM950_LL dev eth0
IFUP
    chmod +x /etc/network/if-up.d/isp-route
else
    echo "  ERROR: Could not discover VM 950 link-local on eth0"
fi

echo "=== ${ISP_NAME} done ==="
