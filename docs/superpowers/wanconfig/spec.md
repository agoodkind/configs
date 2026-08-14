# A standards-modeled MWAN gateway

The MWAN gateway VM terminates several internet provider links and steers
traffic across them. It has no model. Each behavior was added where it was
needed, so IPv4 and IPv6 reach the same goal by different mechanisms, owned
by different components, sharing no vocabulary. Adding a provider takes about
forty scattered inventory edits, four hand-written link files, new rules in a
240-line firewall template, and Go changes, and two validators reject a
fourth provider outright.

This work adopts an existing standard model rather than inventing one. Every
provider becomes an interface with a container per address family, carrying
its own addressing, forwarding, and translation. Translation becomes an
instance with a declared type, so prefix translation, address masquerade,
one-to-one mapping, and no translation at all are values instead of code
paths. Steering becomes members with a tier, a weight, and a probe policy.
The daemon then serves that model, so an operator can answer why traffic is
leaving a given provider without logging in.

The model is defined once in [model.md](model.md). Everything below is
expressed in its terms.

OPNsense at `router.home.goodkind.io` sits behind the gateway and is not
modified by any part of this work.

## Decisions that bind every piece

**Read-only.** The served model exposes configuration as loaded and live
state. It accepts no writes, so the deploy path and its rollback keep their
role and configuration still applies at daemon restart. The library provides
no staged-change workflow, which is the only safe way to accept writes, so
writes wait for one.

**RESTCONF and gNMI.** RESTCONF for reading the tree, gNMI for subscribing to
state that today can only be polled. NETCONF is out: its distinguishing
feature is staged change, which read-only does not use.

**The model's own encoding is the configuration format.** Inventory renders
the model as JSON rather than as TOML, so one representation runs from
inventory through to the served tree.

**The daemon is the only thing that writes the firewall.** The ruleset file
is deleted and the firewall service is masked.

**Nothing is written from scratch.** `freeconf` parses the model, serves
RESTCONF, and speaks gNMI; `openconfig/ygot` is the fallback if binding by
reflection proves insufficient. Do not run both.

## The five pieces

Each has its own specification and its own implementation plan. They are
listed in dependency order, and each one is deployable on its own.

**One, the model and the read-only surface.** Define the model for the whole
daemon, bind it, and serve it against today's configuration. Changes no
behavior. Every later piece is verified against it.
[surface.md](surface.md)

**Two, the configuration format.** Inventory renders the model's JSON
encoding, the daemon loads it, and TOML retires. The rendered file becomes
checkable against the schema before it reaches the gateway.
[config.md](config.md)

**Three, the provider set becomes data.** Inventory takes the model's shape,
routing identifiers derive from a member index, steering becomes tier and
weight, and one renderer builds every link. Adding a provider becomes an
inventory edit and a config deploy. [providers.md](providers.md)

**Four, translation becomes typed instances.** Each family of each provider
declares its translation type, one-to-one mapping works in both families, and
prefixes of differing length follow the standard's rule rather than a
hardcoded length. [translation.md](translation.md)

**Five, the daemon owns the firewall.** The ruleset file is deleted, the
daemon takes the pre-network slot and programs a closed baseline before
anything else, and the kernel backends it needs are declared for load. The
only piece that touches live traffic. [firewall.md](firewall.md)

## Out of scope

Writing configuration over the management interface, and the hot reload that
would come with it. Both need a staged-change workflow the library does not
provide.

Quality-based steering, meaning selection on latency, jitter, or loss.

Load balancing within an activated fallback tier. Mark assignment is computed
from the top tier, so a fallback tier serves through one member at a time.

Splitting the health verdict per address family. A provider with dead IPv6
and working IPv4 currently reads healthy and keeps receiving IPv6 traffic.
Fixing it changes the health state file format and every consumer's
signature, which would break the behavioral equivalence the migration relies
on. It has its own ticket and follows this work.

The daemon running its own delegation client, and moving link creation off
systemd networkd. The second is gated on the first, because splitting link
creation from lease ownership would put one interface under two authorities.
