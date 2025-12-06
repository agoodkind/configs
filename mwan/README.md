# Multi-WAN Load Balancer (mwan)

Two-VM Debian 13 configuration for dual-WAN (AT&T + Webpass) load balancing.

**attauth VM**: AT&T 802.1X auth (X710 passthrough) → bridges to Proxmox
**mwan VM**: Load balancing, 1:1 NAT, NPT, health monitoring (virtio only)

## Quick Start

```bash
cd ansible

# 1. Deploy attauth VM (AT&T 802.1X auth)
ansible-playbook -i inventory playbooks/deploy-attauth.yml -e pci_x710="02:00.0"
# → Creates VM with cloud-init, SSH keys auto-deployed

# Upload AT&T certs and re-run to configure:
scp agoodkind@router:/conf/opnatt/wpa/*.pem root@attauth.home.goodkind.io:/etc/wpa_supplicant/
ansible-playbook -i inventory playbooks/deploy-attauth.yml
# → Configures wpa_supplicant, starts services

# 2. Deploy mwan VM (load balancer)
ansible-playbook -i inventory playbooks/deploy-mwan.yml
# → Creates VM with cloud-init, configures routing, NAT, health monitoring

# Verify interface names after deployment:
ssh root@mwan.home.goodkind.io "ip link show"
# Update group_vars/mwan_servers.yml if needed, re-run playbook
```

**Both playbooks are idempotent** - re-run anytime to apply config changes.

## Architecture

**attauth VM** (X710 passthrough, 512MB):

- Port 1 → wpa_supplicant → VLAN 3242 → bridge to Proxmox "att"
- Port 2 → bridge to Proxmox "lan" (OPNsense LAN unchanged)

**mwan VM** (virtio only, 2GB):

- eth0 ← Proxmox vmbr0 (management)
- eth1 ← Proxmox "att" bridge (authenticated AT&T)
- eth2 ← Proxmox "webpass" bridge
- eth3 → Proxmox "mwanbr" bridge → OPNsense WAN
- eth4 ← Proxmox "mbrains" bridge (optional)

**OPNsense sees:** Single WAN at 10.250.250.1 (mwan gateway)

**Key Points:**

- **AT&T 802.1X**: wpa_supplicant runs in attauth VM (X710 passthrough)
- **mwan VM**: virtio only (no passthrough, can migrate)
- **X710 both ports**: Used by attauth VM, bridges back to Proxmox
- **Cloud-init**: SSH keys auto-deployed, no manual bootstrap needed

## Configuration Files

### attauth VM (AT&T Authentication)

File | Purpose
-----|--------
/etc/network/interfaces | X710 port config, VLAN 3242, bridges to Proxmox
/etc/wpa_supplicant/wpa_supplicant.conf | AT&T 802.1X authentication

### mwan VM (Load Balancer)

File | Purpose
-----|--------
/etc/network/interfaces | Interface config with MAC spoofing for Webpass
/etc/dhcpcd.conf | DHCPv4/v6 + Prefix Delegation with DUID
/etc/dhcpcd.exit-hook | Dynamic prefix handling and NPT updates
/etc/nftables.conf | NAT, NPT, connection marking, and filtering
/etc/sysctl.d/99-mwan.conf | Kernel parameters (forwarding, etc.)
/etc/iproute2/rt_tables | Custom routing tables (att, webpass, monkeybrains)
/usr/local/bin/update-npt.sh | Dynamic NPT rule updates
/usr/local/bin/update-routes.sh | Policy routing table updates
/usr/local/bin/health-check.sh | WAN health monitoring daemon

## Post-Deployment

### Verify Services

```bash
# Check wpa_supplicant (AT&T 802.1X) on attauth
ssh root@attauth.home.goodkind.io
wpa_cli status
systemctl status wpa_supplicant-attauth

# Check services on mwan
ssh root@mwan.home.goodkind.io
systemctl status dhcpcd nftables mwan-health

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

- Check wpa_supplicant logs: `journalctl -u wpa_supplicant-attauth -f`
- Verify certificates are present: `ls -la /etc/wpa_supplicant/*.pem`
- Check Debian wpa_supplicant version supports legacy options

**Webpass not getting DHCP:**

- Verify MAC spoofing: `ip link show eth2` (should show `00:e2:69:66:8b:5a`)
- Check DUID in dhcpcd.conf matches: `grep duid /etc/dhcpcd.conf`
- Check dhcpcd logs: `journalctl -u dhcpcd -f`

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
