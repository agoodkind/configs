# Suburban MWAN testbed

Suburban is the NJ Proxmox hypervisor. The testbed mirrors production MWAN
using the same Ansible templates with different group vars
([test_mwan_servers.yml](../../ansible/inventory/group_vars/test_mwan_servers.yml)).
Live container definitions live in
[opentofu/containers.tf](../../opentofu/containers.tf); treat that file as
ground truth and update this page when it changes.

## Bridges

| Bridge | Role                       | Notes                                            |
| ------ | -------------------------- | ------------------------------------------------ |
| vmbr0  | Comcast uplink             | Suburban-managed management plus outbound NAT    |
| vmbr1  | VM management              | Suburban's testbed management subnet             |
| vmbr2  | MWAN internal (OPNsense)   | `10.250.250.0/29` and `3d06:bad:b01:fe::/64` (testbed-side) |
| vmbr3  | OPNsense LAN               | bare L2, shared by `testbed-proxy` LAN client    |
| vmbr4  | Simulated Webpass ISP      | bare L2                                          |
| vmbr5  | Simulated AT&T ISP         | bare L2                                          |
| vmbr6  | Simulated Monkeybrains ISP | bare L2 plus failover-test eth0                  |

## Guests

OpenTofu owns every suburban guest below. Cross-check VMID, type, and bridges
against [opentofu/containers.tf](../../opentofu/containers.tf) when in doubt.

| VMID | Name               | Type | Role                                                  |
| ---- | ------------------ | ---- | ----------------------------------------------------- |
| 101  | opnsense-test      | QEMU | Testbed OPNsense gateway                              |
| 950  | test-mwan          | QEMU | Testbed MWAN router (mirrors prod MWAN VM)            |
| 100  | mwan-failover-test | LXC  | BGP failover backup (mirrors prod failover LXC)       |
| 200  | isp-webpass        | LXC  | Simulated Webpass ISP                                 |
| 201  | isp-att            | LXC  | Simulated AT&T ISP                                    |
| 202  | isp-mbrains        | LXC  | Simulated Monkeybrains ISP                            |
| 203  | testbed-proxy      | LXC  | LAN-side OPNsense client used during cutover testing  |

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

ISP LXCs 200/201/202, suburban-side sysctl tweaks
(`accept_ra=0` on `vmbr4`/`vmbr5`/`vmbr6`), and suburban masquerade rules
(`vmbr1` to `vmbr0`/`wg0`) only exist on the testbed. These are owned by
[ansible/playbooks/deploy-testbed.yml](../../ansible/playbooks/deploy-testbed.yml).
