# Config-driven WAN

One mwan binary handles any WAN set. Adding, removing, re-tiering, or
re-weighting an ISP is an inventory edit plus a config deploy and service
restart. No ISP name appears in Go code, and no per-ISP scalar variable or
per-ISP template file exists. Changes apply at daemon restart; there is no
hot reload.

## Design contract

1. **One inventory source per environment.** A `mwan_wans` map in the
   environment's group_vars carries every per-WAN fact. Each entry holds
   identity (`index`, `iface`), steering (`tier`, `weight`), link bring-up
   flags, health parameters, static IPv4 NAT mappings, pinned-destination
   CIDRs, and an optional static delegated prefix. A global `mwan_lb_hash`
   key beside the map selects the load-balance hash mode.

2. **Identifiers derive from a stable index.** Each WAN declares one small
   integer `index`, never reused, never dependent on map order. From index
   i the system derives: routing table 100*i, firewall mark i, mark-rule
   priority 100*i, from-rule priority 54+i. Operators never write a table
   id, mark, or rule priority. Validation enforces unique names and
   indexes and rejects any derived identifier that collides with the
   reserved set: tables 900 (cloudflared), 253, 254, 255. The current
   WANs are att index 1, webpass index 2, monkeybrains index 3, which
   reproduces every previously hand-assigned value exactly.

3. **The daemon is the firewall authority.** A `firewall` ifmgr module
   builds the complete ruleset from config at startup and programs it into
   the kernel atomically over netlink via github.com/google/nftables, the
   same ownership pattern the npt module uses for `table ip6 nat`. The
   rendered `/etc/nftables.conf` is a minimal WAN-free bootstrap: input
   drop with established, loopback, ICMP, management SSH, and management
   gRPC accepted. `nftables.service` loads the bootstrap at boot so the
   box is closed and reachable before the daemon starts; the daemon's
   ruleset replaces it. An nft watcher re-applies the daemon's tables
   within seconds when anything external flushes them.

4. **Steering is tier plus weight plus hash mode.** Lower tier is
   preferred. The active tier is the lowest-numbered tier with at least
   one healthy WAN. New connections from internal sources receive a
   firewall mark computed over the tier-1 members: a numgen expression
   modulo the sum of tier-1 weights, mapped onto member marks with one
   slot per weight unit. `mwan_lb_hash` selects the expression: `random`
   (per-connection random), `source` (hash of source address), or
   `source_dest` (hash of source and destination). Health prunes an
   unhealthy WAN's ip rules and marked strays fall through to the main
   table. When a tier above 1 is active, wan.routes emits the priority-50
   catch-all rules to the healthy member of that tier with the lowest
   index. A fallback tier serves through one member at a time; mark
   assignment is computed from tier 1 at startup, so load balancing
   within an activated fallback tier is out of scope.

5. **Delegated prefixes are live by default.** The pd.Source live
   DHCPv6-PD reading is authoritative for each WAN's delegated prefix.
   The npt module and the wan.routes IPv6 source-pin rule both consume
   the live value. A WAN with a static delegation sets `prefix_v6`, which
   overrides the live reading. No WAN sets it today.

6. **Every link renders through one path.** Each entry's `link` block
   exposes independent knobs: `dhcp4`, `dhcp6`, `pd` with `pd_hint`,
   `slaac`, `route_metric`, static `addr4`/`gw4` and `addr6`/`gw6`, DUID
   fields, an optional `vlan` block (`parent`, `id`), and an optional
   `dot1x` block (supplicant identity, certificate paths, and the parent
   port's helper address). One generic networkd template renders exactly
   the sections a WAN's fields enable, and the sysctl file loops over the
   same map. A WAN differs from its peers only in data, never in which
   code path builds it; no entry is exempt from the renderer. systemd
   networkd is the single link authority for every WAN, which keeps link
   creation and DHCP under one owner and keeps the delegated-prefix lease
   that pd.Source reads where the daemon already finds it.

   A `vlan` block makes the WAN a tagged child: the renderer emits the
   netdev, the parent's VLAN declaration, and the child's own network
   file from the same flags any other WAN uses. A `dot1x` block adds the
   supplicant on the parent port and gates the child's DHCP start on
   authentication succeeding. Production att carries both blocks;
   testbed att carries neither and is otherwise the same entry.

7. **The watchdog probes every WAN.** The rollback watchdog pings out of
   each configured WAN interface to tell a real outage apart from a
   routing failure, and it returns healthy on the first interface that
   answers, so the probe list is a logical OR and an added member can
   only reduce false rollbacks. Every WAN in the map is probed. This
   closes a live gap: att is absent from the current probe list, so a VM
   reachable only through att reads as a total outage.

8. **Failure is closed and inert.** A daemon start with invalid config
   (duplicate index, empty tier set, malformed CIDR, derived-identifier
   collision) fails validation before programming anything, and the
   existing kernel ruleset stays. A daemon stop or crash leaves the
   last-programmed rules serving traffic. A boot where the daemon never
   starts leaves the bootstrap: no forwarding, no NAT, management access
   intact.

## Inventory schema

```yaml
mwan_lb_hash: random        # random | source | source_dest
mwan_wg_pin_wan: att        # WAN carrying the WireGuard control-plane pin

mwan_wans:
  att:
    index: 1
    iface: "enatt0.3242"    # steering/health/npt iface (the VLAN child)
    tier: 1
    weight: 1
    link:
      vlan:
        parent: "enatt0"
        id: 3242
      dot1x:                # supplicant on the parent port
        identity: "..."
        cert: "..."
        key: "..."
        parent_addr4: "192.168.1.2/24"   # SFP module management access
        parent_peer4: "192.168.1.1"
      dhcp4: true
      dhcp6: true
      pd: true
      pd_hint: "::/60"
      slaac: true
      duid_type: "vendor"
      duid_raw: "..."
    health: { ... }         # same shape as the per-WAN health block today
    static_mappings:        # 1:1 IPv4 NAT pairs
      - { external: "x.x.x.x", internal: "10.250.250.x" }
    pinned_v4_cidrs: []     # destinations pinned to this WAN
    pinned_v6_cidrs: []
  webpass:
    index: 2
    iface: "enwebpass0"
    tier: 1
    weight: 1
    link:
      addr4: "x.x.x.x/29"
      gw4: "x.x.x.x"
      dhcp6: true
      pd: true
      pd_hint: "::/56"
      slaac: true
      duid_type: "link-layer-time"
      duid_raw: "..."
    health: { ... }
    static_mappings: [ ... ]
  monkeybrains:
    index: 3
    iface: "enmbrains0"
    tier: 2
    weight: 1
    link:
      dhcp4: true
      dhcp6: true
      pd: true
      pd_hint: "::/56"
      slaac: true
      route_metric: 5000
      duid_type: "link-layer-time"
      duid_raw: "..."
    health: { ... }
```

The map replaces these scalars, which are deleted: `mwan_att_iface`,
`mwan_webpass_iface`, `mwan_monkeybrains_iface`, `mwan_att_vlan_id`,
`mwan_npt_*_prefix`, `mwan_rt_tables` WAN entries,
`mwan_ifmgr_wan_fw_marks`, `mwan_ifmgr_wan_fw_mark_prios`,
`mwan_ifmgr_wan_from_prios`, `mwan_health_checks`,
`mwan_static_mappings`, `mwan_att_pinned_v4_seed_cidrs`,
`mwan_att_pinned_v6_seed_cidrs`, and the per-WAN DUID, address, and
gateway scalars.

## Config pipeline

`config-vm.toml.j2` renders one `[ifmgr.wan.<name>]` block per map entry
carrying `index`, `iface`, `tier`, `weight`, `prefix_v6` when set, the
pinned CIDR lists, and the static mappings. `table_id`, `fw_mark`,
`fw_mark_prio`, `from_prio`, and `npt_prefix` leave the rendered config;
Go derives the first four from `index` and reads the delegated prefix
live. A `[ifmgr.lb]` block carries the hash mode. The health loop, the
`[[network.wan_interfaces]]` watchdog probe list, `rt_tables.j2`, and
`sysctl-mwan.conf.j2` render one entry per map entry. `nftables.conf.j2`
is replaced by the bootstrap template.

Link rendering is one template set driven by the same map: a network
file per WAN, a netdev per WAN carrying a `vlan` block, a parent network
file and supplicant configuration per WAN carrying a `dot1x` block, and
the auth-gated DHCP start unit for those same WANs. Every per-ISP
networkd file is deleted, including both AT&T files, the webpass and
monkeybrains files, and the separate testbed forks of att and webpass.
The testbed and production difference becomes data in the map.

## Firewall module ruleset

From config, the module programs:

- `inet filter`: input drop with the fixed accepts (established,
  loopback, ICMP, management SSH, management gRPC, internal BGP and BFD)
  plus DHCP and DHCPv6 accepts per WAN; forward drop with internal-to-WAN
  and WAN-to-internal accepts generated from the WAN list; rate-limited
  drop logging.
- `ip nat`: per-WAN 1:1 DNAT and SNAT from `static_mappings`; per-WAN
  mark-scoped masquerade; the IPv4 steering mark rules.
- `inet mangle`: per-WAN ingress marks (mark = index) for reply symmetry;
  pinned-destination sets for each WAN that declares pinned CIDRs; the
  WireGuard control-plane pin, whose target WAN is named by the global
  `mwan_wg_pin_wan` key; the IPv6 steering mark rules; conntrack mark
  save and restore.

The npt module keeps sole ownership of `table ip6 nat`.

## Go surface changes

- `wanroutes` drops the ISP name constants, the named fallback function,
  and the exact-set priority validators; tier activation and derivation
  validation replace them.
- The `firewall` module is new, structured like npt: a desired-state
  builder, an atomic applier, an nft watcher, and a
  `mwan debug firewall` renderer that prints the desired ruleset for
  parity checks against `nft list`.
- `debug` probe commands iterate the configured WAN set ordered by index;
  the default probe interface is the lowest-index WAN.

## Validation

Runs on the suburban testbed, in order, with the binary unchanged
throughout:

1. **Byte equivalence.** Deploy the migrated inventory with today's WAN
   set. `mwan debug firewall` output matches the live ruleset, and rules,
   routes, marks, and steering match the pre-migration state
   one-for-one. The rendered link files match the hand-authored ones they
   replace, section for section, including the tagged-child case.
2. **Add.** Add a synthetic fourth WAN backed by a testbed ISP simulator
   at tier 1 weight 1 by inventory edit and config deploy. Three-way
   balancing distributes new connections; failing the new WAN's health
   prunes it; recovery restores it.
3. **Re-tier.** Move the fourth WAN to tier 2. It carries no traffic
   while tier 1 has a healthy member; failing all of tier 1 activates it.
4. **Remove.** Delete the entry and deploy. The ruleset, rules, routes,
   and tables converge to the state recorded in step 1.

## Out of scope

- Hot reload: config applies at daemon restart only.
- Quality-based steering (latency, jitter, loss SLA selection).
- Load balancing within an activated fallback tier.
- The daemon running its own DHCPv6-PD client (MWAN-227) and dynamic
  pinned-destination refresh (MWAN-237); both layer on top unchanged.
- Moving link creation from systemd networkd into the daemon. That step
  is gated on the daemon owning DHCP (MWAN-227), because splitting link
  creation from lease ownership would put one interface under two
  authorities. When it happens it moves every WAN at once, since the
  schema already describes them identically.
