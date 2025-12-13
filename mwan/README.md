# Multi-WAN Load Balancer (mwan)

Single-VM Debian 13 configuration for dual-WAN (AT&T + Webpass) load balancing with PCI passthrough.

**mwan VM**: All-in-one solution for AT&T 802.1X + Webpass WAN + load balancing + 1:1 NAT + NPT + health monitoring.

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

### Why PCI passthrough

- **X710 VF (AT&T)**: trust mode required for EAPOL (802.1X) frames (does not work reliably over a virtio bridge)
- **I226-V (Webpass)**: full passthrough avoids MAC address conflicts (Webpass is MAC-sensitive)
- **Performance / simplicity**: direct NIC access; one VM owns all WAN logic

### Trade-offs

- **VM migration**: cannot live-migrate while PCI devices are attached
- **Hardware dependencies**: VF setup on Proxmox host is a prerequisite

### Testing checklist

1. Verify VF trust mode persists after Proxmox host reboot
2. Verify AT&T 802.1X authentication succeeds
3. Verify Webpass DHCP works with the real NIC MAC
4. Test WAN failover between AT&T and Webpass
5. Verify NPT and 1:1 NAT mappings work correctly

## Goal (end state)

- **Outbound IPv4**: OPNsense SNATs all downstream RFC1918 to `10.250.250.2-10.250.250.6`; MWAN load-balances *new* flows across AT&T/Webpass and performs 1:1 SNAT to the corresponding public /29 on the chosen WAN (see the static mapping table below).
- **Outbound IPv6**: downstream uses internal-only `3d06:bad:b01::/60`; MWAN load-balances new flows and performs NPT to each WAN’s delegated /60.
- **Inbound services**: inbound IPv4/IPv6 to either WAN’s public space is translated on MWAN (DNAT / reverse-NPT) and forwarded to OPNsense so OPNsense rules/port-forwards can handle services.
- **Failover**: when a WAN is unhealthy, new flows stop using it; existing sessions drain naturally; recovery restores balancing.

## IPv6 (NPT + inbound DNPT)

This section describes how MWAN does **stateless outbound NPT** and **inbound reverse-NPT (DNPT)** while keeping downstream addressing stable.

### Internal-only prefix (treated like ULA)

Downstream LANs use `3d06:bad:b01::/60` on purpose. For all intents and purposes, this prefix should be treated as **internal-only** (ULA-like): it is **not** meant to be globally routed on the Internet.

**Why this is not a ULA (`fd00::/8`):**

- Modern OSes decide “IPv6 is probably globally useful” largely based on whether the source address is a **GUA** vs a **ULA** (RFC 6724-ish behavior).
- If you use a ULA internally, many clients will deprioritize it vs IPv4 (or choose it only for destinations they already believe are “local”), even though *we know* it will work globally once MWAN does NPT.
- Using a “fake” GUA-shaped internal prefix keeps clients preferring IPv6, while MWAN is the only place where that prefix becomes Internet-routable via NPT.

The only point where traffic becomes Internet-routable is **on `mwan`**, where NPT (stateless prefix translation) swaps the internal /60 to one of the WAN delegated /60 prefixes.

### IPv6 flow examples

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

### Static IP mappings (internal /29 ↔ public /29s)

| Internal | AT&T | Webpass | Purpose |
|----------|------|---------|---------|
| 10.250.250.2 | 104.57.226.193 | 136.25.91.242 | OPNsense primary |
| 10.250.250.3 | 104.57.226.194 | 136.25.91.243 | Service 1 |
| 10.250.250.4 | 104.57.226.195 | 136.25.91.244 | Service 2 |
| 10.250.250.5 | 104.57.226.196 | 136.25.91.245 | Service 3 |
| 10.250.250.6 | 104.57.226.197 | 136.25.91.246 | Service 4 |

Notes:

- Webpass gateway is `136.25.91.241` (not part of the static mapping set).

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

### Inbound IPv6 (hosting services)

Inbound IPv6 relies on **reverse-NPT (DNPT)** on MWAN:

- Traffic to the WAN delegated `/60` (e.g. `2604:...:be00::/60` or `2600:...:c80::/60`) is translated back to the internal-only `3d06:bad:b01::/60` and forwarded to OPNsense.
- The per-WAN `::1/128` (e.g. `2604:...:be00::1`) is reserved as the MWAN↔OPNsense “edge” on that WAN.

Additionally, `update-npt.sh` can DNAT other global IPv6 addresses assigned to a WAN interface back to OPNsense so those addresses don’t terminate on MWAN unexpectedly — **but this only helps if your ISP actually delivers inbound traffic to those addresses.**

#### Reality check (AT&T): inbound to the DHCPv6 /128 “interface address” is blocked

In practice, AT&T (and many residential ISPs) do **not** reliably deliver inbound traffic to the DHCPv6 /128 “interface address”
(`2001:506:72f7:108c::1/128`). We confirmed this by capturing on MWAN’s AT&T interface while probing from an external host: **no packets arrived**.

So for inbound hosting, treat the WAN “interface /128” path as **best-effort / ISP-dependent**.

#### Recommended inbound IPv6 “edge” addresses (symmetric across WANs)

To keep behavior consistent across providers and avoid extra carve-outs, prefer a single, predictable inbound address per WAN taken from the **delegated /60**:

- **AT&T edge**: `2600:1700:2f71:c80::1` (PD `::1/128`)
- **Webpass edge**: `2604:5500:c271:be00::1` (PD `::1/128`)

Both are DNAT’d on MWAN to the same OPNsense MWAN-link address: `3d06:bad:b01:fe::2`.

#### Optional: hairpin extra WAN interface /128s to OPNsense (ISP-dependent)

MWAN’s WAN interfaces will often have additional globally-routable IPv6 **/128 “interface addresses”** (for example:
Webpass `2604:5500:c271:8000::72b/128` or AT&T `2001:506:72f7:108c::1/128`).

If you want packets sent to those **interface /128s** to be handled by **OPNsense** (instead of terminating on MWAN),
MWAN can DNAT them to a stable IPv6 address on the MWAN↔OPNsense link.

**Requirement on OPNsense (MWAN-facing WAN interface):**

- **IPv6 address**: `3d06:bad:b01:fe::2/64`
- **IPv6 gateway**: MWAN’s link-local on the MWAN link (e.g. `fe80::be24:11ff:fe72:c1`)

Why this is required:

- OPNsense can be link-local-only for normal NPT routing and still work.
- But DNAT cannot practically target a link-local address (it is interface-scoped), so OPNsense needs a stable on-link IPv6
  address for MWAN to DNAT to.

**Caution (don’t break replies):**

- When DNATing WAN interface /128s to `3d06:bad:b01:fe::2`, ensure MWAN’s SNAT rules do not override conntrack’s
  reverse-NAT for the return traffic.
- Because inbound flows are fwmarked by ingress WAN, MWAN’s policy routing tables must also include an explicit
  route for the DNAT target (`3d06:bad:b01:fe::2/128`) via the MWAN↔OPNsense link; otherwise the fwmark default
  route will try to send the packet back out the WAN instead of toward OPNsense.

## NPT rule persistence (why `ip6 nat` chains can be empty)

The IPv6 NPT/DNPT rules live in `table ip6 nat` and are **programmed at runtime** by `update-npt.sh` (not baked into the static `nftables.conf`).

So an empty `table ip6 nat` is not “healthy” — it just means the runtime programming didn’t happen (or was flushed after it happened).

Common reasons this happens:

- **Deploy ordering / reloads**: Ansible reloads `nftables`, which flushes the ruleset. If the WAN links are already in a steady “routable” state, the `networkd-dispatcher` “routable” event may not fire again, so `update-npt.sh` doesn’t get re-triggered automatically.
- **Boot races**: `update-npt.sh` can run before VLAN/NIC devices exist (or before PD is present) and exit early (it uses `set -e`).

How `networkd-dispatcher` fits in:

- `systemd-networkd` tracks link/address state.
- `networkd-dispatcher` watches those state transitions and runs scripts in `/etc/networkd-dispatcher/<state>/` (e.g. “routable”).
- It is event-driven; it does not continuously re-apply rules after an unrelated `nftables` reload.

Manual recovery (no guessing), on MWAN:

```bash
/usr/local/bin/update-npt.sh enatt0.3242 2600:1700:2f71:c80::/60
/usr/local/bin/update-npt.sh enwebpass0 2604:5500:c271:be00::/60
```

After that, the rules appear immediately (example):

- `iif "enatt0.3242" ip6 daddr 2001:506:72f7:108c::1 dnat to 3d06:bad:b01:fe::2`
- `oif "enatt0.3242" ip6 saddr 3d06:bad:b01:fe::2 ct status dnat return`

### How to ensure it runs on deploy and reboot

- **Deploy**: the playbook reloads `nftables` and then re-applies NPT rules.
- **Boot**: enable and run `mwan-update-npt.service` (oneshot) so NPT rules are applied even if hooks don’t fire in the
  right order.

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

### Test plan (what “working” looks like)

#### Outbound (from a downstream LAN host)

```bash
# IPv4 should alternate between WANs across NEW connections
watch -n 1 'curl -4 -s ifconfig.co; echo'

# IPv6 should alternate between PD prefixes across NEW connections
watch -n 1 'curl -6 -s ifconfig.co; echo'
```

#### Failover

- Simulate a WAN down (unplug or block health targets) and confirm:
  - New outbound sessions stop using the failed WAN.
  - Traffic continues via the remaining WAN.
- When restored, confirm balancing returns.

#### External inbound IPv4 (from a host on a different network)

- Pick a mapped public IP (example: Webpass `136.25.91.242` → internal `10.250.250.2`).

```bash
ping -c 3 136.25.91.242

# For TCP services, prefer an explicit port check:
nc -vz 136.25.91.242 443
```

Where to observe:

- **On MWAN** (confirm packet arrives on WAN and is forwarded to OPNsense):

```bash
tcpdump -ni enwebpass0 host 136.25.91.242
tcpdump -ni enmwanbr0 host 10.250.250.2
```

- **On OPNsense**:
  - Firewall live view on the MWAN-facing WAN should show traffic to `10.250.250.2` (after MWAN DNAT).

#### External inbound IPv6

- Verify public v6 for the service reaches MWAN and is reverse-translated toward internal `3d06:bad:b01::/60`.
  - Check `nft list chain ip6 nat prerouting` / `postrouting` on MWAN and validate OPNsense sees the internal destination.
  - Note: some providers block ICMPv6 to CPEs; prefer a TCP check (e.g. `nc -vz <v6> 443`).

##### External inbound IPv6 to WAN interface /128 (hairpin validation)

Example targets:

- Webpass interface /128: `2604:5500:c271:8000::72b`
- AT&T interface /128: `2001:506:72f7:108c::1` (**often blocked by AT&T; you may see zero packets on MWAN**)
- Preferred “works everywhere” targets: the per-WAN PD `::1` addresses (e.g. `2600:1700:2f71:c80::1` and `2604:5500:c271:be00::1`)

Where to observe:

- **On MWAN** (confirm packet arrives on WAN and is forwarded to OPNsense over the MWAN link):

```bash
tcpdump -ni enwebpass0 host 2604:5500:c271:8000::72b
tcpdump -ni enmwanbr0 host 3d06:bad:b01:fe::2
```

- **On OPNsense**:
  - Firewall live view on the MWAN-facing WAN should show traffic with destination `3d06:bad:b01:fe::2` (after MWAN DNAT).

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
   - IPv6: `3d06:bad:b01:fe::2/64` with MWAN link-local as gateway
   - Gateway: `10.250.250.1` (IPv4), `fe80::be24:11ff:fe72:c1` (IPv6)
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
