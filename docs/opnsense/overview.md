# OPNsense router

OPNsense is the LAN router and services edge for goodkind.io, the box that every device on the household network talks to and that in turn hands all of its outbound traffic to the MWAN router upstream. It is not the WAN edge itself. The wide-area links, the failover between them, and the address translation onto each provider's space all live on MWAN, so OPNsense treats MWAN as its one gateway for both IPv4 and IPv6. This page sketches the router as it stands today, and the interface addressing it describes lives in the router's own configuration, so read those numbers as a snapshot that can lag the running box.

## Interfaces

Behind that single upstream, OPNsense fans traffic out across a management LAN for the containers and core services, a direct uplink toward the MWAN router, and several VLANs that separate the network by trust. One VLAN carries IoT devices and UniFi management, one carries the physical machines such as the workstations and the storage box, one carries home automation, and one carries guests and the captive portal. The router also terminates the WireGuard tunnels and hosts the NAT64 translator that lets IPv6-only clients reach the IPv4 internet. The guest VLAN carries no IPv6 at all, by design.

## Upstream routing

Because the WAN links live on MWAN, OPNsense never installs a static default route of its own. Its FRR routing daemon learns the default from MWAN over BGP for both address families and installs it in the kernel, which leaves outbound failover and delegated-prefix handling entirely to MWAN. Downstream, the household uses a stable internal IPv6 block, and MWAN rewrites that block onto each provider's delegated space on the way out.

## Anchored services

A few services live on the router rather than on a guest. OPNsense runs Unbound as the household resolver and forwards its queries upstream to NextDNS, and it runs the DHCP servers that hand out addresses and hold the reservations for the physical devices and the home-automation controller. It also owns the LAN-facing addressing plan and the WireGuard configuration.

## WireGuard

OPNsense is the single WireGuard hub for the homelab, so every roaming tunnel terminates on it. The tunnel that matters for infrastructure connects the suburban testbed hypervisor back to production, and the rest are personal access paths that let a few laptops and a phone reach the network from outside. An old tunnel to berylax remains in the configuration but does nothing while berylax is offline.
