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

OpenTofu provisions every suburban guest, and Ansible configures what runs inside
it. Both read the same
[service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml)
for a guest's id, hostname, and address, so that file answers which guest is
which and a renumber changes one line. The
[suburban module](../../opentofu/suburban/) holds the rest of the guest shape:
type, bridges, disk, and memory.

A guest that mirrors a production service takes its counterpart's VMID plus 100,
and the simulated ISPs, which have no production counterpart, sit in the 9xx
range. No id is shared between the two hypervisors.

That separation is what makes a misdirected command safe. The hypervisors are
independent Proxmox installations rather than a cluster, so a shared id is legal,
but a command that lands on the wrong host then finds a guest at that id and
succeeds against it. With no id in common the same mistake fails outright.

Connection addresses for the OPNsense testbed are in
[docs/opnsense/testbed/baseline.md](../opnsense/testbed/baseline.md).

Each ISP simulator terminates one WAN link for the MWAN VM, serves DHCPv6-PD
through kea-dhcp6 and router advertisements through radvd, and masquerades out
via Comcast on vmbr0. Every per-ISP value lives in
[suburban_servers.yml](../../ansible/inventory/group_vars/suburban_servers.yml)
under `testbed_isp_lxcs`, which carries a comment explaining each capability
flag.

The three sims differ because the real WANs do. Monkeybrains runs the full
dynamic stack, so the MWAN VM receives a DHCPv4 lease, a DHCPv6 address, a
delegated prefix, and a SLAAC address exactly as the real Monkeybrains delivers.
AT&T offers a dynamic DHCPv4 link pinned stable by a MAC reservation, over which
the sim routes a static block that the MWAN VM translates one-to-one to its
internal services; the testbed cannot reproduce 802.1X or the VLAN, so that link
is a plain NIC. Webpass offers a static link plus its own routed static block.

Prefix delegation sizes match production, and NPT translates the first `/60` of
each delegation. The delegated prefixes deliberately avoid the `02xx` space that
management, LAN, internal, and SLAAC already use.

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

ISP LXCs 900/901/902, suburban-side safe IPv6 sysctl defaults, and suburban
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
- **`args` ownership.** VMs with a virtio-serial or vsock device (the MWAN VM and the OPNsense guest)
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
