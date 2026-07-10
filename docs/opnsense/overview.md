# OPNsense router

OPNsense is the LAN router and services edge for goodkind.io, the box every household device talks to and that hands all outbound traffic to the MWAN router upstream. It is not the WAN edge. The wide-area links, the failover between them, and the translation onto each provider's space all live on MWAN, so OPNsense treats MWAN as its one gateway for both IPv4 and IPv6. Its interfaces, addresses, VLANs, DHCP reservations, and aliases live in the router's own configuration, not on this page.

## LAN

OPNsense provides the household LAN and separates it into trust zones. It terminates the WireGuard tunnels and runs the NAT64 translator that lets IPv6-only clients reach the IPv4 internet. The guest and captive-portal zone carries no IPv6, by design.

## Upstream routing

Because the WAN links live on MWAN, OPNsense never installs a static default route of its own. Its FRR routing daemon learns the default from MWAN over BGP for both address families and installs it in the kernel, which leaves outbound failover and delegated-prefix handling entirely to MWAN. Downstream, the household uses a stable internal IPv6 block, and MWAN rewrites that block onto each provider's delegated space on the way out.

## Anchored services

A few services run on the router itself rather than on a guest. OPNsense runs Unbound as the household resolver, forwarding upstream queries to NextDNS, and the DHCP servers for the LAN.

## WireGuard

OPNsense is the single WireGuard hub for the homelab, so every roaming tunnel terminates on it. The tunnel that matters for infrastructure connects the suburban testbed hypervisor back to production, and the rest are personal access paths that let a few laptops and a phone reach the network from outside. An old tunnel to berylax remains in the configuration but does nothing while berylax is offline.
