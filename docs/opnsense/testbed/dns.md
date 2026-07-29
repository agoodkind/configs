# Testbed DNS and NAT64

How name resolution works on the suburban testbed, and how to recover it when a
rebuilt testbed OPNsense comes up with DNS broken. The testbed is IPv6 primary:
its simulated ISPs carry IPv4 transit only and no public IPv6,
so IPv6-only guests reach the IPv4 internet through NAT64 plus DNS64. Live
definitions are owned by [opentofu/suburban/](../../../opentofu/suburban/) and the
testbed group vars; update this page when they change.

## Components

Three pieces cooperate, and each one's exact values live with the config that
owns them.

Tayga translates between the two families on the testbed OPNsense through the
`os-tayga` plugin. Its prefix and pool come from the `<tayga>` block in the
imported `config.xml`, where the transform rewrites production's prefix to the
testbed's.

The `dns64-suburban` LXC runs bind9 and synthesizes AAAA records into that
prefix, so an IPv4-only name resolves to an address Tayga can translate. Its
address, resolver, and synthesis settings live in
[dns64_suburban_servers.yml](../../../ansible/inventory/group_vars/dns64_suburban_servers.yml)
and deploy with `configs deploy dns64 --limit dns64_suburban_servers`. It
forwards to the testbed OPNsense's Unbound over native IPv6, so its own recursion
no longer rides the Tayga path.

Unbound on the testbed OPNsense answers for the LAN and is configured from the
imported `config.xml`.

Synthesis applies only to names that lack a real AAAA. A dual-stack name keeps
its native address and reaches it over native IPv6, which is what the testbed is
meant to exercise.

## Recovering DNS on a rebuilt testbed OPNsense

A freshly imported testbed OPNsense (host key changes, so use diagnostics-only
relaxed host checking) tends to come up with DNS down for these reasons, in the
order you hit them.

1. **Unbound will not start because the python DNSBL module file is missing.**
   The prod config import leaves Unbound configured with `module-config: "python
   validator iterator"` and `python-script: unbound-dnsbl/dnsbl_module.py`, but
   the rebuilt box has no `dnsbl_module.py`, so Unbound exits at init with
   `fatal error: bad config during init for python module`. DNSBL is
   `<enabled>0</enabled>` in the config yet the python wrapper still renders on
   26.1. Recover by regenerating the module and clearing the stale pid:

   ```sh
   configctl unbound dnsbl       # regenerates /var/unbound/unbound-dnsbl/dnsbl_module.py
   service unbound onestop       # clears the stale pidfile from the crashed start
   service unbound onestart
   ```

2. **Tayga daemon is down.** The `nat64` tun interface and the `2664::/96` route
   can persist while the translator is not running. There is no `rc.d/tayga`
   script (the plugin uses `opnsense-tayga` via configd), so start it with:

   ```sh
   configctl tayga start
   ```

   Verify translation from the OPNsense with `ping6 3d06:bad:b01:2664::1.1.1.1`.

3. **The DNS64 LXC (CT 464) may be stopped.** Check from the suburban hypervisor
   (`root@[3d06:bad:b01:200::1]`) with `pct status 464`, start it with
   `pct start 464`, and confirm `named` runs inside it.

Diagnostic note: `sockstat` and `pgrep` need `sudo` on the OPNsense to show the
root-owned Unbound and Tayga; without it they falsely report nothing on `:53`.

## Access paths

| Target | Path |
| --- | --- |
| Testbed OPNsense | `ssh -J root@[3d06:bad:b01:200::1] agoodkind@10.250.250.2` (ProxyJump through the suburban hypervisor; relax host-key checking after a rebuild) |
| DNS64 LXC (CT 464) | `pct exec 464 ...` from the suburban hypervisor `root@[3d06:bad:b01:200::1]` |
| Testbed MWAN VM 950 | `ssh root@3d06:bad:b01:204::950` |

## How VM 950 reaches DNS

VM 950 management sits on the `vmbrtrunk` `204::` services LAN, the same segment
as the testbed OPNsense MANAGEMENT interface (`opt9`, `3d06:bad:b01:204::1`) and
the DNS64 LXC (`3d06:bad:b01:204::464`). This mirrors production, where the MWAN
VM `enmgmt0` shares the OPNsense LAN `/64` and reaches DNS on-link.
`test_mwan_servers.yml` sets `mwan_dns_servers` to the on-link OPNsense Unbound at
`3d06:bad:b01:204::1`, so VM 950 resolves A records there and reaches them over
its IPv4 WAN. The OPNsense Unbound does not synthesize DNS64; that path is for the
IPv6-only LAN guests that point at the DNS64 LXC instead. The `204::` segment and
the resolver are codified in
[opentofu/suburban/vms.tf](../../../opentofu/suburban/vms.tf) and
[test_mwan_servers.yml](../../../ansible/inventory/group_vars/test_mwan_servers.yml).

After a daemon deploy, `deploy-opnsense.yml` restarts Tayga
(`configctl tayga restart`) and regenerates the Unbound DNSBL python module
before restarting Unbound (`configctl unbound dnsbl` then
`configctl unbound restart`), on prod and testbed. OPNsense core always renders
`python-script: unbound-dnsbl/dnsbl_module.py` into `unbound.conf` even with
DNSBL disabled, so a freshly imported box crashed Unbound at startup until the
module file existed; the deploy now creates it, and the Tayga restart re-installs
NAT64 routes after a WAN outage or import. Neither needs a manual recovery step.

## Reproducibility gaps

These still need to move from manual recovery into the deploy path:

- The config transform rewrites the prod Unbound forwarder
  (`3d06:bad:b01:200::53`) to a public resolver (`2606:4700:4700::1111`). On the
  IPv6-only testbed that target is only reachable through NAT64, so forwarding
  still depends on the testbed having working upstream transit; a rebuilt
  OPNsense that has not converged BGP/DNS yet returns no answers until it does.
