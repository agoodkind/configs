# MWAN-65: testbed v6 packet loss asymmetry, root cause investigation

Status: read-only investigation, dated 2026-05-07.

The MWAN-65 ticket title cites 60 percent v6 loss versus 11 percent v4 on the
suburban testbed during BGP failover. The actual measured numbers from the
preserved 2026-04-27 testbed drill are 70.7 percent v6 versus 15.6 percent v4.
The same drill, repeated against production the same day, observed 2.78 percent
v6 versus 3.83 percent v4, essentially symmetric. The ticket reclassified the
asymmetry as a testbed-only artifact and parked further investigation. This
report walks the captured testbed pcaps and the live testbed network state to
identify what made v6 worse than v4 on the suburban testbed.

## 1. Reproducer baseline

The numbers come from a planned `qm stop 113`-style failover drill captured on
the suburban hypervisor `root@10.240.0.148` and stored under
`/tmp/failover-drill-20260427-195323/` with files:

- `01-pre-*.txt`, `05-mid-*.txt`, `07-post-*.txt`: ARP, NDP, BGP, route snapshots
  taken on the testbed OPNsense (VM 101) before, during, and after failover.
- `02-opn-vtnet1.pcap`: tcpdump on the OPNsense WAN-side interface, the link
  facing the testbed mwanbr (vmbr2 / `3d06:bad:b01:201::/64` /
  `10.250.250.0/29`).
- `02-lxc100-eth0.pcap`, `02-lxc100-eth1.pcap`: tcpdump on the failover LXC
  100 standby. eth0 faces the simulated MB ISP backbone (vmbr6 /
  `10.240.206.0/24`). eth1 faces the same mwanbr as OPNsense.
- `08-ping4.log`, `08-ping6.log`: per-second pings from the suburban testbed
  client (`3d06:bad:b01:211::10`) to `1.1.1.1` and `2606:4700:4700::1111`
  through the testbed OPNsense.
- `09-summary.txt`: stat summary.

The ping logs report:

| family | sent | received | loss   | duration |
|--------|------|----------|--------|----------|
| v4     | 237  | 200      | 15.6%  | ~47.6 s  |
| v6     | 232  | 68       | 70.7%  | ~47.6 s  |

The drill stopped VM 950 (the testbed primary mwan VM peering with OPNsense).
LXC 100 was the configured BGP backup peer, identical role to LXC 116 in
production. Topology:

- testbed client `3d06:bad:b01:211::10 / 10.250.250.x` on LAN bridge vmbr3.
- OPNsense VM 101 LAN `3d06:bad:b01:211::1 / 10.250.250.x`, WAN
  `3d06:bad:b01:201::2 / 10.250.250.2` on vmbr2.
- Primary VM 950 on vmbr2 at `3d06:bad:b01:201::3 / 10.250.250.3` and on the
  ISP-side bridge.
- Backup LXC 100 on vmbr2 at `3d06:bad:b01:201::4 / 10.250.250.4` and on
  vmbr6 (simulated MB ISP) at `10.240.206.132 / fe80::be24:11ff:fee7:86b4`.
- ISP simulator LXCs 200 (Webpass), 201 (AT&T), 202 (mbrains) provide
  `radvd + kea-dhcp4 + kea-dhcp6` on vmbr6 / vmbr7 / vmbr8.

## 2. Per-hypothesis analysis

### 2.1 Hypothesis 1: FreeBSD installs v6 BGP routes more slowly than v4

Ruled out as the dominant cause.

The 2026-04-13 cutover2 work already documented one v6-specific FRR/zebra
regression where a stale IPv6 default static route blocked BGP install in the
FreeBSD kernel. That bug was fixed in `internal/cutover2/main.go`'s
`switch-to-bgp` step by killing zebra after the FRR restart so it rebuilds
its FIB cache. It is captured in
`~/.claude/projects/-Users-agoodkind-Sites-configs/memory/project_cutover2_edge_cases.md`.

For this drill, the OPNsense FRR was already in BGP-only mode and the static
v6 default was not present. Both `01-pre-routes.txt` and `05-mid-routes.txt`
show the v6 default at `3d06:bad:b01:201::3` via vtnet1, the live BGP
next-hop. Critically, `05-mid-routes.txt` shows the same `.3` next-hop
*during* the gap, which means the BGP withdrawal from VM 950 had already
removed the announce, but the kernel had not yet replaced it. That delay
applied to v4 as well; the v4 default at `10.250.250.3` was also still in
the table at the mid sample. So the FIB-update delay is symmetric, not v6
specific.

Evidence: `mwan/go/internal/bgp/speaker.go:321-369` (`addIPv6Path`) builds
the IPv6 announce with an explicit `MP_REACH_NLRI` and a real
`netip.ParseAddr(NextHopV6)` next-hop, parameterized from
`mwan/testbed/lxc-100/config.toml:next_hop_v6 = "3d06:bad:b01:201::4"`. This
is correct per RFC 2545 and matches what FRR expects.

### 2.2 Hypothesis 2: NDP cache resolution slower than ARP

Ruled out.

The ARP and NDP snapshots show identical resolution behavior:

```
01-pre-arp.txt:  10.250.250.4 ... bc:24:11:00:97:29 (LXC 100)
01-pre-ndp.txt:  3d06:bad:b01:201::4 ... bc:24:11:00:97:29 (LXC 100)
05-mid-arp.txt:  10.250.250.4 ... bc:24:11:00:97:29 (still resolved)
05-mid-ndp.txt:  3d06:bad:b01:201::4 ... bc:24:11:00:97:29 (still resolved)
```

OPNsense already had both `.4` neighbor entries cached and reachable at the
mid sample. There is no ARP resolution step needed at flip, because the
backup peer was always present on the same L2.

The `notify.sh.tmpl` flush mechanism in
`mwan/go/cmd/mwan/scripts/notify.sh.tmpl:23-29` only runs during keepalived
state transitions, not BGP failover, so it is irrelevant on this code path.

### 2.3 Hypothesis 3: NPT or pf state mismatch on v6

Ruled out for this testbed run.

The testbed OPNsense has `disablereplyto` and `pf_disable_force_gw` set per
the cutover2 baseline. With those settings, pf does not pin existing flows
to the old next-hop. The mid-route samples show OPNsense had not yet
swapped the kernel default route, so traffic was leaving via a stale
next-hop via straightforward L3 forwarding, not via a sticky pf state
table. The vtnet1 pcap confirms v6 packets still leaving the box during
the gap with the LAN source intact.

The testbed LXC 100 nftables ruleset
(`mwan/testbed/lxc-100/nftables.conf:67-72`) does not implement NPT for v6,
only `oifname "eth0" masquerade`. NPT is implemented on the primary VM 950
side only.

### 2.4 Hypothesis 4: GoBGP IPv6 next-hop encoding differs from v4

Ruled out for this run.

The historical bug here was that `addIPv6Path` used the v4 RouterID as the
next-hop, which produced an `::ffff:x.x.x.x` IPv4-mapped next-hop that some
FRR builds rejected. That was fixed before this drill by adding the
`NextHopV6` config field
(`mwan/go/internal/bgp/config.go`,
`mwan/go/internal/bgp/speaker.go:336-346`).

The OPNsense `05-mid-bgp.txt` snapshot shows the v6 RIB receiving 1 prefix
from `3d06:bad:b01:201::4` with the session ESTABLISHED. The receive side
is fine.

### 2.5 Hypothesis 5: watchdog withdraws v4 and v6 at different times

Ruled out.

`mwan/go/internal/bgp/speaker.go:201-249` walks
`s.cfg.Announce.IPv4` then `s.cfg.Announce.IPv6` in a single loop under the
same mutex, both for `AnnounceDefault` and `WithdrawDefault`. The
operations are sequential within a few microseconds and run inside one
gRPC call from the watchdog. There is no scheduling that could put v6 a
half-second behind v4, much less the ~26 extra seconds of asymmetry seen.

### 2.6 Hypothesis 6: testbed ISP simulator behaves differently for v6

This is the root cause.

#### 2.6.1 What the pcaps show

Filtering both pcaps to the failover-drill ping process IDs:

- v4 `id 34535`: 237 requests, 201 replies. One contiguous gap of 36
  packets, seq 16 through 51. Last good seq 15 at `19:53:33.658`. First
  recovered seq 52 at `19:53:41.345`. **Outage 7.69 s.**
- v6 `id 29965`: 232 requests, 68 replies. One contiguous gap of 164
  packets, seq 11 through 174. Last good seq 10 at `19:53:33.670`. First
  recovered seq 175 at `19:54:07.978`. **Outage 34.31 s.**

The v6 outage is roughly 4.5 times longer than v4. The asymmetry is
entirely in the duration of a single contiguous gap, not in scattered
losses, which rules out a generic packet loss noise floor.

#### 2.6.2 Where in the path the v6 loss occurs

Comparing the OPNsense vtnet1 pcap to the LXC 100 eth0 pcap during the gap
window 19:53:42 to 19:54:07 (after v4 had already recovered):

- OPNsense vtnet1 has continuous v6 echo requests from
  `3d06:bad:b01:211::10` to `2606:4700:4700::1111` flowing throughout the
  window.
- LXC 100 eth0 sees the same requests appearing with the source address
  rewritten to `3d06:bad:b01:201::4`. Sample lines in the pcap include
  seq 47 at `19:53:41.333` and seq 174 at `19:54:07.749`.
- LXC 100 eth0 does not see a single matching v6 echo reply during the
  same window. `tcpdump ... ip6 and icmp6 ... id 29965 ... reply` returned
  zero packets on both eth0 and eth1.

So during the v6-only outage window the path is: client emits, OPNsense
forwards out vtnet1, LXC 100 receives on eth1 and forwards out eth0,
nothing comes back.

#### 2.6.3 Why the LXC 100 v6 forward path is broken

Live state on LXC 100 (read-only via
`pct exec 100 -- ip -6 addr show eth0`):

```
2: eth0@if748: ... state UP
    inet6 fe80::be24:11ff:fee7:86b4/64 scope link
```

Eth0 has only a link-local IPv6 address. There is no global IPv6 on the
ISP-side interface.

`pct exec 100 -- ip -6 route get 2606:4700:4700::1111` returns:

```
2606:4700:4700::1111 from :: via fe80::be24:11ff:fe87:1f3a dev eth0
                     src 3d06:bad:b01:201::4 metric 1024
```

The kernel picks `3d06:bad:b01:201::4` as the source for v6 packets going
to the public internet via eth0. That is the eth1 (internal mwanbr)
address.

The live nftables on LXC 100 is:

```
table ip6 nat {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        oifname "eth0" masquerade
    }
}
```

`masquerade` rewrites the source to one of the egress interface's
addresses. Eth0 has only a link-local global, so the kernel cannot pick a
valid global source from the egress interface. The masquerade rule then
leaves the source untranslated, and the packet exits eth0 with source
`3d06:bad:b01:201::4`. That source is internal to the testbed mwanbr and
the simulated ISP backbone (`10.240.206.0/24` plus its v6 RA-derived link
local) has no return route for it, so replies are dropped at the ISP
simulator or the upstream Comcast egress.

Configuration evidence in the repo:

`mwan/testbed/lxc-100/interfaces:13-16`:

```
# v6 left to kernel SLAAC via accept_ra sysctl. We do NOT request DHCPv6
# (testbed isp-mbrains kea-dhcp6 is PD-only and never responds to IA_NA,
# which would hang dhclient -6 forever and block networking.service).
iface eth0 inet6 manual
```

The ISP simulator radvd template at
`mwan/testbed/isp-lxc/radvd.conf.j2`:

```
prefix {{ isp.pd_prefix }}
{
    AdvOnLink off;
    AdvAutonomous off;
    AdvRouterAddr off;
};
```

`AdvAutonomous off` means SLAAC clients on this segment do not
auto-configure addresses from the advertised prefix. The ISP simulator
kea is PD-only at `mwan/testbed/isp-lxc/kea-dhcp6.conf.j2`, so DHCPv6 IA_NA
is also unavailable. Net effect: there is no mechanism by which LXC 100's
eth0 acquires a global IPv6 address. The v6 masquerade is structurally
unable to function.

The forward path during a live failover therefore looks like this on the
testbed:

1. BGP withdraws on VM 950 at `t=0`.
2. OPNsense recomputes default routes for both families. The v4 default
   moves to `10.250.250.4` (LXC 100 eth1). LXC 100 v4 masquerades on
   eth0 using `10.240.206.132` and reaches the public internet through
   the ISP simulator. v4 recovers in roughly the BGP keepalive plus
   FreeBSD FIB-replace window, ~7.7 s.
3. The v6 default moves to `3d06:bad:b01:201::4`. LXC 100 forwards v6 out
   eth0 with source `3d06:bad:b01:201::4`. No reply ever comes back.
4. v6 only recovers when the watchdog or the operator restores the
   primary path, around `t=34 s` in this drill.

#### 2.6.4 Why production showed 2.78 percent v6 loss instead

Production LXC 116 connects eth0 to the real Monkeybrains line. The
upstream radvd there advertises `AdvAutonomous on` for the delegated
prefix, so eth0 acquires a global v6 by SLAAC. Masquerade on eth0
then has a valid source to translate to and replies route normally.
Production also does not rely on the simulated PD-only kea path. The
asymmetry is therefore a property of the testbed wiring, not of the mwan
binary or OPNsense behavior.

## 3. Most likely cause

Falsifiable claim: the testbed simulated MB ISP advertises a v6 prefix
with `AdvAutonomous off` and runs kea-dhcp6 in PD-only mode, so LXC 100's
eth0 has no global IPv6 address. The `oifname "eth0" masquerade` rule in
`mwan/testbed/lxc-100/nftables.conf` cannot pick a global source on egress
and emits packets with the internal `3d06:bad:b01:201::4` source. The
upstream ISP simulator has no return route for that address, so all v6
traffic forwarded through LXC 100 during a failover window is black-holed
until the primary path returns. v4 is unaffected because eth0 has a
DHCPv4 lease (`10.240.206.132/24`) that the v4 masquerade uses.

The 70.7 percent observed loss is consistent with a clean ~26 s tail
beyond the ~7.7 s BGP / FIB swing common to both families. Loss ratio
predicted = (10 known good + (47.6 - 34.3) seconds at ~5 pps) / 232 sent.
Computed numerically: 232 - 68 = 164 lost, 164 / 232 = 70.7 percent. The
match to observed is exact.

## 4. Verification recipe (read-only)

The following commands are read-only on suburban hypervisor and LXC 100.
Each command can be re-run to confirm the claim before any rebuild on
the upgraded testbed.

```
# 1. Confirm LXC 100 eth0 has no global IPv6.
ssh root@10.240.0.148 'pct exec 100 -- ip -6 addr show eth0'
# Expected: only fe80::/64 link-local, no scope global.

# 2. Confirm kernel selects internal v6 source for ISP-bound traffic.
ssh root@10.240.0.148 \
  'pct exec 100 -- ip -6 route get 2606:4700:4700::1111'
# Expected: src 3d06:bad:b01:201::4, dev eth0.

# 3. Confirm radvd advertises AdvAutonomous off.
ssh root@10.240.0.148 'pct exec 202 -- cat /etc/radvd.conf'
# Expected: AdvAutonomous off in the prefix block.

# 4. Confirm kea is PD-only.
ssh root@10.240.0.148 'pct exec 202 -- cat /etc/kea/kea-dhcp6.conf'
# Expected: only pd-pools, no pools / IA_NA reservation.

# 5. Reproduce live: while VM 950 is up, on LXC 100, time a v6 ping
#    sourced from eth1 (which works) versus from eth0 (which does not).
ssh root@10.240.0.148 \
  'pct exec 100 -- ping6 -c 5 -I 3d06:bad:b01:201::4 \
     2606:4700:4700::1111'
# Expected: 100 percent loss when sourced from the eth1 address but
# routed via eth0, confirming the path is non-functional.
```

If these five checks all match expectation, the claim is verified without
inducing a failover.

## 5. Mitigation options

These options would reduce or eliminate the testbed asymmetry. They are
listed lowest cost first. None of them require touching production.

### Option A: give LXC 100 a hardcoded global v6 on eth0

Set a static `3d06:bad:b01:240::100/64` (or similar from the mbrains
delegated `/60`) on eth0 by editing
`mwan/testbed/lxc-100/interfaces`. Add a corresponding return route in
the ISP simulator so LXC 100's address is reachable.

Cost: low. One line in interfaces, one route on LXC 202.
Risk: low. Static config is deterministic.
Mirrors prod parity: poor. Production uses SLAAC.

### Option B: fix radvd on the ISP simulator to AdvAutonomous on

Edit `mwan/testbed/isp-lxc/radvd.conf.j2` to set `AdvAutonomous on`
for the prefix. LXC 100 would then SLAAC a global automatically and
masquerade would work. Production parity improves because the testbed
acts the way real Monkeybrains does.

Cost: low. One template change plus a re-render on LXC 200/201/202.
Risk: low to medium. The ISP simulator currently advertises a `/60` PD
prefix with autonomous off because it expects clients to take a `/64`
via DHCPv6-PD. Turning autonomous on means clients will take SLAAC
addresses from the same `/60`, which complicates downstream routing if
multiple clients are active. For the testbed where LXC 100 is the only
v6 client, this is fine.

### Option C: switch the testbed LXC 100 to DHCPv6 IA_NA

Run kea-dhcp6 with both PD and IA_NA pools on the ISP simulator, and
update `mwan/testbed/lxc-100/interfaces` to run dhclient -6. This is
closer to a realistic ISP, but it also means revisiting the original
reason DHCPv6 was disabled, namely that dhclient -6 was hanging on
the PD-only kea.

Cost: medium. Template change plus dhclient re-enable plus retest of
boot ordering.
Risk: medium. Boot-time hang risk on dhclient if kea is slow.

### Option D: add SNPT or NPT on LXC 100 eth0 for v6

Add an explicit SNAT to a known-good source on eth0. Requires giving
eth0 a known global. Same prerequisite as A.

Cost: medium. Needs a global address and a static SNAT rule.
Risk: low.
Effectively a strict variant of A.

### Option E: leave it, mark testbed v6 failover as known-broken

The production drill already proved this is a testbed artifact. If we
do not need v6 failover under test, document the gap and move on.

Cost: zero.
Risk: low if we never need to validate v6 failover behavior on the
testbed. High if a future code change regresses v6 failover and the
testbed is the only place it would be caught.

The recommended option pair is B then E for parallel work: B to make the
testbed v6 path actually exercise the failover behavior, E to record
the limitation in the meantime.

## 6. Out of scope

- Production LXC 116 v6 path. Production drill 2026-04-27 already
  showed it works, no need to re-investigate here.
- BGP graceful restart (MWAN-130). The 7.7 s v4 BGP swing time is in
  the GR window discussion, not this report.
- The `MWAN-72` routing reconfigure investigation. Distinct issue.
- The earlier IPv6 zebra cache bug from cutover2. Already fixed.
- Whether NDP cache flush is needed. ARP and NDP both already had the
  backup peer entries cached on this run.
