# Three: the provider set becomes data

Adding, removing, re-tiering, or re-weighting a provider becomes an inventory
edit and a configuration deploy. No provider name appears in Go outside tests
or in a per-provider inventory variable. One binary serves any provider set.

Depends on the configuration format, so the inventory is written once in its
final shape.

## What blocks a fourth provider today

The daemon carries the names att, webpass, and monkeybrains as constants and
decides the fallback by comparing against one of them. Two validators accept
only the three rule priorities in use, 100, 200, 300 and 55, 56, 57, and a
failed validation stops the daemon. The load balancer is three fixed lines in
the firewall ruleset file, one for IPv4 and two for IPv6, each a coin flip
between marks 1 and 2, so a third or fourth member can never be selected. The
renderer for `network.json` lists the three providers by name inside the
template. Each of those four things goes away in this piece.

## Inventory takes the model's shape

Each gateway group carries one list with one entry per provider. Every value
the daemon reads about a provider sits in its entry, and the 34 variables that
carry a provider name in each gateway group today collapse into it.

Today, in the production group:

```yaml
mwan_att_iface: "enatt0"
mwan_webpass_iface: "enwebpass0"
mwan_monkeybrains_iface: "enmbrains0"

mwan_npt_att_prefix: "2600:1700:2f71:c80::/60"
mwan_npt_webpass_prefix: "2604:5500:c271:be00::/60"
mwan_npt_monkeybrains_prefix: "2607:f598:d3e8:4500::/60"

mwan_rt_tables:
  att: 100
  webpass: 200
  monkeybrains: 300
  cloudflared: 400
mwan_ifmgr_wan_fw_marks:
  att: 1
  webpass: 2
  monkeybrains: 3
mwan_ifmgr_wan_fw_mark_prios:
  att: 100
  webpass: 200
  monkeybrains: 300
mwan_ifmgr_wan_from_prios:
  att: 55
  webpass: 56
  monkeybrains: 57

mwan_health_checks:
  att: { enabled: true, ping_count: 3, ... }
  webpass: { ... }
  monkeybrains: { ... }

mwan_static_mappings:
  att: [ { internal: ..., external: ... }, ... ]
  webpass: [ ... ]
```

After this piece, the same group:

```yaml
mwan_providers:
  - name: att
    iface: enatt0
    vlan_id: 3242
    table: 100
    mark: 1
    mark_prio: 100
    from_prio: 55
    tier: 0
    weight: 1
    npt_prefix: "2600:1700:2f71:c80::/60"
    static_mappings:
      - { internal: "10.250.250.2", external: "104.57.226.193" }
      # ... four more
    health:
      enabled: true
      ping_count: 3
      success_threshold: 2
      failure_threshold: 2
      recovery_threshold: 2
      check_interval: 10
      targets_v4: ["1.1.1.1", "8.8.8.8"]
      targets_v6: ["2606:4700:4700::1111", "2001:4860:4860::8888"]
      http_targets: ["https://ifconfig.co/ip"]

  - name: webpass
    iface: enwebpass0
    table: 200
    mark: 2
    mark_prio: 200
    from_prio: 56
    tier: 0
    weight: 1
    v4_source: "136.25.91.242"
    npt_prefix: "2604:5500:c271:be00::/60"
    static_mappings: [ ... ]
    health: { ... }

  - name: monkeybrains
    iface: enmbrains0
    table: 300
    mark: 3
    mark_prio: 300
    from_prio: 57
    tier: 1
    weight: 1
    npt_prefix: "2607:f598:d3e8:4500::/60"
    health: { ... }

mwan_hash_mode: random
mwan_reserved_tables:
  cloudflared: 400
  oob: 500
mwan_pin_provider: att
```

A fourth provider is one more entry with `table: 600`, `mark: 4`,
`mark_prio: 600`, `from_prio: 58`, and its own tier. Table 400 and 500 are
taken, so 600 is the first free hundred.

Two groups of values stay outside the list because they feed the
systemd-networkd link files, which this piece leaves as they are: the
hardware values (the Webpass hardware address and DUID, its static IPv4
address and gateway, the AT&T EAP identity and DUID, the Monkeybrains
hardware address) keep their current variable names.

The pinned-destination lists lose the AT&T name: `mwan_pin_provider` names
the provider the pins target, and the seed and name lists are named for what
they pin, not for a provider. The kernel set names and the refresher timer
keep their current names until the refresher moves into the daemon under its
own ticket. The two WireGuard control-plane pins in the firewall file, which
hardcode mark 1 today, take the pin provider's mark.

`network.json` renders by looping over the list. The three-provider literal
leaves the template.

## Routing numbers are typed, and only checked

Each provider carries its routing table, its firewall mark, and its two policy
rule priorities as typed values, exactly as today. Nothing is derived. The
current numbering, 100, 200, 300 for tables and mark-rule priorities, 1, 2, 3
for marks, and 55, 56, 57 for source-rule priorities, is what every operator
of this gateway already knows, and it does not change.

What changes is validation. The two validators that accept only the three
current priority values are deleted, and three checks replace them at load:
every provider's table, mark, mark-rule priority, and source-rule priority is
unique across providers; no provider's table is in the reserved set; every
weight is at least one. A failed check stops the daemon before it touches the
kernel, which is the existing failure contract.

The reserved set is typed once in inventory, in `mwan_reserved_tables`,
rendered into `network.json` under the steering group, and read from there by
the daemon. The tunnel table, 400, already sits in the inventory registry
that names the routing tables; the out-of-band table, 500, is a bare literal
in three TOML templates today and joins the registry, and those templates read
the registry value. The kernel's own tables, 253, 254, 255, and 0, are always
reserved. No reader carries a copy of the set.

The deletion and the new checks land in one change, so no window exists where
one layer accepts a fourth provider and another rejects it.

## Steering becomes tier and weight

Every provider carries a tier and a weight. The active tier is the
lowest-numbered tier holding at least one healthy provider. New connections
from internal sources are assigned a mark computed over that tier's healthy
providers: a generated number modulo the sum of their weights, mapped onto
their marks with one slot per weight unit. A weight is a positive integer, so
the sum is never zero. The hash mode, `mwan_hash_mode`, selects whether the
number is random per connection, derived from the source address, or derived
from source and destination.

The tiers in inventory decide fallback and nothing else does. A provider
alone in its tier is the sole carrier when that tier is active, which is
exactly today's behavior with monkeybrains alone in tier 1. Providers that
share a tier share it by weight. The daemon carries no tie-break rule of its
own, and the monkeybrains name check leaves it.

The daemon owns the balancing rule from this piece on. A new steering module
computes it from the active tier and programs it into a kernel table and
chain the module creates, with the same apply discipline the translation
module uses: create the table, create the chain, clear the chain, add the
rules, commit once, and repair a flushed table through the watcher. The three
fixed lines leave the firewall ruleset file in the same change. This is the
shape the firewall piece keeps, so the daemon owning the firewall later adds
the rest of the ruleset around this module and rewrites nothing in it.

An unhealthy provider leaves the split on the next reconcile pass instead of
falling through to the main table. Its policy rules are pruned as today.

An unknown health state reads as healthy, so before the health module writes
its first state every provider reads healthy and the first tier activates.
That matches today's startup behavior and is preserved.

## The link files stay

The ten hand-written systemd-networkd link files and the two testbed forks
stay as they are in this piece. A fourth provider needs its interface file
and network file added by hand until the daemon brings links up itself under
the monolith epic (MWAN-397 to MWAN-401), which is gated on the daemon
running its own delegation client (MWAN-227). Any renderer written now would
be deleted when that lands, so none is written. For the same reason,
`network.json` does not describe link bring-up in this piece.

## The watchdog stops holding a provider list

The rollback watchdog on the hypervisor pings the internet through each
provider interface during a diagnosis, from a list typed by hand in its
configuration. That list omits AT&T, and the verdict it computes is
discarded by its only caller.

After this piece the gateway daemon pushes its per-provider health verdict to
the watchdog. The watchdog keeps its basic egress pings and smoke checks,
drops its per-interface pings, and holds no interface names. Whether the
pushed verdict then blocks a rollback is separate work (MWAN-442, MWAN-332,
MWAN-336).

## Carried through unchanged

The IPv6 source-pin prefix stays a configured value through this piece.
Steering builds a policy rule from it, and the cleanup pass claims that rule's
priority unconditionally, so rendering the value empty does not skip the rule,
it deletes the live one. Moving the pin onto the live delegation is separate
work with its own failure mode, since at daemon start the delegation may not
be readable yet.

## Acceptance

No provider name remains in Go outside tests or in a per-provider inventory
variable.

For the current provider set, the routes, policy rules, and the served tree
are unchanged, and the firewall rules are unchanged except that the three
balancing lines move from the ruleset file into the daemon's chain, where
they express the same half-and-half split.

A fourth provider can be added, re-tiered, and removed by inventory edit and
configuration deploy, with the binary unchanged, and traffic is observed
leaving it at the simulator's ingress in both address families. The testbed
gets a fourth simulated provider, named astount, built the same way as the
three that exist.

## Failure modes

Deleting the priority validators and introducing the new checks in separate
changes leaves a window where a fourth provider is accepted by one layer and
rejected by another. Do both in one change.

The ordering within the firewall's translation chain decides behavior,
because a translation statement stops rule evaluation. Grouping outbound
rules by provider is equivalent to today's grouping only because every
outbound translation rule carries an outgoing-interface match; the inbound
one-to-one rules match on the incoming interface instead. Assert both
invariants rather than relying on them.

The steering module's chain must run after the ruleset file's mangle chain,
which restores the connection mark and sets the ingress marks, and its
balancing rules must keep the `meta mark 0` guard, or the control-plane pins
set earlier in the pass are overwritten.
