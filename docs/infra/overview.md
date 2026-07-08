# Infrastructure overview

This directory holds the point-in-time infrastructure snapshot for `goodkind.io`.
Every IP address, route, bridge, and service state here can be stale, because
these files are not a live feed. When a state doc conflicts with a live host, the
live host wins, so read the host before changing production. The live-first
workflow rules are in [AGENTS.md](../../AGENTS.md).

Production runs on the vault Proxmox hypervisor in San Francisco, whose LXCs, VMs,
and host services are in [vault.md](vault.md). Other hosts, including the suburban
testbed hypervisor, the mini, the NAS, and the offline berylax, are in
[hosts.md](hosts.md), with the deeper berylax history in [berylax.md](berylax.md).
The production OPNsense router and its role relative to MWAN are in
[opnsense.md](opnsense.md), and MWAN itself lives under [docs/mwan/](../mwan/).
Cloudflare tunnels, WARP routes, load balancers, and DNS are in
[cloudflare.md](cloudflare.md), and emergency out-of-band access is in
[oob.md](oob.md).

Three docs support that state work rather than describing a host:
[access.md](access.md) for SSH entry points and jump-host patterns,
[network.md](network.md) for IPv6 and DHCP diagnosis, and
[wireguard.md](wireguard.md) for WireGuard roaming research.
