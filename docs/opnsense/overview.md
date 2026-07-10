# OPNsense router

The production OPNsense router is the LAN router and services edge for goodkind.io, and it is not the WAN edge. All upstream traffic flows through the MWAN router, which OPNsense treats as its only upstream, for both IPv4 and IPv6. This page describes the router's role and shape; it is a point-in-time snapshot, and the interface addressing lives in the router's own config, so the addresses can be stale.

The rest of the area covers the router in depth. Its operating contract, the BGP steady state and the gateway and NAT foot-guns, is in [operations.md](operations.md). Its out-of-band serial daemon is in [daemon.md](daemon.md), importing a config is in [import.md](import.md), and reaching a UI page over an SSH forward is in [ui.md](ui.md).

## Role and interfaces

OPNsense runs as a QEMU VM on the vault hypervisor. It presents a management LAN for the containers and core services, an uplink toward the MWAN router, and a set of VLANs for IoT and UniFi management, for the physical devices such as the workstations and the NAS, for home automation, and for guest and captive-portal traffic. It also hosts the WireGuard hub interface and the NAT64 translator interface. The guest VLAN carries no IPv6 by design.

## Upstream routing

OPNsense does not hold the WAN uplinks. The FRR routing daemon installs the default route from BGP toward the MWAN router for both address families, so outbound failover and delegated-prefix handling happen on MWAN, not here. Downstream uses the stable internal IPv6 block, and MWAN translates the internal segments onto provider-delegated space for outbound reachability.

## Services anchored here

OPNsense owns Unbound for DNS resolution, the DHCP servers, WireGuard, and the LAN-facing interface addressing. Unbound forwards upstream to NextDNS. The DHCP reservations for the physical devices and the home-automation controller live in the router config.

## WireGuard peers

The router is the WireGuard hub. The meaningful infrastructure peer is the suburban testbed hypervisor, and the rest are client access paths for the workstations and a phone. The berylax peer is historical, because berylax is offline.
