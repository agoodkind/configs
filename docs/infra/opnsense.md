# Production OPNsense State

This file records the current production OPNsense role and topology. It is a
state document, not the architectural source of truth for MWAN behavior. For
steady-state BGP and operational foot-guns, use
[../opnsense/operational-notes.md](../opnsense/operational-notes.md). For the
current testbed baseline and import workflow, use
[../opnsense/testbed-baseline.md](../opnsense/testbed-baseline.md) and
[../opnsense/testbed-config-import.md](../opnsense/testbed-config-import.md). For the
out-of-band serial control channel into this router, use the
[OPNsense OOB daemon](../opnsense/daemon.md).

## Role

Production OPNsense runs as QEMU VM `101` on `vault`. It is the LAN router and
services edge, but it is not the WAN edge. All upstream Internet traffic flows
through the production MWAN VM, and OPNsense treats that VM as its upstream for
both IPv4 and IPv6.

## Interfaces

| Interface | Role | IPv4 | IPv6 |
| --------- | ---- | ---- | ---- |
| `vtnet0` | Management LAN for containers and core services | `10.250.0.1/24` | `3d06:bad:b01::1/64` |
| `vtnet1` | Uplink toward the MWAN VM | `10.250.250.2/29` | `3d06:bad:b01:fe::2/64` |
| `iavf0` | IoT and UniFi management | `10.250.4.1/24` | `3d06:bad:b01:4::1/64` |
| `vlan0100` | Physical devices such as `mini` and `nas` | `10.250.1.1/24` | `3d06:bad:b01:1::1/64` |
| `vlan0200` | Home automation segment | `10.250.2.1/24` | `3d06:bad:b01:2::1/64` |
| `vlan0300` | Guest and captive-portal segment | `10.250.3.1/24` | None by design |
| `wg0` | WireGuard hub | `10.250.10.1/24`, `10.240.10.2/24` | `3d06:bad:b01:10::1/64`, `3d06:bad:b01:a::1/64` |
| `nat64` | Tayga NAT64 | `10.250.46.1`, translated toward `10.250.64.1` | `3d06:bad:b01:64::ffff:1/128`, prefix `3d06:bad:b01:6464::/96` |

## Upstream routing

The default IPv4 route points at `10.250.250.1` on `vtnet1`. The default IPv6
route points at the MWAN-side link-local next hop on `vtnet1`. OPNsense does
not hold the WAN uplinks directly, so outbound failover and delegated-prefix
handling happen on MWAN, not on OPNsense.

The internal IPv6 space remains `3d06:bad:b01::/48`. The repo treats that block
as the stable internal prefix, and the MWAN VM applies NPT to map internal
segments onto provider-delegated space for outbound reachability.

## Services anchored here

OPNsense still owns Unbound, DHCP, WireGuard, and the LAN-facing interface
address plan.

Notable current DHCP and DNS state:

- `mini` is reserved at `10.250.1.2` and `3d06:bad:b01:1::2`.
- `nas` is reserved at `3d06:bad:b01:1::3`, and the `nas_host` alias follows
  that address.
- `home-assistant` is reserved at `10.250.2.3` and `3d06:bad:b01:2::3`.
- Unbound forwards upstream queries to NextDNS.

## WireGuard peers

The current configured peers are `alexs-mba`, `alexs-iphone`, `suburban`,
`berylax`, and `alexs-mbp`.

`suburban` is the meaningful infrastructure peer. The laptop and phone peers
are client access paths. `berylax` remains offline, so its peer config is only
historical state.
