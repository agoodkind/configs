# Multi-WAN Load Balancer (mwan)

Single-VM Debian 13 configuration for dual-WAN (AT&T + Webpass) load balancing with PCI passthrough.

**mwan VM** is an all-in-one solution for AT&T 802.1X + Webpass WAN + load balancing + 1:1 NAT + NPT + health monitoring.

## Overview

This design keeps the downstream network simple:

- **OPNsense** sees a single “upstream” router: MWAN.
- **MWAN** owns all WAN complexity: authentication, policy routing, NAT44 (1:1), NPTv6 (stateless), health checks, and failover.

High-level goals:

- **Outbound IPv4**: OPNsense SNATs downstream RFC1918 into `10.250.250.2-10.250.250.6`; MWAN load-balances *new* flows and performs 1:1 SNAT to the corresponding public /29 on the chosen WAN.
- **Outbound IPv6**: downstream uses internal-only `3d06:bad:b01::/60`; MWAN load-balances *new* flows and performs NPT to each WAN’s delegated /60.
- **Inbound services**: inbound IPv4/IPv6 to either WAN’s public space is translated on MWAN (DNAT / reverse-NPT) and forwarded to OPNsense so OPNsense rules/port-forwards can handle services.
- **Failover**: when a WAN is unhealthy, new flows stop using it; existing sessions drain naturally; recovery restores balancing.

## Architecture

**mwan VM** (PCI passthrough + virtio, 2GB):

- eth0: virtio → vmbr0 (management)
- eth1: **X710 VF** (trust mode) → wpa_supplicant → VLAN 3242 → AT&T WAN
- eth2: **I226-V NIC** (full passthrough) → Webpass WAN
- eth3: virtio → mwanbr → OPNsense WAN
- eth4: virtio → mbrains (Monkeybrains, optional)

**OPNsense sees:** single upstream at `10.250.250.1` (mwan gateway)

### Addressing summary

- **MWAN↔OPNsense IPv4 link**: `10.250.250.0/29`
  - MWAN: `10.250.250.1/29`
  - OPNsense: `10.250.250.2/29` (plus `10.250.250.3-6` used as SNAT identities)
- **Downstream IPv6 internal prefix**: `3d06:bad:b01::/60` (internal-only, “fake GUA” shaped)
- **WAN IPv6 delegated prefixes** (examples):
  - AT&T PD: `2600:1700:2f71:c80::/60`
  - Webpass PD: `2604:5500:c271:be00::/60`

### Static IPv4 mappings (internal /29 ↔ public /29s)

| Internal | AT&T | Webpass | Purpose |
|----------|------|---------|---------|
| 10.250.250.2 | 104.57.226.193 | 136.25.91.242 | OPNsense primary |
| 10.250.250.3 | 104.57.226.194 | 136.25.91.243 | Service 1 |
| 10.250.250.4 | 104.57.226.195 | 136.25.91.244 | Service 2 |
| 10.250.250.5 | 104.57.226.196 | 136.25.91.245 | Service 3 |
| 10.250.250.6 | 104.57.226.197 | 136.25.91.246 | Service 4 |

Notes:

- Webpass gateway is `136.25.91.241` (not part of the static mapping set).

### IPv4 data-plane diagram

```
Downstream RFC1918
   |
   |  (OPNsense SNAT) src 10.250.X.Y -> 10.250.250.2-10.250.250.6
   v
OPNsense WAN (to MWAN)
   - 10.250.250.2/29
   - gw 10.250.250.1
   |
   v
MWAN internal
   - 10.250.250.1/29
   - mark NEW flows (1=AT&T, 2=Webpass)
   - 1:1 SNAT to the selected WAN public /29
   |
   +--------------------------+--------------------------+
   |                          |                          |
   v                          v                          v
AT&T WAN                  Webpass WAN                (optional)
104.57.226.192/29         136.25.91.240/29          Monkeybrains

Inbound IPv4:
Internet -> (MWAN DNAT public -> 10.250.250.X) -> OPNsense -> internal services
```

### IPv6 data-plane diagram

```
Downstream IPv6 (internal-only)
   - 3d06:bad:b01::/60
   |
   v
OPNsense
   - LANs: 3d06:bad:b01:...::/64
   - WAN to MWAN: link-local (fe80::/64)
   - (required for inbound hairpin DNAT target) 3d06:bad:b01:fe::2/64
   |
   |  next-hop is MWAN link-local on mwanbr
   v
MWAN
   - mark NEW flows (1=AT&T, 2=Webpass)
   - policy route by fwmark
   - NPT: 3d06:bad:b01::/60 -> selected WAN PD /60
   |
   +--------------------------+--------------------------+
   |                          |
   v                          v
AT&T PD /60                Webpass PD /60
2600:1700:2f71:c80::/60    2604:5500:c271:be00::/60

Inbound IPv6:
Internet -> (MWAN DNPT back to 3d06:bad:b01::/60) -> OPNsense -> internal services
```

### Why PCI passthrough

- **X710 VF (AT&T)**: trust mode required for EAPOL (802.1X) frames (does not work reliably over a virtio bridge)
- **I226-V (Webpass)**: full passthrough avoids MAC address conflicts (Webpass is MAC-sensitive)
- **Performance / simplicity**: direct NIC access; one VM owns all WAN logic

### Trade-offs

- **VM migration**: cannot live-migrate while PCI devices are attached
- **Hardware dependencies**: VF setup on Proxmox host is a prerequisite

## Quick Start

Deploy is fully automated via Ansible; re-running the playbook is expected.

```bash
cd ansible
ansible-playbook -i inventory playbooks/deploy-mwan.yml
```

If AT&T 802.1X certs aren’t present yet:

```bash
scp 'agoodkind@router:/conf/opnatt/wpa/*.pem' root@mwan.home.goodkind.io:/etc/wpa_supplicant/
ansible-playbook -i inventory playbooks/deploy-mwan.yml
```

Verify interface names after deployment:

```bash
ssh root@mwan.home.goodkind.io "ip link show"
```

## Setup notes (Proxmox host: X710 VF)

On `vault`, create a VF and set trust/spoof settings so EAPOL frames work.

- Create VF: `echo 1 > /sys/bus/pci/devices/0000:02:00.0/sriov_numvfs`
- Enable trust mode: `ip link set enp2s0f0np0 vf 0 trust on`
- Disable spoof checking: `ip link set enp2s0f0np0 vf 0 spoof off`
- Find VF PCI address: `lspci | grep "Virtual Function"` (example: `02:02.0`)

Persist those settings with a small systemd oneshot unit on `vault`:

- **Where**: `/etc/systemd/system/x710-vf-setup.service`
- **Enable**: `systemctl enable --now x710-vf-setup.service`

Why trust mode is required:

- `trust on`: allows non-standard EtherTypes (EAPOL frames `0x888e` for 802.1X)
- `spoof off`: allows wpa_supplicant to change MAC address during authentication

## How the networking stack works (systemd-networkd + networkd-dispatcher)

This project uses **systemd’s network “ecosystem”**, which is a different mental model than `ifupdown`:

- `ifupdown`: imperative “bring interface up/down” actions, typically keyed off explicit `ifup` / `ifdown`.
- `systemd-networkd`: declarative config (`.netdev` / `.network`) that is applied whenever a device appears and matches.

Core components:

- **`systemd-networkd`**: configures links, VLANs, bridges, addresses, and routes from files under `/etc/systemd/network/`.
- **`networkd-dispatcher`**: watches `systemd-networkd` state changes and runs hook scripts in state directories under `/etc/networkd-dispatcher/` (e.g. `routable.d/`).
- **Systemd targets (ordering)**:
  - `network-pre.target`: very early networking prep
  - `network.target`: “basic networking is configured”
  - `network-online.target`: “network is usable” (often via `systemd-networkd-wait-online.service`)

What MWAN runs automatically:

- **Routing policy + marks**: `/usr/local/bin/update-routes.sh` (also invoked by health checks)
- **IPv6 NPT/DNPT runtime rules**: `/usr/local/bin/update-npt.sh`
- **Event glue**:
  - `networkd-dispatcher` “routable” hooks call `update-routes.sh` and `update-npt.sh`
  - `mwan-health` continuously probes WANs and calls `update-routes.sh` on failure/recovery
  - `mwan-update-npt.service` exists because `nftables` reloads can flush dynamic NPT rules without generating a new networkd event

### Services + hooks (who calls what)

`systemd-networkd` configures interfaces based on files under `/etc/systemd/network/`. A “device appears” when the kernel creates a netdevice (physical NIC, virtio NIC, VLAN netdev, bridge, etc) and it shows up in `ip link` and `/sys/class/net/…`; `systemd-networkd` is notified via netlink and matches it to `.network/.netdev` config.

`networkd-dispatcher` is **event-driven via D-Bus signals from `systemd-networkd`** (no polling) and runs scripts when an interface changes operational state (e.g. becomes `routable`). If `networkd-dispatcher` starts after an interface has already reached a steady state, it may not replay past transitions unless it is configured to run startup triggers (package feature: `--run-startup-triggers`).

- **`mwan-trace-boot.service`** → writes `/run/mwan-trace-id` early in boot
- **`systemd-networkd.service`** → creates/links netdevs, configures addresses/routes/DHCP from `.network/.netdev`
- **`networkd-dispatcher.service`** → on `routable`, runs:
  - `/etc/networkd-dispatcher/routable.d/50-update-routes.sh` → `/usr/local/bin/update-routes.sh` (updates `ip rule`/`ip route` policy tables)
  - `/etc/networkd-dispatcher/routable.d/55-update-npt.sh` → `/usr/local/bin/update-npt.sh` (programs `nft` `table ip6 nat` runtime rules + adds PD `::1/128`)
- **`nftables.service`** → loads the base `/etc/nftables.conf` ruleset (includes *empty* `table ip6 nat` chains; NPT rules are added later)
- **`wpa_supplicant-mwan.service`** → runs AT&T 802.1X (EAPOL) on the parent interface
  - **`wpa-cli-action.service`** → `wpa_cli -a /usr/local/bin/wpa-action.sh` → creates/removes `/run/wpa_supplicant-mwan.authenticated`
  - **`wpa-authenticated.path`** → triggers **`wpa-authenticated.service`** → starts **`bringup-att-vlan.service`**
    - **`bringup-att-vlan.service`** → `/usr/local/bin/bringup-att-vlan.sh` → `networkctl renew/reconfigure` to trigger DHCP on the VLAN after auth
- **`mwan-update-routes.service`** → one-shot safety net at boot: runs `update-routes.sh`
- **`mwan-update-npt.service`** → one-shot safety net at boot (and after deploy-time reload): runs `update-npt.sh` for each WAN
- **`mwan-health.service`** → runs `/usr/local/bin/health-check.sh --daemon` → `ping`/`ping6` + optional `curl` checks; calls `update-routes.sh` when a WAN is marked down/up

Typical state flows:

- **Boot**: devices appear → networkd config applies → WAN becomes routable → dispatcher triggers hooks → NPT/routes applied → health loop begins.
- **Deploy / reboot**: an `nftables` reload can flush runtime NPT rules; if the WAN was already routable, dispatcher may not fire again, so we run `mwan-update-npt.service` to reapply.
  - **Why dispatcher doesn’t fire**: `networkd-dispatcher` is event-driven off `systemd-networkd` **state transitions** (e.g. “configuring” → “routable”). Reloading `nftables` does not change link/address state, so there’s no new transition to trigger hooks.
- **Link down / up (hard failure)**: carrier loss/return changes `systemd-networkd` state and can trigger `networkd-dispatcher` hooks.
- **Soft failure / recovery (health state)**: the interface stays `UP`, but upstream connectivity is broken/degraded.
  - **Important**: “down” here means **MWAN health state**, not necessarily `ip link state DOWN`.
    - If the kernel link is `DOWN`, MWAN can’t send probes and will treat it as down immediately.
  - **How we detect a soft failure** (`mwan-health` / `health-check.sh`):
    - Probes multiple targets per WAN over that WAN interface (IPv6 first, then IPv4; optional HTTP checks).
    - Probes used today: `ping6` / `ping` plus optional `curl -6` / `curl -4` to configured HTTP endpoints (no `nc`).
    - Marks a WAN **down** after **N consecutive failed check cycles** (`failure_threshold`) to avoid flapping.
  - **How we detect recovery**:
    - Marks a WAN **up** only after **M consecutive successful check cycles** (`recovery_threshold`) — hysteresis to prevent oscillation during partial recovery.
  - **What happens on down/up**:
    - Calls `update-routes.sh` to remove/add the WAN for *new* flows (existing sessions drain via conntrack).

## IPv6 (NPT + inbound DNPT)

This section describes how MWAN does **stateless outbound NPT** and **inbound reverse-NPT (DNPT)** while keeping downstream addressing stable.

### Internal-only prefix (treated like ULA)

Downstream LANs use `3d06:bad:b01::/60` on purpose. For all intents and purposes, this prefix should be treated as **internal-only** (ULA-like): it is **not** meant to be globally routed on the Internet.

**Why this is not a ULA (`fd00::/8`):**

- Modern OSes decide “IPv6 is probably globally useful” largely based on whether the source address is a **GUA** vs a **ULA** (RFC 6724-ish behavior).
- If you use a ULA internally, many clients will deprioritize it vs IPv4, even though *we know* it will work globally once MWAN does NPT.
- Using a “fake” GUA-shaped internal prefix keeps clients preferring IPv6, while MWAN is the only place where that prefix becomes Internet-routable via NPT.

The only point where traffic becomes Internet-routable is **on `mwan`**, where NPT (stateless prefix translation) swaps the internal /60 to one of the WAN delegated /60 prefixes.

### IPv6 flow examples

#### Example 1: VLAN100 client → OPNsense → MWAN → AT&T (mark 1) → Internet

- **Client (VLAN100)**: `3d06:bad:b01:1::100`
- **GW (OPNsense VLAN100)**: `3d06:bad:b01:1::1`
- **OPNsense WAN next-hop**: MWAN link-local on the OPNsense↔MWAN link (e.g. `fe80::...%<opnsense_wan_if>`)
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

### Inbound IPv6 (hosting services)

Inbound IPv6 relies on **reverse-NPT (DNPT)** on MWAN:

- Traffic to the WAN delegated `/60` is translated back to the internal-only `3d06:bad:b01::/60` and forwarded to OPNsense.
- The per-WAN PD `::1/128` (e.g. `2600:...:c80::1` / `2604:...:be00::1`) is reserved as the MWAN↔OPNsense “edge” on that WAN.

Additionally, `update-npt.sh` can DNAT other global IPv6 addresses assigned to a WAN interface back to OPNsense so those addresses don’t terminate on MWAN unexpectedly — **but this only helps if your ISP actually delivers inbound traffic to those addresses.**

#### Reality check (AT&T): inbound to the DHCPv6 /128 “interface address” is blocked

In practice, AT&T (and many residential ISPs) do **not** reliably deliver inbound traffic to the DHCPv6 /128 “interface address” (example: `2001:506:72f7:108c::1/128`).
We confirmed this by capturing on MWAN’s AT&T interface while probing from an external host: **no packets arrived**.

So for inbound hosting, treat the WAN “interface /128” path as **best-effort / ISP-dependent**.

#### Recommended inbound IPv6 “edge” addresses (symmetric across WANs)

To keep behavior consistent across providers and avoid extra carve-outs, prefer a single, predictable inbound address per WAN taken from the **delegated /60**:

- **AT&T edge**: `2600:1700:2f71:c80::1`
- **Webpass edge**: `2604:5500:c271:be00::1`

Both are DNAT’d on MWAN to the same OPNsense MWAN-link address: `3d06:bad:b01:fe::2`.

#### Optional: hairpin extra WAN interface /128s to OPNsense (ISP-dependent)

If you want packets sent to WAN interface /128s to be handled by OPNsense (instead of terminating on MWAN), MWAN can DNAT them to a stable IPv6 address on the MWAN↔OPNsense link.

**Requirement on OPNsense (MWAN-facing WAN interface):**

- **IPv6 address**: `3d06:bad:b01:fe::2/64`
- **IPv6 gateway**: MWAN’s link-local on the MWAN link (e.g. `fe80::...`)

Caution (don’t break replies):

- Ensure MWAN’s SNAT rules do not override conntrack’s reverse-NAT for hairpinned return traffic.
- Because inbound flows are fwmarked by ingress WAN, MWAN’s policy routing tables must include an explicit route for the DNAT target (`3d06:bad:b01:fe::2/128`) via the MWAN↔OPNsense link.

### Nuances / gotchas (debugging IPv6)

- **OPNsense WAN is link-local-only (by design)**:
  - OPNsense can be link-local-only for normal NPT routing.
  - Any MWAN routes “back toward LAN” must use the **OPNsense WAN link-local as next-hop**.

- **The internal `/60` is not on-link from MWAN’s perspective**:
  - If MWAN has `3d06:bad:b01::/60 dev enmwanbr0` in table 100/200, MWAN will attempt neighbor discovery for each internal host on the OPNsense↔MWAN link.
  - Correct shape is:
    - `3d06:bad:b01::/60 via <opnsense_wan_linklocal> dev enmwanbr0` in **table 100**, **table 200**, and **main**.

- **IPv6 default routes may be multipath**:
  - Gateway discovery must handle both single-path and multipath formats.

## IPv4 (OPNsense NATs downstream → MWAN load-balances + maps to public /29s)

For IPv4, **OPNsense only “sees” the MWAN internal link** (`10.250.250.0/29`). OPNsense is responsible for NATing all downstream RFC1918 networks into that internal /29. MWAN then:

- **Load-balances flows** across AT&T vs Webpass (mark 1 / mark 2)
- **Maps the internal /29 to each WAN’s public /29** (dual 1:1 mapping: one external mapping per WAN)

### Flow examples

#### Example 1: VLAN100 client → OPNsense SNAT to `10.250.250.2` → MWAN chooses AT&T → Internet

1. Client on VLAN100 (e.g. `10.250.1.45`) sends traffic to OPNsense.
2. OPNsense SNATs source to an internal MWAN address (e.g. `10.250.250.2`) and forwards it to MWAN (`10.250.250.1`).
3. MWAN marks the new flow **mark=1** (AT&T) and policy-routes it out AT&T.
4. MWAN maps `10.250.250.2` to the AT&T public /29 (e.g. `10.250.250.2` → `104.57.226.193`) and sends it out AT&T.
5. Return traffic to `104.57.226.193` arrives on AT&T at MWAN, is mapped back to `10.250.250.2`, then forwarded to OPNsense.

#### Example 2: VLAN100 client → OPNsense SNAT to `10.250.250.2` → MWAN chooses Webpass → Internet

Same as above, except MWAN marks the new flow **mark=2** (Webpass) and maps `10.250.250.2` to the Webpass public /29 (e.g. `10.250.250.2` → `136.25.91.242`).

### Inbound IPv4 (hosting services)

Inbound traffic to the public /29s must be **DNAT’d on MWAN** back to the internal /29 so OPNsense can see it.

- Example: external host pings `136.25.91.242`
  - Packet arrives at MWAN on `enwebpass0` with `dst=136.25.91.242`
  - MWAN **DNATs** it to `dst=10.250.250.2` and forwards it to OPNsense over `enmwanbr0`

MWAN also marks **inbound NEW flows** based on ingress WAN to keep replies symmetric:

- `iif enatt0.3242` → **mark=1** (AT&T)
- `iif enwebpass0` → **mark=2** (Webpass)

### Nuances / gotchas (debugging IPv4)

- **Do not override IPv4 fwmarks in `inet mangle prerouting` for `10.250.250.2-10.250.250.6`**.
  - IPv4 load balancing is driven by per-flow random fwmark assignment for *new* connections.
  - Setting `meta mark` by `ip saddr 10.250.250.x` pins that host to one WAN.

## NPT rule persistence (why `ip6 nat` chains can be empty)

The IPv6 NPT/DNPT rules live in `table ip6 nat` and are **programmed at runtime** by `update-npt.sh` (not baked into the static `nftables.conf`).

So an empty `table ip6 nat` is not “healthy” — it means runtime programming didn’t happen (or was flushed after it happened).

Common reasons:

- **Deploy ordering / reloads**: `nftables` reload flushes the ruleset. If WANs were already “routable”, `networkd-dispatcher` may not fire again (no state change), so NPT isn’t re-applied automatically.
- **Boot races (why they happen at all)**: at boot, multiple things are happening asynchronously and systemd starts many units in parallel. The exact timing varies run-to-run:
  - PCI/virtio devices can appear slightly later (driver load timing).
  - AT&T needs 802.1X before DHCPv6-PD on the VLAN will succeed (so PD can be “late”).
  - `networkd-dispatcher` and `nftables` are independent services; it is possible for a “routable” event to occur before `nftables` has loaded the base ruleset.
  - If `update-npt.sh` runs before the target interface exists *or* before the base `table ip6 nat` exists, it can fail/exit early (it uses `set -e` and calls `nft`).

Two different “empty” symptoms to distinguish:

- **`nft list ruleset` is empty / tiny**: `nftables` didn’t load successfully. Our config begins with `flush ruleset`, so a load error after that can leave you with very few/no rules.
- **Only `table ip6 nat` is empty**: base rules loaded, but the runtime NPT programming (via `update-npt.sh`) didn’t happen or was flushed after it happened.

Manual recovery (no guessing), on MWAN:

```bash
/usr/local/bin/update-npt.sh enatt0.3242 2600:1700:2f71:c80::/60
/usr/local/bin/update-npt.sh enwebpass0 2604:5500:c271:be00::/60
```

How to ensure it runs on deploy and reboot:

- **Deploy**: the playbook reloads `nftables` and then re-applies NPT rules.
- **Boot**: enable and run `mwan-update-npt.service` (oneshot) so NPT rules are applied even if hooks don’t fire in the right order.

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
```

## OPNsense Migration

### Phase 1: Parallel Testing

1. Add vNIC to OPNsense on vmbr_mwan
2. Configure new interface:
   - IPv4: `10.250.250.2/29`
   - IPv6: `3d06:bad:b01:fe::2/64` with MWAN link-local as gateway
   - Gateway: `10.250.250.1` (IPv4), MWAN link-local (IPv6)
3. Test connectivity through mwan gateway

### Phase 2: Cutover

1. Change default gateway to MWAN (`10.250.250.1`)
2. Verify traffic flows through load balancer
3. Remove old multi-WAN config:
   - Delete gateway groups
   - Delete NPT rules
   - Remove AT&T and Webpass interfaces

## Operations

### Verify services (on MWAN)

```bash
ssh root@mwan.home.goodkind.io
wpa_cli status
systemctl status wpa_supplicant-mwan dhcpcd nftables mwan-health
/usr/local/bin/health-check.sh --status
```

### Quick IPv6 sanity checks

```bash
ip -6 route show table 100
ip -6 route show table 200
ip -6 rule show
nft -a list chain ip6 nat postrouting
nft -a list chain ip6 nat prerouting
```

## Testing

### Core checklist

1. Verify VF trust mode persists after Proxmox host reboot
2. Verify AT&T 802.1X authentication succeeds
3. Verify Webpass DHCP works with the real NIC MAC
4. Test WAN failover between AT&T and Webpass
5. Verify NPT and 1:1 NAT mappings work correctly

### Test plan (what “working” looks like)

Outbound (from a downstream LAN host):

```bash
watch -n 1 'curl -4 -s ifconfig.co; echo'
watch -n 1 'curl -6 -s ifconfig.co; echo'
```

External inbound IPv4 (from a different network):

```bash
ping -c 3 136.25.91.242
nc -vz 136.25.91.242 443
```

Where to observe:

```bash
tcpdump -ni enwebpass0 host 136.25.91.242
tcpdump -ni enmwanbr0 host 10.250.250.2
```

External inbound IPv6:

- Prefer a TCP check (`nc -vz <v6> 443`), since some providers block ICMPv6 to CPEs.
- For “works everywhere” inbound targets, use the PD `::1` addresses:
  - AT&T `2600:1700:2f71:c80::1`
  - Webpass `2604:5500:c271:be00::1`

Hairpin validation to WAN interface /128:

- Webpass interface /128: `2604:5500:c271:8000::72b`
- AT&T interface /128: `2001:506:72f7:108c::1` (**often blocked; you may see zero packets on MWAN**)

## Troubleshooting

### AT&T 802.1X not authenticating

- On Proxmox host, verify VF trust mode: `ssh root@vault "ip link show enp2s0f0np0"`
  - Must show: `vf 0 ... spoof checking off, trust on`
- Check logs: `journalctl -u wpa_supplicant-mwan -f`
- Verify certs: `ls -la /etc/wpa_supplicant/*.pem`
- Verify VLAN 3242 exists: `ip link show | grep 3242`

### Webpass not getting DHCP

- With full PCI passthrough, VM uses the real NIC MAC (no spoofing)
- Verify NIC is assigned: `lspci | grep I226`

## Future Enhancements

- **Phase 3**: Add Monkeybrains as failover WAN
- **Phase 4**: Dynamic DNS for Monkeybrains public IPv4
- **Go Rewrite**: Single binary orchestrator for better state management
