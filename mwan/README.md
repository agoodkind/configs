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
scp 'agoodkind@router:/conf/opnatt/wpa/*.pem' root@mwan.home.goodkind.io:/etc/wpa_supplicant/
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

## IPv6 NPT (How it’s intended to work)

### Internal-only prefix (treated like ULA)

Downstream LANs use `3d06:bad:b01::/60` on purpose. For all intents and purposes, this prefix should be treated as **internal-only** (ULA-like): it is **not** meant to be globally routed on the Internet.

The only point where traffic becomes Internet-routable is **on `mwan`**, where NPT (stateless prefix translation) swaps the internal /60 to one of the WAN delegated /60 prefixes.

### IPv4 flow examples

#### Example 1: VLAN100 client → OPNsense → MWAN → AT&T (mark 1) → Internet

- **Client (VLAN100)**: `3d06:bad:b01:1::100`
- **GW (OPNsense VLAN100)**: `3d06:bad:b01:1::1`
- **OPNsense WAN next-hop**: MWAN link-local on the OPNsense↔MWAN link (e.g. `fe80::be24:11ff:fe72:c1%<opnsense_wan_if>`)
- **On MWAN**:
  - MWAN marks the new flow: **mark=1**
  - Policy routing sends it out AT&T (fwmark 1 → table `att`)
  - **Outbound NPT** swaps the prefix:
    - `3d06:bad:b01:1::100` → `2600:1700:2f71:c81::100`
  - Return traffic arriving on AT&T gets **reverse NPT** back to:
    - `3d06:bad:b01:1::100`

#### Example 2: VLAN200 client → OPNsense → MWAN → Webpass (mark 2) → Internet

- **Client (VLAN200)**: `3d06:bad:b01:2::50`
- **GW (OPNsense VLAN200)**: `3d06:bad:b01:2::1`
- **On MWAN**:
  - New flow gets marked: **mark=2**
  - Policy routing sends it out Webpass (fwmark 2 → table `webpass`)
  - **Outbound NPT** swaps the prefix:
    - `3d06:bad:b01:2::50` → `2604:5500:c271:be02::50`
  - Return traffic gets reverse-translated back to:
    - `3d06:bad:b01:2::50`

### What load-balanced IPv6 should look like

For hosts using `3d06:bad:b01::/60`, repeated *new* outbound connections should alternate between the two ISP prefixes. For example:

- `watch curl -6 ifconfig.co` should alternate between:
  - `2600:1700:2f71:c8x::…` (AT&T /60)
  - `2604:5500:c271:be0x::…` (Webpass /60)

Each individual TCP session stays pinned to the chosen WAN for the duration (session affinity via conntrack mark restore), while new sessions can be distributed 50/50.

### Nuances / gotchas (read this first when debugging IPv6)

- **OPNsense WAN is link-local-only (by design)**:
  - OPNsense has only `fe80::/64` on its WAN interface toward MWAN.
  - Any MWAN routes “back toward LAN” must use the **OPNsense WAN link-local as next-hop**.

- **The internal `/60` is not on-link from MWAN’s perspective**:
  - If MWAN has `3d06:bad:b01::/60 dev enmwanbr0` in table 100/200, MWAN will attempt neighbor discovery for each internal host on the OPNsense↔MWAN link.
  - This commonly shows up as: `curl -6` sometimes works but `ping -6` fails (ICMPv6 echo replies are stateless and can take a path that does not reliably re-use conntrack marks).
  - Correct shape is:
    - `3d06:bad:b01::/60 via <opnsense_wan_linklocal> dev enmwanbr0` in **table 100**, **table 200**, and **main**.

- **IPv6 default routes may be multipath**:
  - When both WANs are up, MWAN can have a multipath default (`default nexthop via … dev …`).
  - Any scripts that “discover” a WAN gateway must handle both the single-path and multipath formats.

- **IPv6 NPT rules are programmed at runtime**:
  - `nftables.conf` contains *empty* `table ip6 nat { prerouting/postrouting }` chains.
  - `update-npt.sh` (triggered by `55-update-npt.sh`) adds the actual NPT rules and assigns PD `::1/128` to each WAN.
  - If the nftables ruleset is flushed/reloaded after interfaces are already “routable”, NPT rules may disappear until `update-npt.sh` is run again.
    - Quick recovery: run `/usr/local/bin/update-npt.sh enatt0.3242 <prefix>` and `/usr/local/bin/update-npt.sh enwebpass0 <prefix>`.

### Nuances / gotchas (read this first when debugging IPv4 load balancing)

- **Do not override IPv4 fwmarks in `inet mangle prerouting` for `10.250.250.2-10.250.250.6`**:
  - IPv4 load balancing is driven by the per-flow random fwmark assignment for *new* connections.
  - If you set `meta mark` based only on `ip saddr 10.250.250.x`, you will pin that host to a single WAN and `watch curl -4 ifconfig.co` will stop alternating.

#### Quick IPv6 sanity checks

On MWAN:

```bash
# Policy routing tables should send the internal /60 back to OPNsense (via link-local)
ip -6 route show table 100
ip -6 route show table 200
ip -6 route show | grep -F '3d06:bad:b01::/60'

# Confirm fwmark policy rules exist
ip -6 rule show

# Confirm NPT rules exist for both WANs
nft -a list chain ip6 nat postrouting
nft -a list chain ip6 nat prerouting
```

On OPNsense:

```bash
# Confirm OPNsense can reach MWAN via link-local (this should always work)
ping -6 <mwan_internal_linklocal>%<wan_if>
```

## IPv4 (OPNsense NATs downstream → MWAN load-balances + maps to public /29s)

For IPv4, **OPNsense only “sees” the MWAN internal link** (`10.250.250.0/29`). OPNsense is responsible for NATing all downstream RFC1918 networks (e.g. VLAN100/VLAN200) into that internal /29. MWAN then:

- **Load-balances flows** across AT&T vs Webpass (mark 1 / mark 2)
- **Maps the internal /29 to each WAN’s public /29** (dual 1:1 mapping: one external mapping per WAN)

### Flow examples

#### Example 1: VLAN100 client → OPNsense NAT to `10.250.250.2` → MWAN chooses AT&T → Internet

1. Client on VLAN100 (e.g. `10.250.1.45`) sends traffic to OPNsense.
2. OPNsense SNATs source to an internal MWAN address (e.g. `10.250.250.2`) and forwards it to MWAN (`10.250.250.1`).
3. MWAN marks the new flow **mark=1** (AT&T) and policy-routes it out AT&T.
4. MWAN maps `10.250.250.2` to the AT&T public /29 (e.g. `10.250.250.2` → `104.57.226.193`) and sends it out AT&T.
5. Return traffic to `104.57.226.193` arrives on AT&T at MWAN, is mapped back to `10.250.250.2`, then forwarded to OPNsense, which de-NATs back to `10.250.1.45`.

#### Example 2: VLAN100 client → OPNsense NAT to `10.250.250.2` → MWAN chooses Webpass → Internet

Same as above, except MWAN marks the new flow **mark=2** (Webpass) and maps `10.250.250.2` to the Webpass public /29 (e.g. `10.250.250.2` → `136.25.91.242`).

### What load-balanced IPv4 should look like

From a downstream host (e.g. VLAN100), repeated *new* outbound requests should alternate between the two WAN public IPv4s for the chosen internal mapping:

- `watch curl -4 ifconfig.co` should alternate between:
  - `104.57.226.19x` (AT&T public /29)
  - `136.25.91.24x` (Webpass public /29)

### Inbound IPv4 (hosting services)

Inbound traffic to the public /29s must be **DNAT’d on MWAN** back to the internal /29 so OPNsense can see it.

- **Example**: external host pings `136.25.91.242`
  - Packet arrives at MWAN on `enwebpass0` with `dst=136.25.91.242`
  - MWAN **DNATs** it to `dst=10.250.250.2` and forwards it to OPNsense over `enmwanbr0`
  - OPNsense then applies its own inbound rules/port-forwards (if any)

Additionally, MWAN marks **inbound NEW flows** based on ingress WAN:

- `iif enatt0.3242` → **mark=1** (AT&T)
- `iif enwebpass0` → **mark=2** (Webpass)

This keeps replies **symmetric** (return path uses the same WAN the flow came in on).

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
- Webpass is MAC-sensitive - using real hardware MAC avoids conflicts

## OPNsense Migration

### Phase 1: Parallel Testing

1. **Add vNIC to OPNsense** on vmbr_mwan
2. **Configure new interface**:
   - IPv4: `10.250.250.2/29`
   - IPv6: Link-local with mwanbr LL as gateway
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
| 10.250.250.2 | 104.57.226.193 | 136.25.91.242 | OPNsense primary |
| 10.250.250.3 | 104.57.226.194 | 136.25.91.243 | Service 1 |
| 10.250.250.4 | 104.57.226.195 | 136.25.91.244 | Service 2 |
| 10.250.250.5 | 104.57.226.196 | 136.25.91.245 | Service 3 |
| 10.250.250.6 | 104.57.226.197 | 136.25.91.246 | Service 4 |

Notes:

- Webpass gateway is `136.25.91.241` (not part of the static mapping set).

## Tracing (deploy + boot)

MWAN scripts can emit **structured JSON logs** to `/var/log/mwan-debug.log` when `mwan_debug_logging: true`.
Each log line includes a **traceId** so you can correlate events across:

- `systemd-networkd` / `networkd-dispatcher` hooks
- `update-routes.sh`, `update-npt.sh`
- `health-check.sh`

Trace ID sources:

- **Boot trace**: `mwan-trace-boot.service` writes `/run/mwan-trace-id` and `/var/lib/mwan/trace-id`
- **Deploy trace**: `deploy-mwan.yml` writes the same files at the start of deploy

Quick usage on MWAN:

```bash
cat /run/mwan-trace-id
tail -n 200 /var/log/mwan-debug.log
journalctl -b --no-pager | grep -F 'traceId=' | tail -n 50
```

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
