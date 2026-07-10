# Suburban MWAN testbed

Suburban is the NJ Proxmox hypervisor. The testbed mirrors production MWAN
using the same Ansible templates with different group vars
([test_mwan_servers.yml](../../ansible/inventory/group_vars/test_mwan_servers.yml)).
Live suburban definitions live in
[opentofu/suburban/](../../opentofu/suburban/); treat that module as
ground truth and update this page when it changes.

## Bridges

| Bridge | Role                       | Notes                                            |
| ------ | -------------------------- | ------------------------------------------------ |
| vmbr0  | Comcast uplink             | Suburban-managed management plus outbound NAT    |
| vmbr1  | VM management              | Suburban's testbed management subnet; no longer carries VM 950 |
| vmbr2  | MWAN internal (OPNsense)   | `10.250.250.0/29` and `3d06:bad:b01:201::5/64` (testbed-side) |
| vmbrtrunk | Services LAN (OPNsense MANAGEMENT) | VLAN-aware trunk, vids `64 100 200 300`, host `3d06:bad:b01:204::5/64`. Untagged `204::` LAN holds OPNsense MANAGEMENT `204::1`, DNS64 LXC `204::464`, seaweedfs `204::410`, tack-qa `204::400`, and VM 950 mgmt `204::950`. "MWAN-140 slice 1". |
| vmbr4  | Simulated Webpass ISP      | bare L2                                          |
| vmbr5  | Simulated AT&T ISP         | bare L2                                          |
| vmbr6  | Simulated Monkeybrains ISP | bare L2 plus failover-test eth0                  |

## Guests

OpenTofu owns every suburban guest below. Cross-check VMID, type, and bridges
against [opentofu/suburban/containers.tf](../../opentofu/suburban/containers.tf),
[opentofu/suburban/vms.tf](../../opentofu/suburban/vms.tf), and
[opentofu/suburban/networks.tf](../../opentofu/suburban/networks.tf) when in doubt.

| VMID | Name               | Type | Role                                                  |
| ---- | ------------------ | ---- | ----------------------------------------------------- |
| 101  | opnsense-test      | QEMU | Testbed OPNsense gateway                              |
| 950  | test-mwan          | QEMU | Testbed MWAN router (mirrors prod MWAN VM)            |
| 100  | mwan-failover-test | LXC  | BGP failover backup (mirrors prod failover LXC)       |
| 200  | isp-webpass        | LXC  | Simulated Webpass ISP                                 |
| 201  | isp-att            | LXC  | Simulated AT&T ISP                                    |
| 202  | isp-mbrains        | LXC  | Simulated Monkeybrains ISP                            |

Authoritative connection addresses for the OPNsense testbed are documented in
[docs/opnsense/testbed/baseline.md](../opnsense/testbed/baseline.md). Other
guest IPs are encoded in
[ansible/inventory/group_vars/all/service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml)
and in the matching OpenTofu resources.

The ISP LXCs (200/201/202) each provide DHCPv6-PD (kea-dhcp6) and radvd (RA), and
masquerade out via Comcast on vmbr0. Per-ISP addressing is parameterized in
[suburban_servers.yml](../../ansible/inventory/group_vars/suburban_servers.yml)
`testbed_isp_lxcs`, mirroring how prod addresses each WAN. DHCPv6-PD sizes match
prod: webpass `/56`, att `/60`, monkeybrains `/56`, with NPT using the first `/60`
of each delegation. The PD prefixes use the 22/23/24 `/56`-clean scheme to stay
clear of the `02xx` mgmt/LAN/internal/SLAAC space:

| WAN | sim LXC | DHCPv6-PD | NPT (first /60) | v4 | SLAAC |
| --- | ------- | --------- | --------------- | -- | ----- |
| Monkeybrains | 202 | `3d06:bad:b01:2400::/56` | `3d06:bad:b01:2400::/60` | DHCPv4 (kea-dhcp4) + DHCPv6 IA_NA, masquerade egress | `3d06:bad:b01:0250::/64` |
| AT&T | 201 | `3d06:bad:b01:2300::/60` | `3d06:bad:b01:2300::/60` | dynamic DHCPv4 link (MAC-pinned to `10.240.205.2`) + routed static `/29` `10.241.205.0/29` 1:1-NAT'd to services; no 802.1X/VLAN | none |
| Webpass | 200 | `3d06:bad:b01:2200::/56` | `3d06:bad:b01:2200::/60` | static v4 link + routed static `/29` `10.241.204.0/29` 1:1-NAT'd to services | none |

Monkeybrains (202) runs the full prod dynamic stack: DHCPv4, DHCPv6 IA_NA,
DHCPv6-PD, and SLAAC, so VM 950 gets a dynamic v4, a DHCPv6 address, the PD, and a
SLAAC address exactly as prod's real Monkeybrains delivers. AT&T (201) models prod
AT&T: a dynamic DHCPv4 link (pinned stable by a sim MAC reservation) over which
the sim routes a static `/29` (`10.241.205.0/29`) that VM 950 1:1-NATs to the five
internal services, plus DHCPv6-PD; the testbed cannot reproduce 802.1X/VLAN, so
the link is a direct NIC. Webpass (200) models prod Webpass: a static v4 link plus
a static `/29` (`10.241.204.0/29`) 1:1-NAT'd to the five services, with DHCPv6-PD
`/56` (NPT on its first `/60`).

## Production vs testbed

| Component                  | Production (vault)                              | Testbed (suburban)                                                |
| -------------------------- | ----------------------------------------------- | ----------------------------------------------------------------- |
| MWAN VM                    | mwan                                            | test-mwan                                                         |
| Failover LXC               | mwan-failover                                   | mwan-failover-test                                                |
| OPNsense                   | router.home.goodkind.io                         | opnsense-test                                                     |
| Hypervisor                 | vault                                           | suburban                                                          |
| Group vars (MWAN)          | [ansible/inventory/group_vars/mwan_servers.yml](../../ansible/inventory/group_vars/mwan_servers.yml) | [ansible/inventory/group_vars/test_mwan_servers.yml](../../ansible/inventory/group_vars/test_mwan_servers.yml) |
| Group vars (OPNsense)      | [ansible/inventory/group_vars/opnsense_servers.yml](../../ansible/inventory/group_vars/opnsense_servers.yml) | [ansible/inventory/group_vars/opnsense_test_servers.yml](../../ansible/inventory/group_vars/opnsense_test_servers.yml) |
| Deploy playbook (MWAN)     | [ansible/playbooks/deploy-mwan.yml](../../ansible/playbooks/deploy-mwan.yml) `--limit mwan_servers` | [ansible/playbooks/deploy-mwan.yml](../../ansible/playbooks/deploy-mwan.yml) `--limit test_mwan_servers` |
| Deploy playbook (failover) | [ansible/playbooks/deploy-mwan-failover.yml](../../ansible/playbooks/deploy-mwan-failover.yml) `--limit mwan_failover_servers` | [ansible/playbooks/deploy-mwan-failover.yml](../../ansible/playbooks/deploy-mwan-failover.yml) `--limit mwan_failover_test_servers` |
| Deploy playbook (OPNsense) | [ansible/playbooks/deploy-opnsense.yml](../../ansible/playbooks/deploy-opnsense.yml) `--limit opnsense_servers` | [ansible/playbooks/deploy-opnsense.yml](../../ansible/playbooks/deploy-opnsense.yml) `--limit opnsense_test_servers` |
| Suburban-only extras       | n/a | [ansible/playbooks/deploy-testbed.yml](../../ansible/playbooks/deploy-testbed.yml) `--limit suburban` |

## Testbed-only infrastructure

ISP LXCs 200/201/202, suburban-side safe IPv6 sysctl defaults, and suburban
masquerade rules (`vmbr1` to `vmbr0`/`wg0`) only exist on the testbed. The
bridge shape stays in OpenTofu, the safe early-boot sysctl defaults stay in
[ansible/playbooks/deploy-testbed.yml](../../ansible/playbooks/deploy-testbed.yml),
and the live per-bridge Router Advertisement policy is reconciled continuously by
`mwan-ifmgr` from the suburban host config rendered by the Proxmox host tasks.

## Suburban gotchas

These are suburban-specific facts that differ from vault and cost time when
unknown.

- **Cloud-init drive storage.** `local-lvm` is disabled on suburban; only
  `local-zfs` is active. Guest `initialization.datastore_id` must be `local-zfs`,
  or a cloud-init drive regen fails with `storage 'local-lvm' is not available`.
- **`args` ownership.** VMs with a virtio-serial or vsock device (VM 950, VM 101)
  carry their `args` set by Ansible as `root@pam`, because the Proxmox API rejects
  `args` writes from a token. OpenTofu must not manage `kvm_arguments` for those
  VMs (`lifecycle.ignore_changes = [kvm_arguments]`), or a plan tries to null the
  field and the apply fails with `VM is locked`. See
  [docs/opnsense/operations.md](../opnsense/operations.md) Rule 8.
- **Management return path.** VM 950 management has no policy route, mirroring
  prod, so on-link replies to peers on the `204::` services LAN return directly.
  A management policy table carrying only a default route shadows the connected
  route and triangles on-link replies through the gateway, which breaks
  reachability.
- **Reachability probing.** The testbed OPNsense blocks ICMP echo to LAN hosts
  but allows TCP, so measure reachability with TCP or SSH, not `ping6`, or a
  healthy host reads as down.
- **Watchdog host config address.** `mwan-watchdog-testbed` on the suburban host
  must target VM 950's current management address (`204::950`) in
  `/etc/mwan/config.toml`. A stale address degrades its VM health probe to the
  TCP and PVE fallback channels (the vsock channel still works because it is
  CID-based), and a wedged snapshot plus a tight retry loop can hold the VM lock.
  The config is rendered by `deploy-proxmox --limit suburban`.
