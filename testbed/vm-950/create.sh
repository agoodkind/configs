#!/bin/bash
# Create the full MWAN testbed VM on suburban.
# Mirrors production VM 113 with 5 NICs (management, internal, 3x WAN).
# Run from suburban as root.
set -euo pipefail

VMID=950
NAME="test-mwan"
TEMPLATE="/var/lib/vz/template/iso/debian-13-generic-amd64.qcow2"
STORAGE="local-zfs"

echo "=== Destroying old VM $VMID if exists ==="
qm stop $VMID 2>/dev/null || true
sleep 2
qm destroy $VMID 2>/dev/null || true
sleep 1

echo "=== Creating VM $VMID ==="
qm create $VMID \
  --name "$NAME" \
  --ostype l26 \
  --machine q35 \
  --cores 2 \
  --memory 2048 \
  --agent enabled=1 \
  --serial0 socket \
  --vga serial0 \
  --scsihw virtio-scsi-pci \
  --boot order=scsi0 \
  --net0 virtio,bridge=vmbr1 \
  --net1 virtio,bridge=vmbr2 \
  --net2 virtio,bridge=vmbr4 \
  --net3 virtio,bridge=vmbr5 \
  --net4 virtio,bridge=vmbr6 \
  --ipconfig0 ip=dhcp,ip6=3d06:bad:b01:200::950/64,gw6=fe80::1 \
  --ciuser root \
  --sshkeys /root/.ssh/authorized_keys

# Import the cloud image as the boot disk
qm importdisk $VMID "$TEMPLATE" "$STORAGE" 2>&1 | tail -1
DISK=$(pvesm list $STORAGE 2>/dev/null | grep "vm-${VMID}-disk" | tail -1 | awk '{print $1}')
qm set $VMID --scsi0 "${DISK},discard=on,size=16G"

echo "=== VM $VMID created ==="
qm config $VMID | grep -E 'net|name|cores|memory'
echo ""
echo "Start with: qm start $VMID"
