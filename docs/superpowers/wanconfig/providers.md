# Three: the provider set becomes data

Adding, removing, re-tiering, or re-weighting a provider becomes an inventory
edit and a configuration deploy. No provider name appears in Go, in a
template filename, or in a per-provider variable. One binary serves any
provider set.

Depends on the configuration format, so the inventory is written once in its
final shape.

## Identifiers derive from a member index

Each member declares one small integer index, never reused and never
dependent on map order. From it the system derives the routing table number,
the firewall mark, and both policy rule priorities. An operator never writes
any of the four.

Validation rejects a duplicate index, and rejects a derived table number that
collides with a routing table registered for something else. The reserved set
is read from the same inventory that names the routing tables, so it cannot
drift from the live registrations. Those are 400 for the tunnel and 500 for
the out-of-band table, plus the kernel's own three. An earlier draft of this
work reserved 900, which is wrong and would have let a fourth member collide.

For the current three members every derived value equals the number assigned
by hand today, so this changes no live state.

Two validators accept only the three current priority values and reject
anything else, and a failed validation stops the daemon. They are what makes
a fourth provider impossible, and they must be deleted in the same change
that introduces derivation, not after it.

## Steering becomes tier and weight

The active tier is the lowest-numbered tier holding at least one healthy
member. New connections from internal sources are assigned a mark computed
over that tier's members: a generated number modulo the sum of their weights,
mapped onto member marks with one slot per weight unit. A weight is a
positive integer, and validation rejects anything else, so the sum the
modulo divides by is never zero. The hash mode selects whether that number
is random per connection, derived from the source address, or derived from
source and destination.

When a tier above the first is active, the catch-all rules point at that
tier's healthy member with the lowest index. An unhealthy member's policy
rules are pruned and stray marked traffic falls through to the main table.

Today the balancer is three hardcoded expressions, one for IPv4 and two for
IPv6, each with a fixed modulus of two and a fixed pair of marks. The
fallback member's mark appears in none of them, so the balancer can never
select it in either family. Deriving the expression from the active tier's
member list removes all three.

An unknown health state reads as healthy, so before the health module writes
its first state every member reads healthy and the first tier activates. That
matches today's startup behavior and must be preserved.

## One renderer builds every link

The per-provider link files are replaced by one set driven by the per-family
containers: an interface file and a network file for every member, a
tagged-child device file where a member declares a tag, and a parent file
plus supplicant configuration where a member declares authentication. A
member differs from its peers in data only, never in which code path builds
it.

The schema must express what the current files actually do, which is more
than a flat set of flags. Members are matched either by driver or by hardware
address, and two of them additionally override the hardware address, one of
those because the card reports the wrong one. A statically addressed member
needs two route entries, one into the main table at a low metric and one into
its own table. Router lifetime, unsolicited start, and delegated-prefix
settings differ per member today and must remain expressible.

Every per-provider link file is deleted, including both testbed forks and the
unreferenced static copies that no playbook reads and whose comments quote
production public addressing.

## The probe list is derived

The rollback watchdog pings out of each configured provider interface to tell
a real outage apart from a routing failure, and it returns healthy on the
first interface that answers. The list is therefore a logical union, and an
added member can only reduce false rollbacks.

That list is hand-maintained today and omits one provider, so a gateway
reachable only through that provider reads as a total outage. Deriving the
list from the member list closes it.

The watchdog computes this verdict and then discards it: its only caller
ignores the return value, so the result reaches an operator as log lines and
an alert body and no rollback decision reads it. Adding the missing provider
is worth doing for the diagnosis alone. Wiring the verdict into the rollback
decision changes the safety system and has its own ticket.

## Carried through unchanged

The IPv6 source-pin prefix stays a configured value through this piece.
Steering builds a policy rule from it, and the cleanup pass claims that rule's
priority unconditionally, so rendering the value empty does not skip the rule,
it deletes the live one. Moving the pin onto the live delegation is separate
work with its own failure mode, since at daemon start the delegation may not
be readable yet.

## Acceptance

No provider name remains in Go outside tests, in a template filename, or in a
per-provider variable.

For the current provider set, the firewall rules, routes, policy rules, and
the served tree are unchanged.

The rendered link files match the hand-authored ones they replace, outside
comment text. Accept nothing less here: a wrong link file renames an
interface, and every layer above keys on interface names.

A fourth member can be added, re-tiered, and removed by inventory edit and
configuration deploy, with the binary unchanged.

## Failure modes

Deleting the priority validators and introducing derivation in separate
changes leaves a window where a fourth member is accepted by one layer and
rejected by another. Do both in one change.

The ordering within the firewall's translation chain decides behavior,
because a translation statement stops rule evaluation. Grouping rules by
member is equivalent to today's grouping only because every rule carries an
outgoing-interface match. Assert that invariant rather than relying on it.
