# Pinning a device or app to one WAN via DSCP

Status: design plus manual PoC. Not deployed. Captured 2026-06-15.

## Why this exists

Hulu shows a "location changed" notification when its flows load-balance across
WANs, because each WAN egresses a different ISP IP (AT&T `104.57.226.x`, Webpass
`136.25.91.x`, Monkeybrains `158.247.70.x`) and Hulu reads the account as hopping
networks. The first fix was destination-IP pinning via the `att_pinned_v4/v6`
nftables sets (see [overview.md](overview.md) and
`ansible/inventory/group_vars/mwan_servers.yml`). A live capture on 2026-06-15
showed that approach loses to Hulu's CDN: Hulu sprays video segments across
Akamai, Fastly, and CloudFront `/24`s (and v6 `/64`s) faster than the 6h refresh
can track, so new rotated IPs keep leaking to the load-balanced WAN.

The durable fix is to stop classifying by destination and instead carry a
classification signal from OPNsense (which sees the device and the DNS) across
the routed hop to mwan, then pin on that signal. DSCP is the one piece of
metadata that survives a plain routed hop.

## What mwan can and cannot see (verified live)

Checked against `/proc/net/nf_conntrack` and `conntrack -L` on the mwan VM:

- IPv4: mwan sees `src=10.250.250.2` for every LAN flow. OPNsense source-NATs all
  IPv4 LAN clients to its `10.250.250.2` edge before handing traffic to mwan
  (consistent with the masquerade rule matching only `ip saddr 10.250.250.0/29`
  in `mwan/config/nftables.conf.j2`). So mwan cannot tell v4 devices apart by
  source, and a v4 source pin at mwan would pin all LAN v4 traffic.
- IPv6: mwan sees the real client `/128` (e.g. `3d06:bad:b01:2:9121:f8:be06:8f4a`)
  because the internal `3d06:bad:b01::/60` is routed end to end with no NAT. A v6
  source pin at mwan works, though SLAAC privacy addresses rotate, so pin a
  device `/64` (its own VLAN) rather than a single `/128`.

This split is why OPNsense-side tagging is required for v4: OPNsense is the only
node that sees the real `10.250.2.x` client.

## How to set DSCP on OPNsense (verified in source)

OPNsense can only SET DSCP through legacy scrub/normalization rules:

- `opnsense-core-25.7/src/etc/inc/filter.inc:534` emits `set-tos <value>` from
  `$config['filter']['scrub']['rule']`. Live on 26.1.9 the same line is
  `filter.inc:555`, and the GUI field is `firewall_scrub_edit.php:580`
  ("TOS / DSCP").
- Allowed values come from `get_tos_values()` (filter.inc:781-807): named
  (`lowdelay`, `ef`, ...), `af11`-`af43`, `cs0`-`cs7`, and raw hex `0x00`-`0xFF`.
- The MVC firewall filter-rule `tos` field (`Filter.xml`, `TosField.php`) and the
  Traffic Shaper `dscp` field are MATCH-only, not set. There is no ipfw
  `setdscp`. So scrub `set-tos` is the only set primitive.

Scrub runs before OPNsense's outbound NAT, so a scrub rule can match the real
v4/v6 client source, stamp a codepoint, and the bits ride through the NAT to
mwan.

## Automation reach (verified in source)

- The `opnsense-go` REST client cannot do this: its `Filter` struct has no
  `tos`/`dscp` field (`opnsense-go/pkg/firewall/filter.go:21-39`), and it
  implements no traffic-shaper or scrub resource.
- The configs stack does not use that REST client. It drives OPNsense through the
  `mwan-opnsense` gRPC daemon over virtio-serial, which mutates `config.xml` via
  `XPathSet`/`XPathGet`/`XPathDelete` plus `Exec`
  (`mwan/go/internal/opnsense/rpc_typed.go:71-170`, `docs/mwan/overview.md`).
  Scrub rules live at `config.xml` `filter/scrub/rule`, so that daemon can
  program the tag with `XPathSet` and a filter reload. It currently only touches
  BGP, gateways, and upgrades, so this would be a new use of an existing
  mechanism.

## Design

Chosen codepoint: CS1 (DSCP 8, ToS byte `0x20`), a scavenger-class value nothing
else here uses. nft accepts `ip dscp cs1`; OPNsense scrub stores `set-tos` as
`cs1`.

### OPNsense half (scrub set-tos)

One Normalization rule per family (or one rule with an alias holding both
addresses): match the streaming device source, set TOS/DSCP to `cs1`. Config
shape under `config.xml` `filter/scrub/rule`:

```
interface = lan
proto     = any
src       = <streaming-device-ip-or-alias>
dst       = any
set-tos   = cs1
descr     = Tag streaming device for AT&T pin
```

### mwan half (nft ip dscp)

In `mwan/config/nftables.conf.j2`, `table inet mangle` / `chain prerouting`,
right after the existing `@att_pinned` rules (currently lines 207-208) and before
the v6 numgen load-balancer and the `ct state established,related` restore:

```
# Pin DSCP-tagged app traffic (set by OPNsense scrub set-tos) to AT&T (mark 1).
ip dscp cs1 meta mark set 1
ip6 dscp cs1 meta mark set 1
```

This works for both families because the inet mangle chain (priority mangle)
runs before the IPv4 numgen load-balancer in `table ip nat` (priority dstnat),
so the mark is set before the `meta mark 0` guard there skips it. The existing
postrouting `ct mark set meta mark` and the prerouting `ct state
established,related meta mark set ct mark` carry the mark across the rest of the
flow, identical to how `att_pinned` behaves today.

## GUI-guided PoC (manual, about 10 minutes, no code deploy)

This proves the path end to end before any Ansible or daemon work.

1. Identify the streaming device LAN addresses. On the mwan VM during playback,
   the device's v6 appears as the conntrack source for Hulu flows (for example
   `3d06:bad:b01:2:9121:f8:be06:8f4a`); its v4 is its DHCP lease on OPNsense.
2. OPNsense, optional alias: Firewall > Aliases > Add, type Host(s), name
   `streaming_device`, add the device v4 and v6. Apply.
3. OPNsense scrub rule: Firewall > Settings > Normalization > Add.
   - Interface: the LAN the device is on.
   - Protocol: any.
   - Source: `streaming_device` (or the device IP). Leave invert off.
   - Destination: any.
   - TOS / DSCP: `cs1`.
   - Description: `Tag streaming device for AT&T pin`.
   - Save, then Apply changes.
4. mwan, live nft rules (PoC only, reverted on next nft reload):
   ```
   nft add rule inet mangle prerouting ip dscp cs1 ct state new meta mark set 1
   nft add rule inet mangle prerouting ip6 dscp cs1 ct state new meta mark set 1
   ```
   These append to the chain. `ct state new` marks fresh flows; established flows
   are restored from ct mark by the existing rule, so append order is safe for a
   PoC. The permanent placement is after the `@att_pinned` rules as shown above.
5. Restart Hulu on the device (so flows are new), then watch egress on the mwan
   VM. A simple watcher over `conntrack -L` filtered to Hulu destinations should
   show `mark=1` regardless of which CDN IP Hulu rotates to, since the pin now
   keys on DSCP, not destination.

If it works, promote it: add the two nft lines to `nftables.conf.j2`, and either
keep the scrub rule in OPNsense config or program it from `mwan-opnsense` via
`XPathSet` into `filter/scrub/rule`.

## Verification

- Confirm the tag survives to mwan: on the mwan VM, `conntrack -L` Hulu flows
  show `mark=1`; before the change they split between `mark=1` and `mark=2`.
- Confirm the device egresses AT&T: its public IP as seen by an IP-echo service
  while streaming should be in the AT&T `104.57.226.x` range.
- Confirm no collateral: only the tagged device should change WAN; other LAN
  traffic still load-balances.

## Caveats

- It is config.xml, not the REST API. Automating it means the `mwan-opnsense`
  XPath path, which has not yet been used for firewall rules; verify the daemon
  can insert a rule subtree, not just scalar values, before relying on it.
- Verify nothing else re-normalizes ToS to 0 between OPNsense and mwan (a quick
  live capture settles this).
- Pick a codepoint not otherwise in use. CS1 (`0x20`) is the assumed free value
  here; confirm no Traffic Shaper or other consumer keys on it.
- This pins the whole device, which is the intended "blunt" behavior. For
  app-only granularity on a shared device, OPNsense would still need a per-app
  classifier, which inherits the same shared-CDN problem.
