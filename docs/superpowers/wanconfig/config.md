# Two: the configuration format

The gateway's network configuration becomes the model's own JSON
encoding, so one representation runs from inventory through to the
served tree for everything the management surface describes. Ansible
renders it as `/etc/mwan/network.json`; the daemon validates it against
the schema at startup; the deploy validates it with the same schema
before it ever reaches the gateway. The TOML configuration file stays
beside it and keeps every non-network section.

Depends on the model existing and being served, because the encoding is
the model's and the served tree is how the change is verified.

## Scope

`network.json` carries exactly the gateway daemon's network tree: the
provider inventory, each provider's routing numbers, translation prefix,
IPv4 source pin, and health probe settings, and the group-wide
translation, internal-link, and probe-timeout values. Each provider's
configuration hangs off the interface that carries it, and health
settings live inside the provider they probe, one to one, with no shared
policy object.

Everything else stays in `config.toml` with no schema node: alert mail,
notify cadence, the agent, the watchdog, the Proxmox API, the OPNsense
sections, the BGP speaker, the failover section, the publish gate, the
host identity leaves, the interface manager's plumbing scalars, the
standalone policy rules, and every credential. No secret ever enters the
JSON. The daemon reads both files, and exactly one file owns each
section.

The inventory remains the source of truth: group_vars render the file,
and a provider change is an inventory edit plus a deploy. No value
conversion exists anywhere, because the inventory stores bare integers
and the schema's time spans are integers that name their unit. Writing
configuration over the management API, with `network.json` becoming the
source of truth, is deferred work (MWAN-440).

## Why the format changes

Two representations of the same thing is the problem this work exists to
remove. A model that describes the daemon while the daemon loads a
differently shaped file recreates it one layer up, and the two drift the
moment someone edits one of them.

There is a second gain. A file in the model's encoding can be checked
against the schema before it ever reaches the gateway. That matters
because the piece that moves firewall ownership into the daemon deletes
the only pre-flight validation that exists today, a check-mode parse of
the rendered ruleset on the target before it lands. Schema validation of
the rendered configuration replaces it.

## What changes

Inventory renders `network.json` in the model's encoding. The daemon
loads it at startup and validates it against the schema before acting on
any of it. A file that does not validate stops the daemon before it
programs anything, which is the existing failure contract. The TOML
loader stops reading a network section in the same change the JSON
loader starts owning it, so no state exists where both files feed one
setting.

The per-provider catalogue lives in the shared inventory group, keyed by
environment, because the rollback watchdog's probe list is rendered on
the hypervisor and that variable group cannot read the gateway's. A
catalogue in the gateway's own group would leave that list
hand-maintained and free to drift, which is how it came to omit a
provider.

The environment file stays. The pinned-destination refresher and the
whole 802.1X chain read it, and it hardcodes both the variable names and
the address set names, so retiring it is separate work.

## What must not change

The daemon's behavior, given equivalent input. This piece changes how
the network configuration is written and read, not what any value means.

Validation must remain strict in the same places it is strict today. A
missing value is a load-time failure, not a defaulted one. The model
makes that easier to enforce, since a leaf is either present or it is
not.

The failover container and the hypervisor keep their TOML-only
configuration untouched; their roles run none of the sections the JSON
carries.

## Acceptance

Both environments render a `network.json` that validates against the
schema.

Rendering an invalid configuration fails in the deploy, before the file
reaches the gateway, and the gateway is untouched.

On the same host, the served tree and the programmed rules, routes, and
policy rules are unchanged across the cutover. File equivalence is not
the test, because the file is a different format by design. Behavioral
equivalence is the test, proven by a traffic and routing matrix run
before and after the cutover on the testbed: forced egress per provider
and per address family with the translated source observed at the
provider simulator's ingress, reply symmetry, observed balancer
distribution, a fallback drill in both families, pinned destinations,
and the inbound translation paths.

The network tree is deleted from the TOML render and load paths;
`config.toml` keeps only the non-network sections.

## Failure modes

A file that validates but means something different from its TOML
predecessor is the risk this piece carries, and the served tree plus the
traffic matrix are what catch it. Compare both, before and after, on the
same host.

Schema validation at deploy time and at load time must use the same
schema files. Two copies of a schema is the same duplication failure in
a new place. The deploy validates with the schema staged from the
release, which is the same artifact the gateway installs.
