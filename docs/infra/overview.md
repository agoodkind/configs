# Infrastructure overview

This directory is the point-in-time infrastructure snapshot for `goodkind.io`.
Treat every IP address, route, bridge, and service state here as potentially
stale, because these files are not a live feed.

Use this directory for current state and inventory-shaped facts. Use
[docs/ansible/](../ansible/) for Ansible contracts and policies,
[docs/mwan/](../mwan/) for MWAN architecture and coding rules, and
[docs/opnsense/](../opnsense/) for OPNsense operational notes and import
runbooks.

## Current state docs

- [vault.md](vault.md): Proxmox vault hypervisor, vault LXCs, QEMU VMs, stopped
  VMs, and vault host services.
- [mwan-layout.md](mwan-layout.md): MWAN command surfaces, per-host runtime
  layout, rollout order, and WAN links.
- [suburban-testbed.md](suburban-testbed.md): suburban bridges, testbed guests,
  and production versus testbed shape.
- [opnsense.md](opnsense.md): current production OPNsense topology and its role
  relative to MWAN.
- [hosts.md](hosts.md): non-vault host state, including suburban itself, mini,
  NAS, and historical berylax notes.
- [cloudflare.md](cloudflare.md): Cloudflare account, tunnels, WARP routes,
  load balancers, Pages, Workers, email routing, and DNS records.
- [oob.md](oob.md): emergency out-of-band access state.

## Reference docs that support state work

- [access.md](access.md): SSH entry points and host access patterns.
- [network.md](network.md): IPv6 and DHCP diagnosis rules and workflows.
- [berylax.md](berylax.md): historical berylax host and serial-console notes.
- [wireguard-roaming.md](wireguard-roaming.md): WireGuard roaming research and
  split-brain analysis.

## Operating note

If a state doc conflicts with a live host, the live host wins. Read the host
before changing production. The repo rules for that live-first workflow are in
[AGENTS.md](../../AGENTS.md).
