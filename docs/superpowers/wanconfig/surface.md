# One: the model and the read-only surface

Define the model for the whole daemon and serve it against the system as it
stands, before any behavior moves. This piece changes nothing an operator
would notice in the data path, and it produces the instrument every later
piece is verified against.

Depends on nothing. The model it defines lives in [model.md](model.md).

## What the daemon serves

Two kinds of thing, in one tree.

**Configuration as loaded.** Every interface with its per-family containers,
addressing, and forwarding. Every translation instance with its type and its
policy. Every steering member with its tier, weight, and probe policy. The
routing tables and the protocols that fill them. What the daemon read at
startup, not what a file on disk says, so the tree cannot drift from the
running process.

The whole daemon, not only the provider path. That includes the modules an
operator reaches for during an incident and cannot currently see without a
shell: the rollback watchdog's thresholds and its probe targets, the
out-of-band access policy, and the tunnel tap. A module whose configuration
is not in the tree is a module whose behavior has to be guessed at from
logs.

**Live state.** The delegation each provider currently holds. Each member's
health and the probe results behind it. Which tier is active and which member
is carrying traffic. Whether each translation instance is realized in the
kernel. Whether the routing session is established. The firewall ruleset the
daemon intends, which subsumes the ad-hoc debug views.

The test of coverage is a question, not a checklist: an operator should be
able to answer why traffic is leaving a given provider, and why a given
provider is not carrying any, without opening a shell on the gateway.

## How it is served

The mature management stack serves the tree. libyang reads the model,
sysrepo holds the data, and the stack's own servers answer requests:
RESTCONF for reading over ordinary web requests carrying JSON, with NETCONF
available from the same stack. How a subscriber is told about changes is
settled in the streaming piece; the model is identical either way.

The daemon registers as the provider of its own subtrees, so a read reaches
into the daemon at request time rather than reading a copy that can drift.
Where a value is pushed instead, the push happens at the moment the value
changes. Drift in the one thing whose purpose is to describe reality is
worse than no description.

The daemon's own part is one small publishing binding: connect, open a
session, set values by path, apply. Everything protocol-shaped is off the
shelf.

## What must not change

No data-path behavior. This piece adds a reader and nothing else.

The surface listens on the management interface only, which is where the
existing management access already terminates. It is a read-only surface, but
it exposes the gateway's full configuration, so it belongs on the same
restricted path as the existing management access and not on any provider
interface.

## Acceptance

The served tree describes the running system accurately enough to answer the
two operator questions above.

Every value in the tree is read from the daemon's own state, not
reconstructed from a file, and no value is served that the daemon does not
actually hold.

A subscription reports a health transition and a tier change as they happen.

The data path is unchanged, verified by comparing the firewall ruleset,
routes, and policy rules before and after.

## Failure modes

The reader must never be able to affect the daemon it describes. A slow or
abandoned request, a subscription that stops being read, or a malformed path
must not stall a reconcile pass or hold a lock the data path needs.

If the tree cannot be built, the daemon serves nothing and keeps running.
Describing the system is not a precondition for running it.
