# The network configuration file (MWAN-339 design)

The gateway's network configuration travels in one representation from
inventory to the served tree. Ansible renders it as
`/etc/mwan/network.json` in the model's own JSON encoding, the deploy
validates it against the schema before it lands, and the daemon
validates it again at startup before programming anything. The TOML
configuration file stays beside it and keeps every non-network section.

This design implements epic MWAN-339 through its three child tickets:
MWAN-347 (render), MWAN-348 (load), MWAN-349 (deploy validation). The
epic spec is [config.md](../wanconfig/config.md); its whole-file wording
is amended to this scope in the first implementation PR.

## Scope

`network.json` carries exactly the gateway daemon's network tree, which
is what the management surface describes: the provider inventory, each
provider's routing numbers and translation prefix and source pin, each
provider's health probe settings, and the group-wide translation,
internal-link, and probe-timeout values.

Everything else stays in `config.toml` with no schema node: alert mail,
notify cadence, the agent, the watchdog, the Proxmox API, the OPNsense
sections, the BGP speaker, the failover section, the publish gate, the
host identity leaves, the interface manager's plumbing scalars, the
standalone policy rules (an out-of-band and host role module), and every
credential. No secret ever enters the JSON.

The inventory remains the source of truth: group_vars render the file,
and adding or changing a provider is an inventory edit plus a deploy.
Writing configuration over the management API, with `network.json`
becoming the source of truth, is deferred work tracked as MWAN-440
under the flexible multi-WAN epic.

## The schema

One additive revision of the steering module. Nothing existing changes
shape, name, or type; data valid under the deployed revision stays
valid.

Each provider hangs off the interface that carries it, as a `wan`
presence container augmenting the interface entry, a sibling of the
existing `steering` container:

- `name`: the provider's name, mandatory. The table key of today's
  `[ifmgr.wan.<name>]` becomes this leaf.
- `table-id`, `fw-mark` (both ranged 1 or higher), `fw-mark-prio`,
  `from-prio`: the four routing numbers, ordinary nodes. When the
  provider-set epic (MWAN-324) derives them from a member index, they
  follow the standard deprecation lifecycle.
- `npt-prefix`: the provider's external translation prefix.
- `v4-source`: the IPv4 source pin, a union of address and prefix.
- `health`: a presence container with the provider's own probe
  settings: `enabled`, `ping-count`, the three thresholds,
  `check-interval` (uint32, units seconds), `targets-v4`, `targets-v6`,
  `http-urls`. Health lives inside the provider it probes, mirroring
  the daemon's 1:1 join; there is no shared policy object and no
  reference typing. The existing `probe-policy` leaf is untouched.

Group-wide values join the existing `steering-group` container:
`translation` (internal prefix and the two edge addresses), `routes`
(internal interface and internal IPv4 net), and `health`
(`probe-timeout`, uint32, units milliseconds).

Time spans are integers that name their unit; no duration string type
exists. `tier` stays out of the file: the placement as a sibling of
`steering` avoids that container's mandatory `tier`, and the daemon
keeps deriving tier in code until MWAN-324 makes it configuration.

The sibling `wan` container keeps a deliberate redundancy: the
translation prefixes also appear in the served `ietf-nat` instances the
daemon produces. The configuration leaves are the inputs; the NAT
subtree is output. Writing the configuration as NAT instances instead
would invent instance numbering and repeat the internal prefix once per
provider.

## The renderer (MWAN-347)

A new template, `mwan/config/network.json.j2`, renders
`/etc/mwan/network.json` on the gateway VM at mode 0600, from the same
group_vars that feed the TOML template. No value conversion exists
anywhere: the inventory already stores bare integers (the TOML template
is what appends the `s` suffix today), and the one hardcoded probe
timeout becomes an inventory integer.

The failover container and the hypervisor get no JSON: their roles run
none of the sections the file carries.

In this slice the daemon still loads only TOML, so the rendered JSON is
inert and behavior cannot change. The slice's proof is that both
environments' rendered output validates against the schema.

## The loader (MWAN-348)

At startup the daemon reads `network.json`, validates it against the
schema with the libyang the linux build already links, and fills the
same internal structures the TOML sections fill today. The TOML loader
stops reading the network sections in the same change, so exactly one
file owns each section at every moment.

The failure contract is unchanged: an invalid or missing `network.json`
stops the daemon before it programs anything, and a missing value is a
startup failure, never a silent default. The freebsd build never loads
this file. The failover container's TOML-only load path is untouched.

## Deploy-time validation (MWAN-349)

After rendering and before any file reaches the gateway, the deploy
runs yanglint on the rendered `network.json` against the schema files
staged from the release, the same files the gateway installs and the
daemon validates with at load. One schema, two checkpoints, zero
copies. A failed validation fails the play with the gateway untouched.

This check is the replacement for the pre-flight ruleset parse that the
firewall epic deletes, which is why MWAN-349 carries high priority.

## Cutover and proof

Testbed first, three deploys in order:

1. Render slice: `network.json` appears, unread. Capture the served
   tree, the nft ruleset, routes, and policy rules as the before
   snapshot, and run the traffic matrix below as the before run.
2. Loader slice: the daemon boots reading `network.json`. Capture the
   after snapshot and run the matrix again; every row and every state
   comparison must match the before run. File equivalence is not the
   test; behavior is. This slice also runs the breakage probe: one
   deploy with a deliberately invalid render fails the play and the
   gateway is untouched.
3. Deletion slice: the network sections leave the TOML template and the
   TOML load path. The comparison runs once more.

The traffic and routing matrix, per provider and per address family:

- Forced egress from an internal client exits that provider, and the
  translated source is verified at the provider simulator's ingress,
  because the simulators masquerade outbound and observing beyond them
  proves nothing.
- The reply returns on the same provider, proving ingress-mark
  symmetry.

Group behavior: new connections spread across the preferred tier's
members, with the distribution observed rather than assumed; a fallback
drill forces the preferred tier unhealthy and verifies traffic exits
the fallback member translated in both families, then recovers; pinned
destinations exit their pinned provider; the inbound translation paths
reach their internal targets.

Production repeats render, loader, and deletion under the standing
rules: each command separately approved, the deploy gate's reboot and
egress verdicts, per-provider forced-egress probes in both families,
tree and kernel equivalence, egress announcements to the peer sessions
before any reboot, and the exact reboot window reported afterwards. No
synthetic failover drill runs on production unless the operator orders
one.

## Error handling

- Invalid render: the deploy fails before touching the gateway.
- Invalid or missing file at boot: the daemon stops before programming,
  and the previous kernel state keeps forwarding, which is the existing
  contract for a bad configuration.
- Schema and file disagree only through a bug in one validator, since
  both read the same schema files; the deploy validation failing while
  the load succeeds, or the reverse, is itself a defect to fix, never
  to work around.

## Testing

- Unit: the loader red-green on a valid file, an invalid file, and each
  missing-value case; the renderer's output validated in CI against the
  schema for both environments.
- Deploy: the breakage probe on the testbed.
- Behavior: the traffic matrix and state comparison, before and after,
  on the testbed; the reduced live checks on production.
