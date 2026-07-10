# Infrastructure overview

This area records the current state of the goodkind.io homelab: the hypervisors, the hosts that are not guests, the network, and how to reach any of them. It is a point-in-time snapshot rather than a live feed, so when a page here disagrees with a live host, the live host wins, and you read the host before you change production.

The vault hypervisor in San Francisco runs production, and [vault.md](vault.md) records its containers, VMs, and host services. Hosts that are not vault guests, including the suburban testbed hypervisor, the workstations, and the NAS, are in [hosts.md](hosts.md), and the offline berylax device keeps its own record in [berylax.md](berylax.md). The Cloudflare account, its tunnels, and its DNS are in [cloudflare.md](cloudflare.md), and the emergency out-of-band access paths are in [oob.md](oob.md).

Three pages support that state work rather than describing a host. [access.md](access.md) covers how to reach a host and which entry point to prefer, [network.md](network.md) covers diagnosing IPv6 and DHCP, and [wireguard.md](wireguard.md) covers the WireGuard roaming behavior.

MWAN and OPNsense are large enough to own their own areas. The multi-WAN router lives under [docs/mwan/](../mwan/), and the OPNsense router and its out-of-band daemon live under [docs/opnsense/](../opnsense/).
