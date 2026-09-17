# Three: the provider set becomes data

A provider is one entry in inventory. Adding, removing, re-tiering, or
re-weighting a provider is an edit to that entry and a configuration deploy.
The entry holds the provider's routing numbers, tier, weight, translation
prefix, probe policy, and link identity. The daemon validates the entry at
load and renders the systemd-networkd unit files for the provider's link
from it. No provider name appears in Go outside tests, and no file is
written by hand for a provider.

One link is exempt: the AT&T physical interface, which runs 802.1X
authentication and carries a fixed address for the fiber module. Its `.link`
and `.network` units stay in the repository with the 802.1X services that
reference them. The AT&T provider link is the VLAN on that interface, and the
VLAN renders like every other provider.

This piece depends on the configuration format, so the inventory is written
once in its final shape.

## Why a fourth provider is impossible today

Five things block it.

The daemon carries att, webpass, and monkeybrains as constants and decides
the fallback by comparing against one of them.

Two validators accept only the rule priorities 100, 200, 300 and 55, 56, 57.
A failed validation stops the daemon, so a provider with any other numbers
cannot start.

The load balancer is three fixed lines in the firewall ruleset file, one for
IPv4 and two for IPv6. Each line chooses between marks 1 and 2, so a third
member cannot be selected.

The network configuration renders from a template that names the three
providers, so a fourth inventory entry is not rendered.

The systemd-networkd unit files are written by hand, one pair per provider,
each named in a deploy list. A fourth provider needs two to four new files,
and nothing checks them against the provider's other values.

## Inventory takes the model's shape

Each gateway group carries one list with one entry per provider. Every value
the gateway reads about a provider is in that entry, including the hardware
values. No per-provider variable exists outside the list.

The production group after this piece:

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
      forwarding: true
      dhcp: true
    ipv6:
      forwarding: true
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
      forwarding: true
      address: "136.25.91.242/29"
      gateway: "136.25.91.241"
      route_metric: 10
    ipv6:
      forwarding: true
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
      forwarding: true
      dhcp: true
      route_metric: 5000
    ipv6:
      forwarding: true
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
Tables 400 and 500 are reserved, so 600 is the first free hundred. An
IPv4-only provider has no `ipv6` block, no `npt_prefix`, and no IPv6 probe
targets. It gets no IPv6 lease, no translation, and no IPv6 source rule.

Each value is typed once. Where a gateway entry and a simulator definition
describe the same wire, both read the service map.

Inventory writes an address the way an operator writes one, as a prefix in
slash notation. The published model splits it into an address and a prefix
length, so the template that renders the configuration file splits it too. That
is the only shape difference between an entry and the document it produces, and
it exists because both sides are written for their own reader.

The pinned-destination lists carry no provider name. `mwan_pin_provider`
names the provider the pins target. The seed and name lists are named for
what they pin. The kernel set names and the refresher timer keep their
current names until the refresher moves into the daemon under its own ticket.
The two WireGuard control-plane pins in the firewall ruleset file use the pin
provider's mark.

The network configuration renders by looping over the list. No template
names a provider.

## Routing numbers are typed, and only checked

Each provider carries its routing table, firewall mark, and two policy rule
priorities as typed values. Nothing derives them. The current numbering (100,
200, 300 for tables and mark-rule priorities; 1, 2, 3 for marks; 55, 56, 57
for source-rule priorities) does not change.

Three checks run at load. Every provider's table, mark, mark-rule priority,
and source-rule priority is unique across providers. No provider's table is
in the reserved set. Every weight is at least one. Two checks in the routing
module stay: a mark is never zero, because zero is the unmarked state the
balancing rule's guard tests, and neither rule priority equals the catch-all
priority the routing module owns. A failed check stops the daemon before it
touches the kernel.

The reserved set is typed once in inventory, in `mwan_reserved_tables`,
rendered into the network configuration under the steering group, and read
from there by the daemon. The tunnel table, 400, and the out-of-band table,
500, are in the inventory registry that names the routing tables, and every
template that needs one reads the registry. The kernel's own tables (253,
254, 255, and 0) are always reserved. No reader carries a copy of the set.

The fixed priority checks are deleted and the new checks land in one change.
No state exists where one layer accepts a fourth provider and another rejects
it.

## Steering becomes tier and weight

Every provider carries a tier and a weight. The active tier is the
lowest-numbered tier with at least one healthy provider. A new connection
from an internal source is assigned a mark computed over that tier's healthy
providers: a generated number modulo the sum of their weights, mapped onto
their marks with one slot per weight unit. A weight is a positive integer,
so the sum is never zero. `mwan_hash_mode` selects whether the number is
random per connection, derived from the source address, or derived from
source and destination.

The tiers in inventory decide fallback. A provider alone in its tier is the
sole carrier when that tier is active. Providers that share a tier share it
by weight. The daemon carries no tie-break rule.

The daemon owns the balancing rule. A steering module computes the rule from
the active tier and programs it into a kernel table and chain the module
creates, with the same apply discipline the translation module uses: create
the table, create the chain, clear the chain, add the rules, commit once, and
repair a flushed table through the watcher. The three fixed lines leave the
firewall ruleset file in the same change. The firewall piece later gives the
daemon the whole ruleset, and this rule stays where it is.

An unhealthy provider leaves the split on the next reconcile pass, and its
policy rules are pruned. It does not fall through to the main table.

An unknown health state reads as healthy. Before the health module writes its
first state, every provider reads healthy and the first tier activates.

## Link bring-up renders from the provider entry

systemd-networkd brings links up: it matches the device, names it, sets its
address, and runs both DHCP clients. The daemon writes its unit files. From
the loaded network configuration it renders one `.link` and one `.network`
per provider, plus a `.netdev` and a second `.network` for a provider on a
VLAN. No per-provider template exists in the repository. The deploy copies no
unit file for a provider link.

The daemon starts before udev names the devices and before systemd-networkd
starts: its unit carries `DefaultDependencies=no` and orders itself before
`systemd-udev-trigger.service` and `systemd-networkd.service`. udev applies a
`.link` file when a device appears, and the kernel refuses to rename a link
that is already up, so the files must be on disk before either happens. The
daemon writes the unit files first, then waits on netlink for the links as it
does today. It renders again whenever it reloads the configuration, tracks
which unit files it wrote for the current provider list, deletes any it wrote
for a provider that list no longer names, writes a file only when the content
differs from what is on disk, and asks systemd-networkd to reload after a
write or a deletion. Its sandbox gains a write path for
the network manager's unit directory. The firewall keeps loading before
`network-pre.target`, and the daemon takes no ordering after
`network-online.target`, which would close a cycle.

The entry describes a link in two layers.

**Typed leaves.** The standard per-family containers, plus the leaves this
piece adds for what the published models do not name: how the device is
matched (by driver or by hardware address), the interface name, the hardware
address, whether each family runs a DHCP client, the delegation client's
identity and hint, whether router advertisements are accepted, the route
metric, and an optional VLAN parent with its tag. The schema validates each
one. The daemon reads the ones it needs for its own behavior and maps each
leaf to a unit-file section and key through one table. That table is the
only place a networkd key name appears in Go.

**Free-form sections.** An augment on the interface that mirrors the unit
file format: for each of `link`, `network`, and `netdev`, an ordered list of
sections, each an ordered list of key and value pairs. The schema validates
the structure. networkd validates the keys. Any shape networkd can read can be
written here: a bond, a bridge, a tunnel, a VLAN stack, or an option added to
networkd after the model revision. A shape can start as free-form sections
and gain typed leaves later without a renderer change.

The renderer maps the typed leaves through its table, then appends the
free-form sections. A section named by both layers is merged under one
heading, because networkd treats repeated headings as one. A key named by
both layers is a load error. The unit files are written through a maintained
serializer for the systemd unit format, so the daemon owns the mapping and
not the syntax.

The current three providers render from typed leaves except for the keys
named in the examples below. The fidelity gate lists every free-form key the
current providers use.

An entry the renderer rejects stops the daemon at load, before it writes any
file or touches the kernel.

### Example: a static link with a delegation client

Webpass in `network.json`, link identity only. Leaf names are illustrative;
the model revision fixes them.

```json
{
  "name": "enwebpass0",
  "goodkind-mwan-steering:link": {
    "match": { "driver": "igc" },
    "hardware-address": "...",
    "delegation": { "hint": "::/56", "duid-type": "link-layer-time", "duid": "..." },
    "route-metric": 10
  },
  "ietf-ip:ipv4": {
    "forwarding": true,
    "address": [ { "ip": "136.25.91.242", "prefix-length": 29 } ],
    "goodkind-mwan-steering:gateway": "136.25.91.241"
  },
  "ietf-ip:ipv6": {
    "forwarding": true,
    "goodkind-mwan-steering:dhcp": true,
    "goodkind-mwan-steering:accept-ra": true
  },
  "goodkind-mwan-steering:wan": { "table-id": 200, "...": "..." }
}
```

The daemon writes `20-webpass.link`:

```ini
[Match]
Driver=igc

[Link]
Name=enwebpass0
MACAddress=...
```

and `20-webpass.network`:

```ini
[Match]
Name=enwebpass0

[Network]
Address=136.25.91.242/29
DHCP=ipv6
IPv6AcceptRA=yes
IPv4Forwarding=yes
IPv6Forwarding=yes

[DHCPv6]
DUIDType=link-layer-time
DUIDRawData=...
PrefixDelegationHint=::/56

[Route]
Gateway=136.25.91.241
Metric=10

[Route]
Gateway=136.25.91.241
Table=200
```

Every line comes from a typed leaf. The second route's table is the
provider's routing table, taken from `table-id`.

### Example: a key with no typed leaf

Monkeybrains solicits a delegation on a link where no router advertisement
arrives, and advertises a router lifetime downstream. Neither has a typed
leaf. The entry carries them as free-form sections:

```json
"goodkind-mwan-steering:networkd": {
  "network": [
    { "section": "DHCPv6", "entries": [ { "key": "WithoutRA", "value": "solicit" } ] },
    { "section": "IPv6PrefixDelegation", "entries": [ { "key": "RouterLifetimeSec", "value": "1800" } ] }
  ]
}
```

The rendered `.network` file carries them after the typed keys:

```ini
[DHCPv6]
DUIDType=link-layer-time
DUIDRawData=...
PrefixDelegationHint=::/56
WithoutRA=solicit

[IPv6PrefixDelegation]
RouterLifetimeSec=1800
```

If the free-form sections also set `PrefixDelegationHint`, the daemon stops
at load with:

```
network.json: enmbrains0: networkd section DHCPv6 key PrefixDelegationHint is set by the delegation hint leaf; remove one
```

### Example: a provider on a VLAN

AT&T's provider link is a VLAN on the physical 802.1X link. The entry names
the parent and the tag:

```json
"goodkind-mwan-steering:link": {
  "vlan": { "parent": "enatt0", "id": 3242 },
  "delegation": { "hint": "::/60", "duid-type": "vendor", "duid": "..." }
}
```

The daemon writes `21-att-vlan.netdev`:

```ini
[NetDev]
Name=enatt0.3242
Kind=vlan

[VLAN]
Id=3242
```

and `21-att-vlan.network`:

```ini
[Match]
Name=enatt0.3242

[Network]
DHCP=yes
IPv6AcceptRA=yes
IPv4Forwarding=yes
IPv6Forwarding=yes

[DHCPv6]
DUIDType=vendor
DUIDRawData=...
PrefixDelegationHint=::/60
```

The parent's `20-att.link` and `20-att.network` are not rendered. They carry
the 802.1X match and the fiber-module address and stay in the repository.

### Example: a shape with no typed leaf at all

A bonded uplink has no typed leaves in this piece. It is one entry with
free-form sections:

```json
"goodkind-mwan-steering:networkd": {
  "netdev": [
    { "section": "NetDev", "entries": [ { "key": "Name", "value": "bond0" }, { "key": "Kind", "value": "bond" } ] },
    { "section": "Bond", "entries": [ { "key": "Mode", "value": "active-backup" } ] }
  ],
  "network": [
    { "section": "Network", "entries": [ { "key": "DHCP", "value": "yes" } ] }
  ]
}
```

The daemon writes a `.netdev` and a `.network` with those sections.

## The watchdog holds no provider list

The rollback watchdog on the hypervisor pings the internet through each
provider interface during a diagnosis. After this piece the gateway daemon
pushes its per-provider health verdict to the watchdog. The watchdog keeps its
basic egress pings and smoke checks, drops its per-interface pings, and holds
no interface names.

The push is advisory and stateless. Every message carries the whole verdict,
one entry per provider plus the active tier. The watchdog keeps the latest
message and the time it arrived, and logs both during a diagnosis. A restart
on either side, or a lost message, is repaired by the next probe cycle, which
sends the whole state again. A watchdog that has received nothing reports
that it holds no verdict. No rollback decision reads the verdict in this
piece. Whether it blocks a rollback is separate work (MWAN-442, MWAN-332,
MWAN-336).

## Carried through unchanged

The IPv6 source-pin prefix stays a configured value. Steering builds a policy
rule from it, and the cleanup pass claims that rule's priority
unconditionally, so rendering the value empty deletes the live rule rather
than skipping it. Moving the pin onto the live delegation is separate work,
because at daemon start the delegation may not be readable yet.

The daemon does not create links and does not run its own delegation client.
Both stay with systemd-networkd. Moving them into the daemon is the monolith
epic's work (MWAN-305). It is gated on the daemon owning the lease first,
because a link created by one program and leased by another has two
authorities.

## Acceptance

No provider name remains in Go outside tests or in an inventory variable that
the daemon or a rendered template reads by provider name.

For the current provider set, the routes, policy rules, and the served tree
are unchanged. The firewall rules are unchanged except that the three
balancing lines move from the ruleset file into the daemon's chain, where they
express the same half-and-half split.

The daemon's move before udev and the network manager is proven on the
testbed before it reaches production: a cutover with a reboot that records
the daemon starting before both, every provider link up with its name and
lease, and rules, routes, firewall and served tree unchanged; a failover
exercise (the fallback drill in both families, a reboot with one link
absent, the AT&T 802.1X path, and a daemon restart while links are up); and
a check-mode run against the production gateway before the production
cutover.

The rendered systemd-networkd units for the current provider set are
identical to the hand-authored files they replace, outside comment lines.
This comparison runs in CI against the checked-in files before those files
are deleted. The AT&T physical link's two units are excluded, because they
stay.

A fourth provider can be added, re-tiered, and removed by one inventory entry
and a configuration deploy, with the binary unchanged and no file written by
hand. Traffic is observed leaving it at the simulator's ingress in every
address family the provider has. The testbed's fourth simulated provider,
astound, is IPv4-only, so its proof is IPv4 only.

## Failure modes

Deleting the priority checks and introducing the new checks in separate
changes leaves a state where one layer accepts a fourth provider and another
rejects it. Do both in one change.

Deleting the hand-authored unit files before the rendered ones are proven
identical leaves a gateway whose links come up differently after a reboot.
Every layer above keys on interface names. The fidelity comparison gates the
deletion.

A free-form key is not checked by the deploy's schema validation. A typo
there surfaces as a networkd warning on the gateway, not as a deploy failure.
The typed leaves exist so the common shapes do not take that path.

The ordering within the firewall's translation chain decides behavior,
because a translation statement stops rule evaluation. Grouping outbound
rules by provider is equivalent to the current grouping only because every
outbound translation rule carries an outgoing-interface match, and the
inbound one-to-one rules match on the incoming interface. Assert both
invariants.

The steering module's chain must run after the ruleset file's mangle chain,
which restores the connection mark and sets the ingress marks. Its balancing
rules must keep the `meta mark 0` guard. Otherwise the control-plane pins set
earlier in the pass are overwritten.
