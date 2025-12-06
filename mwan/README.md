# Multi-WAN Load Balancer (mwan)

Single-VM Debian 13 configuration for dual-WAN (AT&T + Webpass) load balancing with PCI passthrough.

**mwan VM**: All-in-one solution:

- AT&T 802.1X authentication (X710 VF with trust mode)
- Webpass WAN (I226-V full passthrough)
- Load balancing, 1:1 NAT, NPT, health monitoring

## Quick Start

```bash
# 0. On Proxmox host (vault), configure X710 VFs for 802.1X
ssh root@vault
echo 1 > /sys/bus/pci/devices/0000:02:00.0/sriov_numvfs
ip link set enp2s0f0np0 vf 0 trust on
ip link set enp2s0f0np0 vf 0 spoof off
ip link show enp2s0f0np0  # Verify: trust on, spoof off
lspci | grep "Virtual Function"  # Note VF PCI address (e.g., 02:02.0)
exit

cd ansible

# 1. Deploy mwan VM (all-in-one: auth + load balancing)
ansible-playbook -i inventory playbooks/deploy-mwan.yml
# → Creates VM with PCI passthrough (VF 02:02.0 + Webpass NIC 06:00.0)

# 2. Upload AT&T certificates
scp agoodkind@router:/conf/opnatt/wpa/*.pem root@mwan.home.goodkind.io:/etc/wpa_supplicant/
ansible-playbook -i inventory playbooks/deploy-mwan.yml
# → Starts wpa_supplicant for AT&T authentication

# Verify interface names after deployment:
ssh root@mwan.home.goodkind.io "ip link show"
# Update group_vars/mwan_servers.yml if needed, re-run playbook
```

**Playbook is idempotent** - re-run anytime to apply config changes.

## X710 VF Setup (Proxmox Host)

### Initial VF Creation

On vault, create one VF from X710 port 1 for AT&T 802.1X authentication:

```bash
# Create VF
echo 1 > /sys/bus/pci/devices/0000:02:00.0/sriov_numvfs

# Configure VF for 802.1X (REQUIRED)
ip link set enp2s0f0np0 vf 0 trust on    # Allow EAPOL frames
ip link set enp2s0f0np0 vf 0 spoof off   # Allow MAC changes

# Optional: Set specific MAC (otherwise gets random MAC)
# ip link set enp2s0f0np0 vf 0 mac D0:FC:D0:7C:85:30

# Verify configuration
ip link show enp2s0f0np0
# Should show: vf 0 ... spoof checking off, link-state auto, trust on

# VF interface will appear as: enp2s0f0v0

# Find VF PCI address
lspci | grep "Virtual Function"
# Example output: 02:02.0 Ethernet controller: Intel Corporation Ethernet Virtual Function 700 Series
```

### Make VF Configuration Persistent

Create systemd service to configure VF on boot:

TODO: put this in a jinja file and add to the deploy system (jinja to set mac address)
TODO: update ansible playbook to use the VF

```bash
cat > /etc/systemd/system/x710-vf-setup.service << 'EOF'
[Unit]
Description=Configure X710 VFs for 802.1X
After=network-pre.target
Before=network.target

[Service]
Type=oneshot
ExecStart=/bin/bash -c 'echo 1 > /sys/bus/pci/devices/0000:02:00.0/sriov_numvfs'
ExecStart=/usr/bin/sleep 1
ExecStart=/usr/sbin/ip link set enp2s0f0np0 vf 0 trust on
ExecStart=/usr/sbin/ip link set enp2s0f0np0 vf 0 spoof off
# Optional: Set specific MAC address for VF
# ExecStart=/usr/sbin/ip link set enp2s0f0np0 vf 0 mac D0:FC:D0:7C:85:30
ExecStop=/bin/bash -c 'echo 0 > /sys/bus/pci/devices/0000:02:00.0/sriov_numvfs'
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

systemctl enable x710-vf-setup.service
systemctl start x710-vf-setup.service
```

**Why trust mode is required:**

- `trust on`: Allows non-standard EtherTypes (EAPOL frames `0x888e` for 802.1X)
- `spoof off`: Allows wpa_supplicant to change MAC address during authentication
- Without these settings, AT&T 802.1X authentication will fail

## Architecture

**mwan VM** (PCI passthrough + virtio, 2GB):

- eth0: virtio → vmbr0 (management)
- eth1: **X710 VF** (02:02.0, trust mode) → wpa_supplicant → VLAN 3242 → AT&T WAN
- eth2: **I226-V NIC** (06:00.0, full passthrough) → Webpass WAN
- eth3: virtio → mwanbr → OPNsense WAN
- eth4: virtio → mbrains (Monkeybrains, optional)

**OPNsense sees:** Single WAN at 10.250.250.1 (mwan gateway)

**Key Points:**

- **AT&T 802.1X**: wpa_supplicant runs in mwan VM (X710 VF with trust mode)
- **Webpass**: Full I226-V NIC passthrough (avoids MAC conflicts with Webpass ISP)
- **X710 VF**: Trust mode required for EAPOL (802.1X) frames
- **Single VM**: Simpler architecture - all WAN logic in one place
- **Cloud-init**: SSH keys auto-deployed, no manual bootstrap needed

## Configuration Files

### mwan VM

File | Purpose
-----|--------
/etc/network/interfaces | PCI passthrough devices + VLAN 3242 config
/etc/wpa_supplicant/wpa_supplicant.conf | AT&T 802.1X authentication
/etc/dhcpcd.conf | DHCPv4/v6 + Prefix Delegation with DUID
/etc/dhcpcd.exit-hook | Dynamic prefix handling and NPT updates
/etc/nftables.conf | NAT, NPT, connection marking, and filtering
/etc/sysctl.d/99-mwan.conf | Kernel parameters (generated from template with interface names)
/etc/iproute2/rt_tables | Custom routing tables (att, webpass, monkeybrains)
/usr/local/bin/update-npt.sh | Dynamic NPT rule updates
/usr/local/bin/update-routes.sh | Policy routing table updates
/usr/local/bin/health-check.sh | WAN health monitoring daemon

## Post-Deployment

### Verify Services

```bash
# Check all services on mwan
ssh root@mwan.home.goodkind.io
wpa_cli status  # AT&T 802.1X authentication
systemctl status wpa_supplicant-mwan dhcpcd nftables mwan-health

# Check interfaces
ip addr show
ip -6 addr show

# Check routing tables
ip route show table att
ip route show table webpass

# Check nftables
nft list ruleset

# Check health status
/usr/local/bin/health-check.sh --status
```

### Troubleshooting

**AT&T 802.1X not authenticating:**

- **On Proxmox host**, verify VF trust mode: `ssh root@vault "ip link show enp2s0f0np0"`
  - Must show: `vf 0 ... spoof checking off, trust on`
  - If not: `ip link set enp2s0f0np0 vf 0 trust on && ip link set enp2s0f0np0 vf 0 spoof off`
- Check wpa_supplicant logs: `journalctl -u wpa_supplicant-mwan -f`
- Verify certificates are present: `ls -la /etc/wpa_supplicant/*.pem`
- Check VF is assigned to VM: `qm config <VMID> | grep hostpci`
- Verify VLAN 3242 interface exists: `ip link show | grep 3242`

**Webpass not getting DHCP:**

- With full PCI passthrough, VM uses real NIC MAC (no spoofing)
- Verify Webpass NIC is assigned: `lspci | grep I226`
- Check DUID in dhcpcd.conf matches: `grep duid /etc/dhcpcd.conf`
- Check dhcpcd logs: `journalctl -u dhcpcd -f`
- Webpass is MAC-sensitive - using real hardware MAC avoids conflicts

**NPT not working:**

- Check delegated prefix: `ip -6 addr show | grep inet6 | grep -v fe80`
- Check nftables rules: `nft list table ip6 nat`
- Run dhcpcd hook manually: `/etc/dhcpcd.exit-hook`

**Health check not failing over:**

- Check health status: `/usr/local/bin/health-check.sh --status`
- Check logs: `journalctl -u mwan-health -f`
- Run manual check: `/usr/local/bin/health-check.sh --check`

## OPNsense Migration

### Phase 1: Parallel Testing

1. **Add vNIC to OPNsense** on vmbr_mwan
2. **Configure new interface**:
   - IPv4: `10.250.250.2/29`
   - IPv6: `3d06:bad:b01:fe::2/64`
   - Gateway: `10.250.250.1`
3. **Test connectivity** through mwan gateway
4. **Point test firewall rule** to use mwan gateway

### Phase 2: Cutover

1. **Change default gateway** to mwan (`10.250.250.1`)
2. **Verify traffic flows** through load balancer
3. **Remove old multi-WAN config**:
   - Delete gateway groups
   - Delete NPT rules
   - Remove AT&T and Webpass interfaces
   - Remove opnatt scripts

## Static IP Mappings

| Internal | AT&T | Webpass | Purpose |
|----------|------|---------|---------|
| 10.250.250.2 | 104.57.226.193 | 136.25.91.241 | OPNsense primary |
| 10.250.250.3 | 104.57.226.194 | 136.25.91.242 | Service 1 |
| 10.250.250.4 | 104.57.226.195 | 136.25.91.243 | Service 2 |
| 10.250.250.5 | 104.57.226.196 | 136.25.91.244 | Service 3 |
| 10.250.250.6 | 104.57.226.197 | 136.25.91.245 | Service 4 |

## Future Enhancements

- **Phase 3**: Add Monkeybrains as failover WAN
- **Phase 4**: Dynamic DNS for Monkeybrains public IPv4
- **Go Rewrite**: Single binary orchestrator for better state management

## Architecture Notes

**Why PCI Passthrough:**

- **X710 VF (AT&T)**: Trust mode required for EAPOL (802.1X) frames - can't do this over virtio bridge
- **I226-V (Webpass)**: Full passthrough avoids MAC address conflicts - Webpass is MAC-sensitive
- **Performance**: Direct hardware access, no bridge overhead
- **Simplicity**: Single VM handles everything - no complex bridging between VMs

**Trade-offs:**

- **VM Migration**: Cannot migrate mwan VM (PCI devices are bound to hardware)
- **X710 Usage**: VF consumes one function, but host can still create more VFs from remaining capacity
- **Dependencies**: Requires VF setup on Proxmox host before VM deployment

**Testing checklist:**

1. Verify VF trust mode persists after Proxmox host reboot
2. Verify AT&T 802.1X authentication succeeds
3. Verify Webpass DHCP works with real NIC MAC
4. Test WAN failover between AT&T and Webpass
5. Verify NPT and 1:1 NAT mappings work correctly
