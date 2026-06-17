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
[docs/opnsense/testbed-baseline.md](../opnsense/testbed-baseline.md). Other
guest IPs are encoded in
[ansible/inventory/group_vars/all/service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml)
and in the matching OpenTofu resources.

The ISP LXCs (200/201/202) provide kea-dhcp6 (DHCPv6-PD), radvd (RA), and
nftables masquerade out via Comcast on vmbr0. IPv4 on the testbed MWAN VM is
statically assigned; the ISP LXCs do not run a DHCPv4 server.

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
