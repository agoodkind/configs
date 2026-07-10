# MWAN host layout

MWAN runs as one Go binary spread across a few hosts, and each host runs only the subcommands its role needs. Production runs on the vault hypervisor in San Francisco, and the suburban hypervisor in New Jersey runs a testbed that mirrors it, with the same roles on matching guests.

## Roles and their command surface

The `mwan` binary is a monolith whose subcommands each do one job, and a host runs the subset its role requires.

- The MWAN VM is the WAN router. It runs `mwan agent`, the gRPC service that drives the embedded BGP speaker and applies health-driven route decisions, under the `mwan-agent.service` unit.
- The failover LXC is the backup BGP peer. It runs `mwan agent` and `mwan ifmgr`, the interface manager that applies interface-mode configuration read from `/etc/mwan/config.toml`, under the `mwan-agent.service` and `mwan-ifmgr.service` units.
- The Proxmox host watches and recovers the VM from outside it. It runs `mwan ifmgr` for its own out-of-band interface and `mwan watchdog`, the daemon that probes connectivity and rolls the VM back to a known-good snapshot when a change breaks it. The testbed host additionally runs `mwan opnsense host serve`, the Unix-socket bridge to the testbed OPNsense serial channel.
- The OPNsense VM runs `mwan opnsense serve`, the FreeBSD daemon that edits `config.xml` over the serial channel. It has no `/etc/mwan/`; its settings live in `rc.conf.d`.

The ISP-simulator containers and the unrelated service containers on these hosts run no MWAN command.

## Binary rollout order

Roll a new MWAN binary onto the testbed first and production second, and verify each host before moving to the next.

1. suburban host
2. testbed MWAN VM
3. testbed failover LXC
4. testbed OPNsense
5. production failover LXC
6. production MWAN VM
7. vault host
8. production OPNsense

A production step needs a live verification and a saved rollback copy of the binary before the swap.

## WAN links

MWAN drives three wide-area networks. It load-balances outbound traffic across AT&T and Webpass and uses Monkeybrains only as a health fallback. That WAN selection is policy routing on the active router, separate from the BGP-based failover between the active router and its standby.

- `enwebpass0` carries Webpass, a Google Fiber line. It takes a dynamic carrier-grade NAT IPv4 address and a provider-delegated IPv6 prefix.
- `enatt0.3242` carries AT&T over an 802.1X-authenticated VLAN, and takes a dynamic IPv4 address and an AT&T-delegated IPv6 prefix.
- `enmbrains0` carries Monkeybrains as the lossy fallback. It takes a public static IPv4 address, a SLAAC IPv6 address, and a DHCPv6 prefix delegation, and MWAN maps its internal IPv6 range onto the first block of that delegation with network prefix translation. The delegation renumbers, so `find-pd-prefixes.sh` reads the live prefix rather than a stored one.

The untagged parent interface `enatt0` carries the management link to the AT&T optical network terminal and is the Layer 2 parent of the tagged `enatt0.3242`. That terminal's access and the 802.1X bring-up chain are in [ont.md](ont.md).
