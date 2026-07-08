# Suburban MWAN testbed

Suburban is the NJ Proxmox hypervisor. The testbed mirrors production MWAN
using the same Ansible templates with different group vars
([test_mwan_servers.yml](../../ansible/inventory/group_vars/test_mwan_servers.yml)).
Live suburban definitions live in
[opentofu/suburban/](../../opentofu/suburban/); treat that module as
ground truth and update this page when it changes.

## Bridges

The bridge addresses and the `vmbrtrunk` VLAN ids live in
[opentofu/suburban/networks.tf](../../opentofu/suburban/networks.tf); this table is
the role mapping.

| Bridge | Role |
| ------ | ---- |
| vmbr0  | Comcast uplink: suburban-managed management plus outbound NAT |
| vmbr1  | VM management: suburban's testbed management subnet (no longer carries the testbed MWAN VM) |
| vmbr2  | MWAN internal link to OPNsense |
| vmbrtrunk | Services LAN and OPNsense MANAGEMENT: VLAN-aware trunk whose untagged services LAN holds the OPNsense MANAGEMENT, DNS64, seaweedfs, tack-qa, and testbed MWAN VM management addresses |
| vmbr4  | Simulated Webpass ISP (bare L2) |
| vmbr5  | Simulated AT&T ISP (bare L2) |
| vmbr6  | Simulated Monkeybrains ISP (bare L2 plus failover-test eth0) |

## Guests

OpenTofu owns every suburban guest. The VMIDs, names, types, and bridges live in
[opentofu/suburban/containers.tf](../../opentofu/suburban/containers.tf),
[opentofu/suburban/vms.tf](../../opentofu/suburban/vms.tf), and
[opentofu/suburban/networks.tf](../../opentofu/suburban/networks.tf); guest IPs are
in [service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml).
The roles they play:

- Testbed OPNsense gateway (QEMU).
- Testbed MWAN router, mirroring the production MWAN VM (QEMU).
- BGP failover backup, mirroring the production failover LXC (LXC).
- Three simulated ISP LXCs for Webpass, AT&T, and Monkeybrains.

Authoritative connection addresses for the OPNsense testbed are in
[docs/opnsense/testbed/baseline.md](../opnsense/testbed/baseline.md).

The ISP LXCs each provide DHCPv6-PD (kea-dhcp6) and radvd (RA) and masquerade out
via Comcast on vmbr0. Per-ISP addressing (PD prefixes, NPT, the v4 links and routed
`/29`s, and SLAAC) is parameterized in
[suburban_servers.yml](../../ansible/inventory/group_vars/suburban_servers.yml)
`testbed_isp_lxcs`, so this page does not restate the literal prefixes. NPT uses
the first `/60` of each delegation, and the PD prefixes use a `/56`-clean scheme
that stays clear of the management, LAN, internal, and SLAAC space. The structural
parity with prod:

- Monkeybrains runs the full prod dynamic stack: DHCPv4, DHCPv6 IA_NA, DHCPv6-PD
  `/56`, and SLAAC, so the testbed MWAN VM gets a dynamic v4, a DHCPv6 address, the
  PD, and a SLAAC address exactly as prod's real Monkeybrains delivers.
- AT&T models prod AT&T: a dynamic DHCPv4 link (pinned stable by a sim MAC
  reservation) over which the sim routes a static `/29` that the MWAN VM 1:1-NATs
  to the internal services, plus DHCPv6-PD `/60`. The testbed cannot reproduce
  802.1X/VLAN, so the link is a direct NIC.
- Webpass models prod Webpass: a static v4 link plus a routed static `/29` 1:1-NAT'd
  to the services, with DHCPv6-PD `/56`.

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
  [docs/opnsense/notes.md](../opnsense/notes.md) Rule 8.
- **Management return path.** The testbed MWAN VM management has no policy route,
  mirroring prod, so on-link replies to peers on the services LAN return directly.
  A management policy table carrying only a default route shadows the connected
  route and triangles on-link replies through the gateway, which breaks
  reachability.
- **Reachability probing.** The testbed OPNsense blocks ICMP echo to LAN hosts
  but allows TCP, so measure reachability with TCP or SSH, not `ping6`, or a
  healthy host reads as down.
- **Watchdog host config address.** `mwan-watchdog-testbed` on the suburban host
  must target the testbed MWAN VM's current management address on the services LAN
  (owned by service_mapping.yml) in `/etc/mwan/config.toml`. A stale address
  degrades its VM health probe to the
  TCP and PVE fallback channels (the vsock channel still works because it is
  CID-based), and a wedged snapshot plus a tight retry loop can hold the VM lock.
  The config is rendered by `deploy-proxmox --limit suburban`.
