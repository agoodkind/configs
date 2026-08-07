# Suburban MWAN testbed

Suburban is the NJ Proxmox hypervisor. The testbed mirrors production MWAN
using the same Ansible templates with different group vars
([mwan_suburban_servers.yml](../../ansible/inventory/group_vars/mwan_suburban_servers.yml)).
Live suburban definitions live in
[opentofu/suburban/](../../opentofu/suburban/); treat that module as
ground truth and update this page when it changes.

## Bridges

Bridge names, addresses, and VLAN ids live in
[opentofu/suburban/networks.tf](../../opentofu/suburban/networks.tf); guest
addresses live in
[service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml).
What the doc adds is the behavior each bridge carries:

- The Comcast uplink bridge is suburban's own internet path and the outbound
  NAT boundary. Nothing testbed-internal attaches to it.
- The VM management bridge is the uplink behind suburban: the ISP sims default
  through it, so replies to tunnel and testbed sources drain to suburban, and
  their internet traffic masquerades onward to Comcast.
- The MWAN internal bridge is the OPNsense transit link. Suburban holds no
  address on it, matching vault, so it reaches the guests through the router
  rather than across the bridge.
- The VLAN-aware trunk carries VMNET, the guest segment every testbed guest
  and the router share, plus the tagged VLANs mirroring production.
- Each simulated ISP has its own bare-L2 bridge terminating one WAN link for
  the MWAN VM; the Monkeybrains one also carries the failover's upstream NIC.

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

When SSH to the testbed OPNsense fails, reach it over the out-of-band channel
in [docs/opnsense/testbed/access.md](../opnsense/testbed/access.md).

Each ISP simulator terminates one WAN link for the MWAN VM, serves DHCPv6-PD
through kea-dhcp6 and router advertisements through radvd, and uplinks through
suburban, which returns tunnel and testbed traffic and masquerades internet
traffic out to Comcast. Each sim's capability flags and downstream subnets
live in
[suburban_servers.yml](../../ansible/inventory/group_vars/suburban_servers.yml)
under `testbed_isp_lxcs`, which carries a comment explaining each flag; ids,
hostnames, and uplink addresses live in
[service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml).

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
| MWAN VM                    | mwan                                            | mwan.suburban.goodkind.io                                         |
| Failover LXC               | mwan-failover                                   | mwan-failover.suburban.goodkind.io                                |
| OPNsense                   | router.home.goodkind.io                         | router.suburban.goodkind.io                                       |
| Hypervisor                 | vault                                           | suburban                                                          |
| Group vars (MWAN)          | [ansible/inventory/group_vars/mwan_servers.yml](../../ansible/inventory/group_vars/mwan_servers.yml) | [ansible/inventory/group_vars/mwan_suburban_servers.yml](../../ansible/inventory/group_vars/mwan_suburban_servers.yml) |
| Group vars (OPNsense)      | [ansible/inventory/group_vars/opnsense_servers.yml](../../ansible/inventory/group_vars/opnsense_servers.yml) | [ansible/inventory/group_vars/opnsense_suburban_servers.yml](../../ansible/inventory/group_vars/opnsense_suburban_servers.yml) |
| Deploy playbook (MWAN)     | [ansible/playbooks/deploy-mwan.yml](../../ansible/playbooks/deploy-mwan.yml) `--limit mwan_servers` | [ansible/playbooks/deploy-mwan.yml](../../ansible/playbooks/deploy-mwan.yml) `--limit mwan_suburban_servers` |
| Deploy playbook (failover) | [ansible/playbooks/deploy-mwan-failover.yml](../../ansible/playbooks/deploy-mwan-failover.yml) `--limit mwan_failover_servers` | [ansible/playbooks/deploy-mwan-failover.yml](../../ansible/playbooks/deploy-mwan-failover.yml) `--limit mwan_failover_suburban_servers` |
| Deploy playbook (OPNsense) | [ansible/playbooks/deploy-opnsense.yml](../../ansible/playbooks/deploy-opnsense.yml) `--limit opnsense_servers` | [ansible/playbooks/deploy-opnsense.yml](../../ansible/playbooks/deploy-opnsense.yml) `--limit opnsense_suburban_servers` |
| Suburban-only extras       | n/a | [ansible/playbooks/deploy-testbed.yml](../../ansible/playbooks/deploy-testbed.yml) `--limit suburban` |

## Testbed-only infrastructure

The ISP sim LXCs, suburban-side safe IPv6 sysctl defaults, and suburban
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
- **Management return path.** The MWAN VM's management interface has no policy
  route, mirroring prod, so on-link replies to peers on the shared guest
  segment return directly. A management policy table carrying only a default
  route shadows the connected route and triangles on-link replies through the
  gateway, which breaks reachability.
- **Reachability probing.** The testbed OPNsense blocks ICMP echo to LAN hosts
  but allows TCP, so measure reachability with TCP or SSH, not `ping6`, or a
  healthy host reads as down.
- **Watchdog host config address.** `mwan-watchdog-testbed` on the suburban host
  must target the MWAN VM's current management address, from
  [service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml),
  in `/etc/mwan/config.toml`. A stale address degrades its VM health probe to the
  TCP and PVE fallback channels (the vsock channel still works because it is
  CID-based), and a wedged snapshot plus a tight retry loop can hold the VM lock.
  The config is rendered by `deploy-proxmox --limit suburban`.
