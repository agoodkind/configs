# Testbed DNS and NAT64

How name resolution works on the suburban testbed, and how to recover it when a
rebuilt testbed OPNsense comes up with DNS broken. The testbed is IPv6 primary:
its simulated ISPs (LXC 200/201/202) carry IPv4 transit only and no public IPv6,
so IPv6-only guests reach the IPv4 internet through NAT64 plus DNS64. Live
definitions are owned by [opentofu/suburban/](../../../opentofu/suburban/) and the
testbed group vars; update this page when they change.

## Components

| Piece | Where | Address / prefix | Source of truth |
| --- | --- | --- | --- |
| Tayga (NAT64 translator) | testbed OPNsense (VM 101/102), `os-tayga` plugin | v6 prefix `3d06:bad:b01:2664::/96`, tun `nat64` at `3d06:bad:b01:264::ffff:1`, v4 pool `10.250.64.0/24` | `<tayga>` block in the imported `config.xml` (prod prefix `3d06:bad:b01:6464::/96` rewritten to `3d06:bad:b01:2664::/96` by the config transform) |
| DNS64 resolver (bind9) | LXC 464 `dns64-suburban` | `3d06:bad:b01:204::464`, synthesizes AAAA into `2664::/96` | [dns64_suburban_servers.yml](../../../ansible/inventory/group_vars/dns64_suburban_servers.yml), deployed by `deploy-dns64.yml --limit dns64_suburban_servers` |
| Unbound (LAN resolver) | testbed OPNsense | binds all interfaces, `:53` | imported `config.xml` |

The DNS64 LXC forwards upstream to `3d06:bad:b01:2664::101:101`, which is
`1.1.1.1` expressed in the NAT64 prefix, so its own recursion rides the Tayga
path. `dns64_force_synth` is true so dual-stack names also resolve over NAT64
(native testbed IPv6 has no public transit).

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

## Reproducibility gaps

These still need to move from manual recovery into the deploy path:

- The config transform
  ([testbed/opnsense/substitutions.yaml](../../../testbed/opnsense/substitutions.yaml))
  should disable the Unbound DNSBL python module for the testbed (it carries no
  blocklist data), and `deploy-opnsense.yml` should restart Unbound and Tayga
  after the import so they come up without manual `configctl` calls.
- The config transform rewrites the prod Unbound forwarder
  (`3d06:bad:b01:200::53`) to a public resolver (`2606:4700:4700::1111`). On the
  IPv6-only testbed that target is only reachable through NAT64, so forwarding
  still depends on the testbed having working upstream transit; a rebuilt
  OPNsense that has not converged BGP/DNS yet returns no answers until it does.
