# Four: translation becomes typed instances

Each address family of each provider declares how its traffic is translated,
or that it is not translated at all. One-to-one mapping works in both
families. Prefixes of differing length follow the standard's rule instead of
a hardcoded length.

Depends on the provider set being data, because an instance attaches to a
member.

## The problem this solves

IPv6 has exactly one translation behaviour and no way to express another.
Every provider gets the same prefix-translation rule set, built
unconditionally, with no type field anywhere. A provider with no readable
delegation is dropped from the translation table entirely, so a statically
routed prefix, a plain address masquerade, and native routing without
translation are all inexpressible.

The exclusivity is already load-bearing and recorded only as a comment in the
firewall template, warning that a static masquerade on one provider would
override prefix translation through rule ordering. Two behaviours that cannot
coexist, with nothing in configuration saying which applies, is the shape of
a bug waiting for a fourth provider.

IPv4 has the mirror gap. One-to-one mapping exists, but the template loops
two provider keys by name, so a third key renders nothing and fails silently,
and there is no IPv6 equivalent at all.

## What each family declares

An instance type per family per member: prefix translation, address
masquerade, one-to-one mapping, or none. No instance means no translation,
which is the case a member with globally routed addresses needs.

A member whose type is none routes that family without translation and raises
no missing-delegation alert. Today the absence of a delegation is always an
error; under the model it is only an error for a member that declared it
needs one.

One-to-one mapping becomes available on any member in either family, which
removes both the two-provider limit and the IPv4-only limit in one change.

## Prefixes of differing length

Apply the standard's rule: when the internal and external prefixes differ in
length, extend the shorter with zeroes so they match, then translate. Do not
force either prefix to a fixed length.

The current implementation forces the delegation to a fixed length regardless
of what was delegated, with no upper bound check, so a provider delegating a
shorter prefix produces translation onto address space the gateway does not
hold. Only one of the four delegation probes filters by length, and it is not
the one that runs first.

Reject rather than silently accept the case the standard cannot resolve, and
say which member and which pair of prefixes in the error.

## Translation never matches a firewall mark

Source translation keys on the outgoing interface and the internal source
address, in both families. The outgoing interface already determines the exit
path, so a mark adds nothing to the match and removes the rule's ability to
handle traffic whose mark is stale.

This is not a change for IPv6, whose rules already key on the interface
alone, and that property must survive the move to typed instances. It is a
change for IPv4, where two of three rules require a matching mark today.
Internal IPv4 traffic carries only the marks the balancer assigns, which are
the top tier's, and steering sends that traffic out an activated fallback
member without rewriting the mark. A mark-scoped rule would not match, the
traffic would leave with a private source address, and IPv4 would fail at the
moment fallback exists to prevent it. Neither the deploy gate nor a normal
testbed run exercises that path, because fallback is not active during a
deploy.

## Realisation is coupled to steering

A member whose family cannot translate stops receiving that family's traffic.

Today there is no such coupling. A provider with no delegation is dropped
from the translation table while steering keeps its rules and health still
passes it on the other family's probes, so half the internal IPv6 flows leave
with an untranslated source and are discarded upstream. The only signal is a
warning alert.

The per-family operational status the read-only surface already exposes is
where this coupling becomes visible: a family whose instance is not realised
reports down, and steering reads that.

## Inbound survives every mode that can support it

A member that declares inbound reachability keeps it: the reverse translation
for prefix translation, the mapping entries for one-to-one, and the edge
address rewrite. Address masquerade is outbound only by nature, and a member
declaring it declares no inbound reachability, which the model states rather
than leaving to be discovered.

## Acceptance

Each of the four types is expressible and produces the rules it should, in
both families where the type applies.

For the current provider set the programmed rules are unchanged, since every
member today is prefix translation on IPv6 and address masquerade with
one-to-one mapping on IPv4.

A member with a delegation shorter than the internal prefix translates
correctly under the standard's rule rather than onto space the gateway does
not hold.

A member whose IPv6 cannot be realised receives no IPv6 traffic, and its
per-family status reports down.

Outbound traffic is translated while a fallback tier is active, in both
families. This is the case the mark rule change exists for, and it must be
exercised deliberately.

## Failure modes

The testbed provider simulators masquerade what they receive on the way out,
so nothing observed beyond a simulator proves the gateway produced the right
source address. Observe at the simulator's ingress.

Loosening the missing-delegation alert to apply only to members that need one
must not silence it for members that do. The alert is the only signal today
that a provider's IPv6 has quietly stopped working.
