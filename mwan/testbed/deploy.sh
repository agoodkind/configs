#!/bin/bash
# Deploy the full MWAN testbed on suburban.
# Run from the repo root on your local machine.
# Usage: ./mwan/testbed/deploy.sh
set -euo pipefail

SUBURBAN="root@10.240.0.148"
TESTBED_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$TESTBED_DIR/../.." && pwd)"

echo "=== Phase 1: Suburban host infrastructure ==="

echo "  Deploying sysctl..."
scp "$TESTBED_DIR/suburban-sysctl.conf" "$SUBURBAN:/etc/sysctl.d/99-mwan-testbed.conf"
ssh "$SUBURBAN" "sysctl --system >/dev/null 2>&1"

echo "  Deploying bridge config..."
# Check if bridges already exist
if ssh "$SUBURBAN" "ip link show vmbr4 >/dev/null 2>&1"; then
    echo "  Bridges vmbr4/5/6 already exist, skipping"
else
    echo "  Appending bridge config to /etc/network/interfaces..."
    scp "$TESTBED_DIR/suburban-interfaces.conf" "$SUBURBAN:/tmp/testbed-bridges.conf"
    ssh "$SUBURBAN" "cat /tmp/testbed-bridges.conf >> /etc/network/interfaces && ifreload -a"
    echo "  Bridges created"
fi

echo ""
echo "=== Phase 2: Create VM 950 ==="
scp "$TESTBED_DIR/vm-950/create.sh" "$SUBURBAN:/tmp/create-vm950.sh"
ssh "$SUBURBAN" "chmod +x /tmp/create-vm950.sh && /tmp/create-vm950.sh"

echo "  Starting VM 950..."
ssh "$SUBURBAN" "qm start 950"
echo "  Waiting 30s for boot..."
sleep 30

echo "  Detecting MACs and updating link files..."
MACS=$(ssh "$SUBURBAN" "qm config 950 | grep '^net' | sort")
echo "$MACS"
# TODO: Parse MACs from qm config output and fill in .link files
# For now, deploy the configs and user fills MACs manually

echo ""
echo "  Deploying systemd-networkd configs to VM 950..."
VM950="root@3d06:bad:b01:200::950"
ssh -o StrictHostKeyChecking=no "$VM950" "mkdir -p /etc/systemd/network /etc/mwan"
for f in "$TESTBED_DIR"/vm-950/*.link "$TESTBED_DIR"/vm-950/*.network; do
    scp -o StrictHostKeyChecking=no "$f" "$VM950:/etc/systemd/network/"
done
scp -o StrictHostKeyChecking=no "$TESTBED_DIR/vm-950/mwan.env" "$VM950:/etc/mwan/mwan.env"

echo ""
echo "  Deploying mwan monolith to VM 950..."
MWAN_BIN="$REPO_ROOT/mwan/go/mwan"
if [[ ! -f "$MWAN_BIN" ]]; then
    echo "  Building mwan monolith..."
    (cd "$REPO_ROOT/mwan/go" && GOOS=linux GOARCH=amd64 go build -o mwan ./cmd/mwan)
fi
scp -o StrictHostKeyChecking=no "$MWAN_BIN" "$VM950:/usr/local/bin/mwan"

echo ""
echo "=== Phase 3: Create LXC 100 ==="
ssh "$SUBURBAN" "
    pct stop 100 2>/dev/null || true
    sleep 2
    pct destroy 100 2>/dev/null || true
    sleep 1
    pct create 100 local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst \
        --hostname mwan-failover-test \
        --ostype debian \
        --cores 1 \
        --memory 512 \
        --unprivileged 0 \
        --features nesting=1 \
        --net0 name=eth0,bridge=vmbr6,ip=dhcp,type=veth \
        --net1 name=eth1,bridge=vmbr2,ip=10.250.250.4/29,ip6=3d06:bad:b01:201::4/64,type=veth \
        --rootfs local-zfs:4 \
        --ssh-public-keys /root/.ssh/authorized_keys
    pct start 100
    echo 'LXC 100 created and started'
"

echo ""
echo "=== Phase 4: TODO ==="
echo "  - Fill in MAC addresses in .link files (from qm config 950 output above)"
echo "  - Deploy nftables.conf to VM 950"
echo "  - Deploy update-routes.sh and update-npt.sh to VM 950"
echo "  - Configure OPNsense VM 101"
echo "  - Create Cloudflare tunnel"
echo "  - Run: mwan cutover preflight"

echo ""
echo "=== Done ==="
