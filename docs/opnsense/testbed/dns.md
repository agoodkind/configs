# Testbed DNS and NAT64

Name resolution on the suburban testbed mirrors production: guests resolve names,
reach IPv6 destinations natively, and reach IPv4-only destinations through NAT64.
This page explains that path and how to recover it when a rebuilt testbed OPNsense
comes up with DNS broken.

Two things make native IPv6 work. Each testbed LAN `/64` sits inside the aggregate
prefix that MWAN translates, set by `mwan_internal_prefix` in
[test_mwan_servers.yml](../../../ansible/inventory/group_vars/test_mwan_servers.yml),
and the simulated ISPs masquerade IPv6 out to the real uplink
([nftables.conf.j2](../../../testbed/isp-lxc/nftables.conf.j2)). A LAN placed
outside that aggregate gets no native IPv6 egress, because the translation only
covers the aggregate.

The live guest shape is owned by [opentofu/suburban/](../../../opentofu/suburban/),
guest addresses by
[service_mapping.yml](../../../ansible/inventory/group_vars/all/service_mapping.yml),
and the OPNsense interface prefixes by the config transform
([substitutions.yaml](../../../testbed/opnsense/substitutions.yaml)).

## Components

| Piece | Where | Role | Source of truth |
| --- | --- | --- | --- |
| Tayga (NAT64 translator) | testbed OPNsense, `os-tayga` plugin | Translates the NAT64 prefix to IPv4 over the `nat64` tun | `<tayga>` block in the imported config.xml; the transform rewrites the prod prefix to the testbed one |
| DNS64 resolver (bind9) | LXC 464 `dns64-suburban` | Synthesizes AAAA for IPv4-only names into the NAT64 prefix | [dns64_suburban_servers.yml](../../../ansible/inventory/group_vars/dns64_suburban_servers.yml), deployed with `configs deploy dns64 --limit dns64_suburban_servers` |
| Unbound (LAN resolver) | testbed OPNsense | Resolves for on-link hosts, binds all interfaces on `:53`, no DNS64 synthesis | imported config.xml |

The DNS64 LXC forwards upstream to Cloudflare expressed in the NAT64 prefix, so its
own recursion rides the Tayga path and reaches an IPv4-only resolver.
`dns64_force_synth` is off, matching production, so a dual-stack name keeps its
native AAAA and the client uses native IPv6. Only IPv4-only names get a
synthesized address. Turning synthesis on hides a broken native-IPv6 path instead
of fixing it.

## Recover DNS on a rebuilt testbed OPNsense

A freshly imported testbed OPNsense changes host keys, so use diagnostics-only
relaxed host-key checking. `deploy-opnsense.yml` now handles the first two
failures below on every run, for prod and testbed: it regenerates the Unbound
DNSBL python module and restarts Unbound, then restarts Tayga. Run the manual
steps only when you are recovering without a deploy.

1. **Unbound will not start because the python DNSBL module file is missing.**
   The prod config import leaves Unbound configured with `module-config: "python
   validator iterator"` and `python-script: unbound-dnsbl/dnsbl_module.py`, and
   OPNsense core renders that wrapper even with DNSBL disabled. A rebuilt box has
   no `dnsbl_module.py`, so Unbound exits at init with `fatal error: bad config
   during init for python module`. Regenerate the module and clear the stale pid:

   ```sh
   configctl unbound dnsbl       # regenerates /var/unbound/unbound-dnsbl/dnsbl_module.py
   service unbound onestop       # clears the stale pidfile from the crashed start
   service unbound onestart
   ```

2. **Tayga is not translating.** The `nat64` tun interface and the NAT64 prefix
   route can persist while the translator is stopped or holding stale routes.
   There is no `rc.d/tayga` script, because the plugin drives `opnsense-tayga`
   through configd, so use:

   ```sh
   configctl tayga restart
   ```

   Verify translation from the OPNsense by pinging an IPv4 address expressed in
   the configured NAT64 prefix, for example `ping6 3d06:bad:b01:2664::1.1.1.1`.

3. **The DNS64 LXC (CT 464) may be stopped.** From the suburban hypervisor, check
   `pct status 464`, start it with `pct start 464`, and confirm `named` runs
   inside it.

If NAT64 fails while the translator is healthy, check the WAN path before the
translator. Tayga translating into a WAN that cannot forward looks identical to a
NAT64 fault from the guest.

Diagnostic note: `sockstat` and `pgrep` need `sudo` on the OPNsense to show the
root-owned Unbound and Tayga; without it they falsely report nothing on `:53`.

## Access paths

| Target | Path |
| --- | --- |
| Testbed OPNsense | ProxyJump through the suburban hypervisor to the OPNsense WAN address (`opnsense_test` in service_mapping.yml). Relax host-key checking after a rebuild. |
| DNS64 LXC (CT 464) | `pct exec 464 ...` from the suburban hypervisor |
| Testbed MWAN VM 950 | SSH directly to its management address (`test_mwan` in service_mapping.yml) |

## How VM 950 reaches DNS

VM 950 management sits on the `vmbrtrunk` services LAN, the same segment as the
testbed OPNsense MANAGEMENT interface and the DNS64 LXC. This mirrors production,
where the MWAN VM `enmgmt0` shares the OPNsense LAN `/64` and reaches DNS on-link.
`mwan_dns_servers` in
[test_mwan_servers.yml](../../../ansible/inventory/group_vars/test_mwan_servers.yml)
points VM 950 at the on-link OPNsense Unbound, so it resolves A records there and
reaches them over its IPv4 WAN. That Unbound does not synthesize DNS64; the DNS64
LXC serves the IPv6-only LAN guests instead.

## Reproducibility gaps

The config transform rewrites the prod Unbound forwarder to a public resolver. On
the testbed that target is reachable only through NAT64, so forwarding still
depends on the testbed having working upstream transit. A rebuilt OPNsense that
has not converged BGP returns no answers until it does.
