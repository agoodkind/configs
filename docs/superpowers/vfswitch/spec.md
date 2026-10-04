# Hardware-switched link between two containers on poweredge

Two containers on poweredge exchange frames through the embedded switch of the
Broadcom BCM57504 card. No Linux bridge forwards those frames. Each container
also owns one whole SFP port.

## Defect this replaces

A Linux bridge between two router containers forwards every frame on a host
CPU. The frame enters the host network stack once for each container. The
BCM57504 card has an embedded switch on each port, and the card was not using
it: SR-IOV was off in the stored card settings.

## Measured facts

Measured on poweredge on 2026-10-04, firmware 236.1.173.0, kernel 7.0.14-20-pve.

| Fact | Result |
| --- | --- |
| Stored setting `enable_sriov` | Set to `true` on all four ports with `devlink dev param set`, active after one reboot |
| Virtual functions per port | 8 |
| Virtual function creation on a port that is administratively down | The driver rejects it |
| Two virtual functions on `nic1`, a port with no module | Created |
| `ip link set nic1 vf <n> state enable` with no cable | Both virtual function links are up |
| Ping between the two virtual functions, one in each network namespace | 3 of 3 replies, about 0.3 ms |
| IOMMU group of each SFP port | One group per port |
| Embedded switch mode | `legacy` |

## Contract

### 1. Ports

| Port | Role | Owner |
| --- | --- | --- |
| `ens1f0` | Proxmox management | The host. The address is on the port, and the port is in no bridge. |
| `nic2` | WAN in | Container 1, whole port |
| `nic0` | LAN out | Container 2, whole port |
| `nic1` | Link between the containers | The host owns the port. Container 1 owns virtual function 0. Container 2 owns virtual function 1. |
| `nic3`, `ens1f1` | Unused | The host, no address |

The host assigns no address and no bridge to `nic0`, `nic1`, `nic2`, or `nic3`.

### 2. Virtual functions on the host

The host network configuration declares `nic1` with `auto` and `manual`. Its
`post-up` commands run after the port is up, in this order:

1. Write `2` to `sriov_numvfs` of the port.
2. Set a fixed MAC address on each virtual function.
3. Set `state enable` on each virtual function.

The driver rejects step 1 on a port that is down. `state enable` gives each
virtual function a link while the port has no cable.

A fixed MAC address gives each container the same address after every boot.

### 3. Container interfaces

Each container receives its interfaces as existing host network devices. The
container start moves each device into the container network namespace, and
the container stop returns it to the host. No `veth` pair and no bridge exist
for these interfaces.

A virtual function is a host network device. A container does not need PCI
passthrough for it.

### 4. Forwarding

The embedded switch of `nic1` forwards frames between virtual function 0 and
virtual function 1 by MAC address. The host network stack does not process
those frames.

Each container kernel routes, translates addresses, and filters in software.

## Boundaries

- The embedded switch stays in `legacy` mode. `switchdev` mode with offloaded
  flow rules is separate work.
- The PCIe card ports `ens1f0` and `ens1f1` share one IOMMU group and stay on
  the host.
- A change of `enable_sriov` needs a reboot of poweredge.
- `nic0` is connected to the live LAN `10.230.0.0/24`. A DHCP server in
  container 2 answers clients on that LAN.

## Acceptance criteria

- AC1: After a reboot of poweredge, `nic1` has two virtual functions with the
  declared MAC addresses and `link-state enable`.
- AC2: Container 1 has `nic2` and virtual function 0. Container 2 has `nic0`
  and virtual function 1. The host has none of the four devices while both
  containers run.
- AC3: Container 1 and container 2 exchange ping replies over the virtual
  functions with no bridge on the host.
- AC4: During AC3, the host receive counter of `nic1` does not increase by the
  ping frames.
- AC5: After a container stop, its devices are host devices again, and the
  next start moves them into the container.
- AC6: `ens1f0` has the management address before and after every step.

## Open decisions

| Decision | Options |
| --- | --- |
| Router software in container 2 | A: dnsmasq for DHCP and DNS with nftables. B: another stack. |
| LAN exposure of container 2 | A: `nic0` stays unplugged from the live LAN until the cutover. B: container 2 runs with DHCP off while `nic0` is on the live LAN. |
| Declaration | A: the host network file and both containers in OpenTofu. B: the host network file by hand, the containers in OpenTofu. |
