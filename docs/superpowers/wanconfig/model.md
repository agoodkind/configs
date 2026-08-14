# The model

The gateway describes itself with published IETF models. Only the steering
concepts have no published model, so they live in one local module that
extends the others. This page is the single home for the model's shape; the
five specifications express their behavior in its terms.

## What the published models give

**Interfaces.** RFC 8343 defines an `interfaces` container holding an
`interface` list keyed by name, with `type`, `enabled`, and operational
status. Every provider link, the internal link, and the management link are
entries in that one list.

**A container per address family.** RFC 8344 augments each interface with an
`ipv4` container and an `ipv6` container. Each carries `enabled`,
`forwarding`, `mtu`, an `address` list, and a `neighbor` list; the IPv6
container adds an `autoconf` container. This is the single most valuable
thing the model brings. Every asymmetry in the current system exists because
there is nowhere per-family to put behavior, so behavior landed wherever it
was first needed. With these containers, treating the two families alike is
structural rather than a thing to remember.

**Translation as a typed instance.** RFC 8512 defines a `nat` container
holding an `instances/instance` list, each with a `type` drawn from
identities including `napt44`, `basic-nat44`, `dst-nat`, and `nptv6`. An
instance carries a `policy`, which for prefix translation holds
`nptv6-prefixes` as an explicit internal and external pair, and a
`mapping-table` whose entries may be `static`. Address masquerade, prefix
translation, one-to-one mapping, and no translation at all become values of
one field.

**Routing.** RFC 8349 defines control-plane protocols and routing tables,
which covers the per-provider tables and the learned routes.

## What the local module adds

One module extends the interface list with the steering properties no
published model covers:

- `tier`, a small integer. Lower is preferred.
- `weight`, for unequal balancing among members of one tier.
- `probe-policy`, naming the health probes that decide the member's state.

Alongside the interface list it adds the group's `hash-mode`, selecting how a
new connection is assigned to a member: at random, by source address, or by
source and destination.

A member is an interface with steering properties, not a parallel object.
That keeps one identity per link.

## What today's behavior becomes

Each row is a current special case and the model element that replaces it.

| Today | In the model |
|---|---|
| IPv4 and IPv6 translation are different code paths in different components | two instances on one interface, types `napt44` and `nptv6` |
| One-to-one mapping exists only for IPv4, and only for the two providers named in a template | `mapping-table` entries of type `static`, on any interface, either family |
| No way to express address masquerade, a statically routed prefix, or no translation | the instance `type`; no instance means no translation |
| A delegation is forced to a fixed length, and a shorter one is widened onto space the gateway does not hold | both prefixes are explicit leaves, and RFC 6296 defines the rest |
| Nothing checks the two prefix lengths against each other | both are modeled, so the check is schema-level |
| The load balancer is hardcoded in three expressions and cannot select the fallback provider | derived from the member list of the active tier |
| A provider with no delegation keeps receiving IPv6 and discarding it | the IPv6 container's operational status is down when its instance cannot be realized |
| The health verdict merges both families | probe policy per family, because there is now a per-family container to hold it |
| Forwarding is enabled globally before any firewall exists | the `forwarding` leaf, per interface per family |
| The rollback watchdog's probe list is hand-maintained and omits one provider | derived from the member list |
| Four routing identifiers are hand-assigned per provider | derived from the member index, and not modeled at all |

## Prefixes of differing length

RFC 6296 does not require the internal and external prefixes to match. It
specifies what to do when they differ: the translation function first ensures
they are the same length, extending the shorter of the two with zeroes. It
also defines the translation as stateless and checksum-neutral, which is why
no per-flow state is kept and why transport checksums need no repair.

The current implementation does neither. It forces the delegation to a fixed
length regardless of what the provider delegated, so a shorter delegation is
widened onto address space the gateway does not hold. The model carries both
prefixes explicitly, so the standard's rule can be applied as written.

## Libraries

`freeconf` parses the model, binds an existing Go value to the tree by
reflection, and serves RESTCONF and gNMI. Its compliance page lists YANG 1.1,
JSON encoding, and the YANG library as implemented, and RESTCONF as
implemented without XML or entity tags, which means JSON only. It does not
list the staged-change datastores, access control, or subscriptions, which is
why the served surface is read-only.

`openconfig/ygot` generates validated Go structures from any model and
renders the same JSON encoding. Reach for it only if binding by reflection
proves insufficient, and do not run both approaches at once.
