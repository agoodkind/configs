# Two router containers on poweredge, linked by the network card

Poweredge runs two containers. Container 1 is the MWAN gateway. Container 2 is
a small router with DHCP and DNS. The Broadcom network card links them. No
Linux bridge is used.

## Why

A Linux bridge copies every frame on a host CPU. The card has a switch built
into each port, and that switch can do the same job in hardware.

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

The host puts no address and no bridge on `nic0` to `nic3`.

### The link between the containers

1. `nic1` has two VFs.
2. Each VF has a fixed MAC address.
3. Each VF has its link forced up. `nic1` has no cable.
4. The card switches frames between the two VFs. The host does not see them.

Each container still routes and filters in its own kernel. Only the link is in
hardware.

### How a container gets its devices

A container gets a whole port or a VF as a normal network device. Proxmox
moves the device into the container at start and back to the host at stop. No
`veth` pair exists.

### Container 1: MWAN

- Container 1 is unprivileged.
- Container 1 has two devices: `wan` (port `nic2`) and `mwanbr` (VF 0).
- Container 1 has no management interface. The host runs commands in
  container 1 with `pct`.
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

Every Proxmox step uses the automation token and a narrow overlay privilege.
No step uses a root login.

| Step | Overlay privilege |
| --- | --- |
| Create the VFs and set MAC and link state | `Sys.SRIOV.Modify` |
| Give a container a port or a VF | `VM.Config.HostNIC` and `Sys.HostNIC.Use` |
| Mount the BPF filesystem with delegation | `VM.Config.BPFDelegate` |
| Run commands and write files in a container | `VM.Guest.Exec`, `VM.Guest.FileRead`, `VM.Guest.FileWrite` |

The overlay does not have these four yet. The test results below came from
hand steps as root.

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

The narrow token mount allows these and no more:

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

1. After a reboot, `nic1` has two VFs with the fixed MACs and link up.
2. Container 1 has `nic2` and VF 0. Container 2 has `nic0` and VF 1.
3. The containers ping each other with no bridge on the host.
4. The host counter on `nic1` does not count the ping frames.
5. A stopped container returns its devices, and a start takes them again.
6. `ens1f0` keeps the management address through every step.
7. MWAN runs in container 1, unprivileged, with NPTv6 loaded.
8. No step used a root login.
