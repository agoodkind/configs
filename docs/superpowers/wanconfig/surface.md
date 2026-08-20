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

## How the configuration is published

The interface-manager process that runs the gateway's steering role owns the
configuration part of the tree. It publishes once, at startup, after it has
loaded and validated its configuration and before it programs anything, and
it publishes from the typed configuration it is about to run with, not from
the file. Editing the file on disk without restarting the daemon therefore
changes nothing in the tree, which is the acceptance test.

Publishing is gated on one configuration setting. A host whose rendered
configuration does not turn it on never opens a connection to the datastore,
so the hypervisors, the failover container, and production before its stack
is installed do not attempt a publish they cannot complete. The gate is off
unless stated.

The publish replaces the subtrees the daemon owns in one transaction: the
interface list with its per-family containers and steering properties, and
the translation instances. Whatever an earlier run left there disappears in
the same change that writes the current values, so a reader never sees the
tree empty or half-replaced, and nothing from a retired configuration
survives a restart.

Only values the daemon holds are published. Each steering member appears as
an interface entry named by its link, marked as a member with its tier and
the name of the probe policy that decides its health. Its address-family
containers are present and enabled, because the daemon steers and probes
both families on every member; the internal link appears the same way. Each
member whose loaded configuration carries a translation prefix appears as a
prefix-translation instance with both prefixes explicit. A value the
configuration does not carry is left to the schema default or left out: the
interface type is published as unspecified because the configuration has no
type, the hash mode and member weight are not published because the balancer
that holds them is still the firewall file, and addresses, routes, and
routing tables are not published because the daemon holds them only as live
state, which the live-state piece serves from the operational datastore.

If the datastore cannot be reached or rejects the publish, the daemon logs
the failure and runs exactly as it would have without a management surface.
A publish is bounded in time so a stalled datastore cannot delay startup.

## How live state is served

The same interface-manager process serves the live half of the tree, from
the operational datastore, as a provider: it registers for its owned
subtrees once at startup, right after the configuration publish and behind
the same gate, and each read then reaches into the daemon at request time.
Nothing is copied on a schedule, so the answer cannot drift from the
process. The provider connection stays open for the daemon's lifetime;
losing it is logged and the daemon runs on, exactly like a failed publish.

A provider callback answers from a snapshot the modules already maintain,
never by taking a lock the reconcile path holds and never by probing
on demand. A read is bounded in time, and a reader that stalls or asks for
a malformed path costs the daemon one bounded callback and nothing else.

What each acceptance item reads, and where the value comes from:

- **Delegated prefix.** Each member's currently held delegation, as the
  daemon's prefix tracking sees it, on the member's ipv6 container through
  a leaf the steering module adds. A member holding no delegation has no
  leaf.
- **Health and probe results.** Each member's verdict plus the evidence
  behind it: per-family last result, consecutive failures, and the time of
  the last transition, as steering-module state under the member. The
  health module owns these values today; the provider serves the module's
  snapshot.
- **Active tier and carrier.** The steering group's active tier and, per
  member, whether it is currently carrying traffic, from the routing
  module's last reconciled decision.
- **Translation realized.** Per translation instance, whether the daemon
  found its rules present in the kernel on the last reconcile, as a
  steering-module leaf on the nat instance. The published model has no
  such node, so the local module augments it.
- **Routing session.** Whether the BGP session is established, per peer.
  The speaker lives in the agent process, not the interface manager, so
  the provider answers from a short-lived cache filled over the agent's
  existing local RPC; a dead agent yields an explicit stale marker, never
  a hang, because the cache refresh is bounded and runs outside the
  callback.
- **Intended firewall ruleset.** The model carries one opaque leaf for
  the ruleset the daemon would program, rendered in its own text form;
  it is served once the daemon gains a render hook for it, with the
  remaining modules. Modeling firewall rules structurally is its own
  published-model project and is not this work.

Values the daemon does not hold are not served, and nothing is
reconstructed from files or the kernel at read time beyond the
snapshots above.

The steering module gains the state nodes this needs, marked as
non-configuration so the running datastore is untouched; yanglint gates
the change like any model edit. Streaming of these values is the next
piece; this one serves reads.

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
