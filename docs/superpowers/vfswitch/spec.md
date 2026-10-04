# Hardware-switched link between router containers

This design connects an MWAN gateway container and a router container through the Broadcom network card on poweredge. The router runs Kea, Unbound, and nftables. The design does not use a Linux bridge between the containers.

## Terms

A port is one of the four SFP ports on the card: `nic0` to `nic3`.

A virtual function (VF) is an extra network device that the card creates on a
port. The card switches frames between the VFs of one port.

## Design

### Who owns each port

| Port | Use | Owner |
| --- | --- | --- |
| `ens1f0` | Proxmox management | Host |
| `nic2` | WAN in | Container 1 |
| `nic0` | LAN out | Container 2 |
| `nic1` | Link between the containers | VF 0 to container 1, VF 1 to container 2 |
| `nic3`, `ens1f1` | Unused | Host |

The host must not assign addresses to nic0 through nic3 or add these ports to a bridge.

### The link between the containers

Configure two virtual functions on nic1. Assign each virtual function a fixed MAC address and force its link state up. The physical port does not have a cable. The card must switch Ethernet frames between the virtual functions.

The containers share the host kernel. Each container has a separate network namespace for routing and filtering.

### How a container gets its devices

Proxmox assigns the existing port and virtual function to the container network namespace at startup. Proxmox returns both devices to the host when the container stops. These interfaces do not use veth pairs.

### Container 1: MWAN

- Container 1 is unprivileged.
- Container 1 has two devices: `wan` (port `nic2`) and `mwanbr` (VF 0).
- Container 1 does not have a management interface. The host executes container commands with pct.
- MWAN finds each device by name. The container config sets the names `wan`
  and `mwanbr`. MWAN does not look up a MAC address and does not rename a
  device in a container.
- MWAN loads eBPF programs. An unprivileged container needs a BPF token for
  the load. Proxmox mounts a BPF filesystem with delegation in container 1 at
  each start.
- A rollback of container 1 uses `pct snapshot` and `pct rollback`.

### Container 2: router

- Container 2 is unprivileged.
- Container 2 has two devices: `lan` (port `nic0`) and `mwanbr` (VF 1).
- Container 2 runs Kea (DHCP), Unbound (DNS), and nftables.
- Kea and Unbound are installed and stopped. `nic0` is on the live LAN, and a
  second DHCP server on that LAN answers real clients.

### Permissions

The completed implementation must use the automation token and scoped overlay privileges for Proxmox operations. It must not require a root login.

| Step | Overlay privilege |
| --- | --- |
| Create the VFs and set MAC and link state | `Sys.SRIOV.Modify` |
| Give a container a port or a VF | `VM.Config.HostNIC` and `Sys.HostNIC.Use` |
| Mount the BPF filesystem with delegation | `VM.Config.BPFDelegate` |
| Run commands and write files in a container | `VM.Guest.Exec`, `VM.Guest.FileRead`, `VM.Guest.FileWrite` |

The recorded tests used root commands. Operation through the automation token remains an acceptance requirement.

## Test results

Poweredge, 2026-10-04, card firmware 236.1.173.0, kernel 7.0.14-20-pve.

### Card

| Test | Result |
| --- | --- |
| VFs per port after SR-IOV is switched on | 8 |
| Create VFs on a port that is set down | Rejected by the driver |
| Create 2 VFs on `nic1` (no cable), force link up | Both links up |
| Each SFP port is in its own IOMMU group | Yes |

### Link

| Test | Result |
| --- | --- |
| Two containers start, each with one port and one VF | Pass |
| Ping between the containers | 5 of 5, about 0.2 ms |
| Bridge on the host | None |
| Host receive counter on `nic1` during the ping | Increased by 1 |
| Stop a container | Its devices return to the host |
| Start it again | Its devices move back in |

### MWAN needs, in an unprivileged container

| Test | Result |
| --- | --- |
| sysctl writes under `/proc/sys/net` | Pass |
| Policy rule and route in a separate table | Pass |
| nftables with conntrack, NAT, and marks | Pass |
| Load an eBPF tc program, no token | Fail |
| Load an eBPF tc program, with the token mount | Pass |
| Load and attach the real MWAN NPT programs, with a narrow token mount | Pass |

The test mount permitted the following BPF operations.

| Kind | Allowed |
| --- | --- |
| Commands | `prog_load`, `map_create`, `btf_load` |
| Map types | `hash` |
| Program types | `sched_cls`, `socket_filter` |
| Attach types | `tcx_ingress`, `tcx_egress`, `cgroup_inet_ingress` |

### Speed of rollback

| Step | Time |
| --- | --- |
| `pct snapshot` | 0.8 s |
| `pct rollback` and start | 3.4 s |
| `pct reboot` | 3.1 s |

## Not tested yet

- The VFs after a reboot of poweredge.
- MWAN in a container with devices found by name. The production VM runs
  MWAN in that mode. No container has run it.
- The full `mwan` program in the container. The test loaded only the NPT
  eBPF programs.
- The MWAN deploy for a container. The deploy supports only a VM.

## Limits

- The switch in the card stays in `legacy` mode. `switchdev` mode with
  offloaded routing rules is separate work.
- `ens1f0` and `ens1f1` are in one IOMMU group. Both stay on the host.
- A change of the SR-IOV setting needs a reboot of poweredge.

## Done when

1. After a host reboot, nic1 has two virtual functions with the configured MAC addresses and enabled links.
2. Container 1 has nic2 and virtual function 0. Container 2 has nic0 and virtual function 1.
3. The containers exchange ping replies without a host bridge.
4. The nic1 host receive counter does not increase for those ping frames.
5. A stopped container returns its devices, and a start takes them again.
6. The host management address remains configured on ens1f0 throughout the test.
7. MWAN runs in the unprivileged gateway container with NPTv6 loaded.
8. The completed automation executes all Proxmox operations through the scoped token privileges.
