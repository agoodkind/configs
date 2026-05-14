#!/bin/bash
# Deploy the full MWAN testbed on suburban.
# Run from the repo root on your local machine.
# Usage: ./testbed/deploy.sh
set -euo pipefail

SUBURBAN="root@10.240.0.148"
TESTBED_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$TESTBED_DIR/.." && pwd)"

# Deterministic link-locals (derived from PVE MAC addresses)
VM950_LL_WEBPASS="fe80::be24:11ff:febe:8eb4"
VM950_LL_ATT="fe80::be24:11ff:fec0:d760"
VM950_LL_MBRAINS="fe80::be24:11ff:fe3d:cecc"

echo "=== Phase 0: Build mwan binary ==="
MWAN_BIN="$REPO_ROOT/mwan/go/bin/mwan-linux"
if [ ! -f "$MWAN_BIN" ]; then
    echo "  Building mwan binary..."
    (cd "$REPO_ROOT/mwan/go" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 make build-linux)
fi

echo "=== Phase 1: Suburban host ==="
echo "  Deploying mwan binary to hypervisor..."
scp "$MWAN_BIN" "$SUBURBAN:/usr/local/bin/mwan"

echo "  Deploying hypervisor config (rendering secrets)..."
CREDS_FILE="$TESTBED_DIR/opnsense-101/.api-credentials"
if [ -f "$CREDS_FILE" ]; then
    API_KEY=$(grep '^key=' "$CREDS_FILE" | cut -d= -f2)
    API_SECRET=$(grep '^secret=' "$CREDS_FILE" | cut -d= -f2)
    sed -e "s/TESTBED_API_KEY/$API_KEY/g" -e "s/TESTBED_API_SECRET/$API_SECRET/g" \
        "$TESTBED_DIR/suburban-cutover2.toml" | ssh "$SUBURBAN" "mkdir -p /etc/mwan && cat > /etc/mwan/config.toml"
else
    echo "  WARNING: $CREDS_FILE not found. Deploying config with placeholder keys."
    scp "$TESTBED_DIR/suburban-cutover2.toml" "$SUBURBAN:/etc/mwan/config.toml"
fi

echo "  Deploying sysctl..."
scp "$TESTBED_DIR/suburban-sysctl.conf" "$SUBURBAN:/etc/sysctl.d/99-mwan-testbed.conf"
ssh "$SUBURBAN" "sysctl --system | tail -1"

echo "  Deploying masquerade rules (sourced file)..."
scp "$TESTBED_DIR/suburban-interfaces.d-testbed-masquerade.conf" "$SUBURBAN:/etc/network/interfaces.d/testbed-masquerade.conf"

echo ""
echo "=== Phase 2: VM 950 ==="
VM950="root@3d06:bad:b01:200::950"
VM950_SCP="root@[3d06:bad:b01:200::950]"
SSH_VM950="ssh -o StrictHostKeyChecking=accept-new $VM950"

echo "  Deploying mwan.env..."
$SSH_VM950 "mkdir -p /etc/mwan /opt/mwan/scripts"
scp "$TESTBED_DIR/vm-950/mwan.env" "$VM950_SCP:/etc/mwan/mwan.env"

echo "  Deploying nftables..."
scp "$TESTBED_DIR/vm-950/nftables.conf" "$VM950_SCP:/etc/nftables.conf"
$SSH_VM950 "systemctl enable nftables"

echo "  Deploying sysctl..."
scp "$TESTBED_DIR/vm-950/sysctl-mwan.conf" "$VM950_SCP:/etc/sysctl.d/99-mwan.conf"

echo "  Deploying production scripts..."
for script in update-routes.sh update-npt.sh mwan-update-npt-all.sh mwan-wait-routes-prereqs.sh mwan-wait-npt-prereqs.sh; do
    scp "$REPO_ROOT/mwan/scripts/$script" "$VM950_SCP:/opt/mwan/scripts/$script"
    $SSH_VM950 "ln -sf /opt/mwan/scripts/$script /usr/local/bin/$script"
done
$SSH_VM950 "chmod +x /opt/mwan/scripts/*.sh"

echo "  Deploying rt_tables..."
$SSH_VM950 "grep -q '^100 att' /etc/iproute2/rt_tables || cat >> /etc/iproute2/rt_tables << 'EOF'
100 att
200 webpass
300 monkeybrains
EOF"

echo "  Deploying systemd services..."
scp "$TESTBED_DIR/vm-950/mwan-update-routes.service" "$VM950_SCP:/etc/systemd/system/"
scp "$TESTBED_DIR/vm-950/mwan-update-npt.service" "$VM950_SCP:/etc/systemd/system/"
$SSH_VM950 "systemctl daemon-reload && systemctl enable mwan-update-routes mwan-update-npt nftables"

echo "  Installing jq (if needed)..."
$SSH_VM950 "which jq >/dev/null 2>&1 || apt-get install -y jq" | tail -1

echo "  Deploying mwan binary..."
MWAN_BIN="$REPO_ROOT/mwan/go/bin/mwan-linux"
if [ ! -f "$MWAN_BIN" ]; then
    echo "  ERROR: $MWAN_BIN not found. Run 'make build-linux' first."
    exit 1
fi
scp "$MWAN_BIN" "$VM950_SCP:/usr/local/bin/mwan"

echo "  Deploying mwan agent config..."
scp "$TESTBED_DIR/vm-950/config.toml" "$VM950_SCP:/etc/mwan/config.toml"

echo "  Deploying mwan-agent systemd service..."
scp "$REPO_ROOT/mwan/go/cmd/mwan/mwan-agent.service" "$VM950_SCP:/etc/systemd/system/mwan-agent.service"
$SSH_VM950 "systemctl daemon-reload && systemctl enable mwan-agent"

echo ""
echo "=== Phase 2b: LXC 100 (failover) ==="
ssh "$SUBURBAN" "pct exec 100 -- mkdir -p /etc/mwan"

echo "  Deploying mwan binary to LXC 100..."
scp "$MWAN_BIN" "$SUBURBAN:/tmp/lxc100-mwan"
ssh "$SUBURBAN" "pct push 100 /tmp/lxc100-mwan /usr/local/bin/mwan"
ssh "$SUBURBAN" "pct exec 100 -- chmod +x /usr/local/bin/mwan"

echo "  Deploying configs to LXC 100..."
scp "$TESTBED_DIR/lxc-100/config.toml" "$SUBURBAN:/tmp/lxc100-config.toml"
ssh "$SUBURBAN" "pct push 100 /tmp/lxc100-config.toml /etc/mwan/config.toml"
scp "$TESTBED_DIR/lxc-100/nftables.conf" "$SUBURBAN:/tmp/lxc100-nftables.conf"
ssh "$SUBURBAN" "pct push 100 /tmp/lxc100-nftables.conf /etc/nftables.conf"

echo "  Deploying mwan-agent service to LXC 100..."
scp "$REPO_ROOT/mwan/go/cmd/mwan/mwan-agent.service" "$SUBURBAN:/tmp/lxc100-mwan-agent.service"
ssh "$SUBURBAN" "pct push 100 /tmp/lxc100-mwan-agent.service /etc/systemd/system/mwan-agent.service"

echo "  Installing nftables (if needed)..."
ssh "$SUBURBAN" "pct exec 100 -- sh -c 'which nft >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq nftables)'"
ssh "$SUBURBAN" "pct exec 100 -- sh -c 'systemctl daemon-reload && systemctl enable nftables mwan-agent'"

echo ""
echo "=== Phase 3: ISP LXCs ==="

deploy_isp_lxc() {
    local ID=$1 NAME=$2 PD=$3 V4_SUBNET=$4 VM950_LL=$5

    echo "  --- LXC $ID ($NAME) ---"

    # sysctl
    scp "$TESTBED_DIR/isp-lxc/sysctl-isp.conf" "$SUBURBAN:/tmp/isp-sysctl-${ID}.conf"
    ssh "$SUBURBAN" "pct push $ID /tmp/isp-sysctl-${ID}.conf /etc/sysctl.d/99-isp.conf"

    # nftables (render template)
    local NFT_FILE="/tmp/isp-${ID}-nftables.conf"
    sed "s|__V4_SUBNET__|$V4_SUBNET|g" "$TESTBED_DIR/isp-lxc/nftables.conf.tmpl" > "$NFT_FILE"
    scp "$NFT_FILE" "$SUBURBAN:/tmp/isp-nft-${ID}.conf"
    ssh "$SUBURBAN" "pct push $ID /tmp/isp-nft-${ID}.conf /etc/nftables.conf"

    # PD route service (render template)
    local SVC_FILE="/tmp/isp-${ID}-pd-route.service"
    sed -e "s|__PD_PREFIX__|$PD|g" -e "s|__VM950_LL__|$VM950_LL|g" \
        "$TESTBED_DIR/isp-lxc/pd-route.service.tmpl" > "$SVC_FILE"
    scp "$SVC_FILE" "$SUBURBAN:/tmp/isp-pd-${ID}.service"
    ssh "$SUBURBAN" "pct push $ID /tmp/isp-pd-${ID}.service /etc/systemd/system/pd-route.service"

    # kea config
    local KEA_FILE="/tmp/isp-${ID}-kea.conf"
    cat > "$KEA_FILE" << KEAEOF
{
    "Dhcp6": {
        "interfaces-config": { "interfaces": ["eth0"] },
        "lease-database": { "type": "memfile", "persist": false },
        "preferred-lifetime": 3000,
        "valid-lifetime": 4000,
        "subnet6": [{
            "id": 1,
            "subnet": "fe80::/10",
            "interface": "eth0",
            "pd-pools": [{ "prefix": "${PD%%::*}::", "prefix-len": 60, "delegated-len": 60 }]
        }]
    }
}
KEAEOF
    scp "$KEA_FILE" "$SUBURBAN:/tmp/isp-kea-${ID}.conf"
    ssh "$SUBURBAN" "pct push $ID /tmp/isp-kea-${ID}.conf /etc/kea/kea-dhcp6.conf"

    # radvd config
    local RADVD_FILE="/tmp/isp-${ID}-radvd.conf"
    cat > "$RADVD_FILE" << RADVDEOF
interface eth0
{
    AdvSendAdvert on;
    MinRtrAdvInterval 30;
    MaxRtrAdvInterval 100;
    AdvManagedFlag on;
    AdvOtherConfigFlag on;
    prefix ${PD}
    {
        AdvOnLink off;
        AdvAutonomous off;
        AdvRouterAddr off;
    };
};
RADVDEOF
    scp "$RADVD_FILE" "$SUBURBAN:/tmp/isp-radvd-${ID}.conf"
    ssh "$SUBURBAN" "pct push $ID /tmp/isp-radvd-${ID}.conf /etc/radvd.conf"

    # Enable services
    ssh "$SUBURBAN" "pct exec $ID -- sh -c '
        sysctl --system >/dev/null 2>&1
        systemctl daemon-reload
        systemctl enable nftables pd-route kea-dhcp6-server radvd 2>/dev/null
        mkdir -p /var/run/kea /run/kea /var/lib/kea
        chmod 777 /var/run/kea /run/kea /var/lib/kea
    '"
    echo "  LXC $ID ($NAME) configured"
}

# Install kea + radvd on all ISP LXCs (they have internet via eth1)
for ID in 200 201 202; do
    ssh "$SUBURBAN" "pct exec $ID -- sh -c 'which kea-dhcp6 >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq kea-dhcp6-server radvd)'" | tail -1
done

deploy_isp_lxc 200 webpass "3d06:bad:b01:220::/60" "10.240.204.0/24" "$VM950_LL_WEBPASS"
deploy_isp_lxc 201 att     "3d06:bad:b01:230::/60" "10.240.205.0/24" "$VM950_LL_ATT"
deploy_isp_lxc 202 mbrains "3d06:bad:b01:240::/60" "10.240.206.0/24" "$VM950_LL_MBRAINS"

echo ""
echo "=== Phase 4: Reboot all and verify ==="
echo "  Rebooting ISP LXCs..."
ssh "$SUBURBAN" "pct reboot 200; pct reboot 201; pct reboot 202"
echo "  Waiting 15s..."
sleep 15

echo "  Restarting VM 950 services..."
$SSH_VM950 "sysctl --system >/dev/null 2>&1; systemctl restart nftables mwan-update-routes mwan-update-npt"

echo ""
echo "=== Phase 5: Verify ==="
echo "  VM 950 nftables:"
$SSH_VM950 "nft list tables"
echo "  VM 950 routes:"
$SSH_VM950 "ip -6 rule show | grep -v local | grep -v main | head -5"
echo "  ISP LXC services:"
for ID in 200 201 202; do
    ssh "$SUBURBAN" "pct exec $ID -- sh -c 'echo LXC${ID}: nft=\$(nft list tables | wc -l) kea=\$(pgrep -c kea-dhcp6 2>/dev/null || echo 0) radvd=\$(pgrep -c radvd 2>/dev/null || echo 0) route=\$(ip -6 route show | grep -c /60)'"
done

echo ""
echo "=== Deploy complete ==="
