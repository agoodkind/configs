# Three: the provider set becomes data

Adding, removing, re-tiering, or re-weighting a provider is one inventory
entry and a configuration deploy. The entry carries everything the gateway
needs to know about that provider: its routing numbers, its tier and weight,
its translation prefix, its probe policy, and its link identity. The daemon
checks the entry instead of knowing it, and the binary renders the network
manager's unit files from it, so one binary serves any provider set and no
provider name appears in Go outside tests.

One link keeps hand-written units: the AT&T physical interface, which exists
to run 802.1X authentication and carries a fixed address for the fiber
module. Its `.link` and `.network` stay in the repository beside the 802.1X
services that reference them. The AT&T provider link is the VLAN on top of
it, and that renders like every other provider.

Depends on the configuration format, so the inventory is written once in its
final shape.

## Why a fourth provider is impossible today

Five things block it, and each one goes away in this piece.

The daemon knows the three providers by name. It carries att, webpass, and
monkeybrains as constants and decides the fallback by comparing against one of
them.

The daemon accepts only the rule priorities in use. Two checks admit exactly
100, 200, 300 and 55, 56, 57, and a failed check stops the daemon, so a
provider with any other numbers cannot start.

The load balancer cannot select a third member. It is three fixed lines in the
firewall ruleset file, one for IPv4 and two for IPv6, and each one flips a
coin between marks 1 and 2.

The network configuration renders from a template that lists the three
providers by name, so a fourth entry in inventory is never rendered.

The network manager's unit files are written by hand, one pair per provider,
each named individually in a deploy list. A fourth provider needs two to four
new files authored from scratch, and nothing checks them against the
provider's other values.

## Inventory takes the model's shape

Each gateway group carries one list with one entry per provider, and every
value the gateway reads about a provider sits in that entry. The per-provider
variables that carry a provider name in each gateway group today collapse
into the list, the hardware values included.

After this piece, the production group:

```yaml
mwan_providers:
  - name: att
    table: 100
    mark: 1
    mark_prio: 100
    from_prio: 55
    tier: 0
    weight: 1
    link:
      iface: enatt0
      vlan_id: 3242
    ipv4:
      dhcp: true
    ipv6:
      dhcp: true
      accept_ra: true
      delegation:
        hint: "::/60"
        duid_type: vendor
        duid: "..."
    npt_prefix: "2600:1700:2f71:c80::/60"
    forced_dscp: cs1
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
    table: 200
    mark: 2
    mark_prio: 200
    from_prio: 56
    tier: 0
    weight: 1
    link:
      iface: enwebpass0
      match: { driver: igc }
      mac: "..."
    ipv4:
      address: "136.25.91.242/29"
      gateway: "136.25.91.241"
      route_metric: 10
    ipv6:
      dhcp: true
      accept_ra: true
      delegation:
        hint: "::/56"
        duid_type: link-layer-time
        duid: "..."
    v4_source: "136.25.91.242"
    npt_prefix: "2604:5500:c271:be00::/60"
    static_mappings: [ ... ]
    health: { ... }

  - name: monkeybrains
    table: 300
    mark: 3
    mark_prio: 300
    from_prio: 57
    tier: 1
    weight: 1
    link:
      iface: enmbrains0
      match: { mac: "..." }
    ipv4:
      dhcp: true
      route_metric: 5000
    ipv6:
      dhcp: true
      accept_ra: true
      route_metric: 5000
      delegation:
        hint: "::/56"
        duid_type: link-layer-time
        duid: "..."
    npt_prefix: "2607:f598:d3e8:4500::/60"
    health: { ... }

mwan_hash_mode: random
mwan_reserved_tables:
  cloudflared: 400
  oob: 500
mwan_pin_provider: att
```

A fourth provider is one more entry with `table: 600`, `mark: 4`,
`mark_prio: 600`, `from_prio: 58`, its own tier, and its own link block.
Tables 400 and 500 are taken, so 600 is the first free hundred. An IPv4-only
provider carries no `ipv6` block, no `npt_prefix`, and no IPv6 probe targets,
and gets no IPv6 lease, no translation, and no IPv6 source rule.

Each value is typed once. Where the gateway's entry and a simulator's
definition describe the same wire, both read the service map rather than
repeating the value.

The pinned-destination lists carry no provider name. `mwan_pin_provider`
names the provider the pins target, and the seed and name lists are named for
what they pin. The kernel set names and the refresher timer keep their
current names until the refresher moves into the daemon under its own ticket.
The two WireGuard control-plane pins in the firewall ruleset file take the
pin provider's mark.

The network configuration renders by looping over the list. No template names
a provider.

## Routing numbers are typed, and only checked

Each provider carries its routing table, its firewall mark, and its two policy
rule priorities as typed values, and nothing derives them. The current
numbering (100, 200, 300 for tables and mark-rule priorities; 1, 2, 3 for
marks; 55, 56, 57 for source-rule priorities) is what every operator of this
gateway already knows, and it does not change.

Three checks run at load: every provider's table, mark, mark-rule priority,
and source-rule priority is unique across providers; no provider's table is in
the reserved set; every weight is at least one. Two structural checks the
routing module makes stay: a mark is never zero, because zero is the unmarked
state the balancing rule's guard tests, and neither rule priority may equal
the catch-all priority the routing module owns for itself. A failed check
stops the daemon before it touches the kernel.

The reserved set is typed once in inventory, in `mwan_reserved_tables`,
rendered into the network configuration under the steering group, and read
from there by the daemon. The tunnel table, 400, and the out-of-band table,
500, sit in the inventory registry that names the routing tables, and every
template that needs one reads the registry value. The kernel's own tables
(253, 254, 255, and 0) are always reserved. No reader carries a copy of the
set.

The fixed priority checks are deleted and the new checks land in one change,
so no window exists where one layer accepts a fourth provider and another
rejects it.

## Steering becomes tier and weight

Every provider carries a tier and a weight. The active tier is the
lowest-numbered tier holding at least one healthy provider. New connections
from internal sources are assigned a mark computed over that tier's healthy
providers: a generated number modulo the sum of their weights, mapped onto
their marks with one slot per weight unit. A weight is a positive integer, so
the sum is never zero. The hash mode, `mwan_hash_mode`, selects whether the
number is random per connection, derived from the source address, or derived
from source and destination.

The tiers in inventory decide fallback, and nothing else does. A provider
alone in its tier is the sole carrier when that tier is active, which is
today's behavior with monkeybrains alone in tier 1. Providers that share a
tier share it by weight. The daemon carries no tie-break rule of its own.

The daemon owns the balancing rule, because the firewall piece later makes the
daemon own the whole ruleset and a rule built in the daemon now is a rule that
piece keeps. A steering module computes the rule from the active tier and
programs it into a kernel table and chain the module creates, with the same
apply discipline the translation module uses: create the table, create the
chain, clear the chain, add the rules, commit once, and repair a flushed table
through the watcher. The three fixed lines leave the firewall ruleset file in
the same change.

An unhealthy provider leaves the split on the next reconcile pass instead of
falling through to the main table, and its policy rules are pruned.

An unknown health state reads as healthy, so before the health module writes
its first state every provider reads healthy and the first tier activates.

## Link bring-up renders from the provider entry

systemd-networkd keeps bringing links up: it matches the device, names it,
sets its address, and runs both DHCP clients. What changes is where its unit
files come from. The binary renders them from `network.json` through the
install verb, one `.link` and one `.network` per provider, plus a `.netdev`
and a second `.network` for a provider on a VLAN. No per-provider template
exists in the repository, and the deploy copies no unit file for a provider
link.

The entry describes a link in two layers.

**Typed leaves.** The standard per-family containers, plus the leaves this
piece adds for what the standard model has no name for: how the device is
matched (by driver or by hardware address), the interface name, the hardware
address, whether each family runs a DHCP client, the delegation client's
identity and hint, whether router advertisements are accepted, the route
metric, and an optional VLAN parent with its tag. The schema validates every
one of them, and the daemon reads the ones it needs for its own behavior.
The binary turns each leaf into the unit-file key it stands for through one
table, which is the only place a networkd key name appears in Go.

**Free-form sections.** An augment on the interface that mirrors the unit
file format itself: for each of `link`, `network`, and `netdev`, an ordered
list of sections, each an ordered list of key and value pairs. The schema
validates the structure; networkd validates the keys, because it is the thing
that knows them. Anything networkd can read, this can say: a bond, a bridge,
a tunnel, a VLAN stack, or an option that did not exist when the model was
written. A shape ships as free-form sections first and gains typed leaves
later, without touching the renderer.

The renderer maps the typed leaves through its table, then appends the
free-form sections. A key set by both is a load error, never a silent
override. The unit files are written through a maintained serializer for the
systemd unit format, so the binary owns the mapping and nothing about the
syntax.

The current three providers render from typed leaves alone. Their free-form
sections are empty, and they stay the exception rather than the norm.

A shape the renderer rejects stops the install verb before it writes
anything, which is the same failure contract a bad configuration has always
had.

## The watchdog holds no provider list

The rollback watchdog on the hypervisor pings the internet through each
provider interface during a diagnosis. After this piece the gateway daemon
pushes its per-provider health verdict to the watchdog. The watchdog keeps
its basic egress pings and smoke checks, drops its per-interface pings, and
holds no interface names.

The push is advisory and stateless. Every message carries the whole verdict,
one entry per provider plus the active tier, so the watchdog keeps only the
latest message and the time it arrived, and logs both during a diagnosis. A
restart on either side, or a lost message, costs nothing to replay: the next
probe cycle sends the whole state again, and a watchdog that has received
nothing yet reports that it holds no verdict. No rollback decision reads the
verdict in this piece; whether it blocks a rollback is separate work
(MWAN-442, MWAN-332, MWAN-336).

## Carried through unchanged

The IPv6 source-pin prefix stays a configured value through this piece.
Steering builds a policy rule from it, and the cleanup pass claims that rule's
priority unconditionally, so rendering the value empty does not skip the rule,
it deletes the live one. Moving the pin onto the live delegation is separate
work with its own failure mode, since at daemon start the delegation may not
be readable yet.

The daemon does not create links itself and does not run its own delegation
client. Both stay with systemd-networkd. Moving them into the daemon is the
monolith epic's work (MWAN-305) and is gated on the daemon owning the lease
first, because a link created by one program and leased by another has two
authorities.

## Acceptance

No provider name remains in Go outside tests or in an inventory variable that
the daemon or a rendered template reads by provider name.

For the current provider set, the routes, policy rules, and the served tree
are unchanged. The firewall rules are unchanged except that the three
balancing lines move from the ruleset file into the daemon's chain, where they
express the same half-and-half split.

The rendered network manager units for the current provider set are identical
to the hand-authored files they replace, outside comment lines, and this
comparison runs in CI against the checked-in files before those files are
deleted. The AT&T physical link's two units are excluded, since they stay.

A fourth provider can be added, re-tiered, and removed by one inventory entry
and a configuration deploy with the binary unchanged, with no file authored
by hand, and traffic is observed leaving it at the simulator's ingress in
every address family the provider has. The testbed's fourth simulated
provider, named astound, is IPv4-only, so its proof is IPv4 only.

## Failure modes

Deleting the priority checks and introducing the new checks in separate
changes leaves a window where a fourth provider is accepted by one layer and
rejected by another. Do both in one change.

Deleting the hand-authored unit files before the rendered ones are proven
identical leaves a gateway whose links come up differently after a reboot,
and every layer above keys on interface names. The fidelity comparison gates
the deletion.

A free-form key is not caught by the deploy's schema check. A typo there
surfaces as a networkd warning on the gateway, not as a deploy failure. The
typed leaves exist so that the common shapes never go through that path.

The ordering within the firewall's translation chain decides behavior,
because a translation statement stops rule evaluation. Grouping outbound
rules by provider is equivalent to today's grouping only because every
outbound translation rule carries an outgoing-interface match, and the
inbound one-to-one rules match on the incoming interface instead. Assert both
invariants rather than relying on them.

The steering module's chain must run after the ruleset file's mangle chain,
which restores the connection mark and sets the ingress marks, and its
balancing rules must keep the `meta mark 0` guard, or the control-plane pins
set earlier in the pass are overwritten.
