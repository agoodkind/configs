# Suburban MWAN Testbed

*Updated 2026-04-12. Suburban is the NJ hypervisor (`10.240.0.148` via Comcast, `3d06:bad:b01:200::1` on vmbr1). The testbed mirrors production MWAN using the same Ansible `.j2` templates with different group vars (`mwan_testbed_servers.yml`).*

**Bridges:**

| Bridge | Role                       | Addresses                                      |
| ------ | -------------------------- | ---------------------------------------------- |
| vmbr0  | Comcast uplink             | `10.240.0.148/24` (static), Comcast RA (SLAAC) |
| vmbr1  | VM management              | `10.240.200.1/24`, `3d06:bad:b01:200::1/64`    |
| vmbr2  | MWAN internal (OPNsense)   | `10.250.250.5/29`, `3d06:bad:b01:201::5/64`    |
| vmbr3  | OPNsense LAN               | bare L2                                        |
| vmbr4  | Simulated Webpass ISP      | bare L2                                        |
| vmbr5  | Simulated AT&T ISP         | bare L2                                        |
| vmbr6  | Simulated Monkeybrains ISP | bare L2                                        |

**Guests:**

| VMID | Name               | Type | Role                                       | Management address                           |
| ---- | ------------------ | ---- | ------------------------------------------ | -------------------------------------------- |
| 101  | opnsense-test      | QEMU | Testbed OPNsense gateway                   | `192.168.1.1` (LAN), `10.250.250.2/29` (WAN) |
| 950  | test-mwan          | QEMU | Testbed MWAN router (mirrors prod VM 113)  | `3d06:bad:b01:200::950`                      |
| 100  | mwan-failover-test | LXC  | BGP failover backup (mirrors prod LXC 116) | `3d06:bad:b01:200::100`                      |
| 200  | isp-webpass        | LXC  | Simulated Webpass ISP                      | on vmbr4                                     |
| 201  | isp-att            | LXC  | Simulated AT&T ISP                         | on vmbr5                                     |
| 202  | isp-mbrains        | LXC  | Simulated Monkeybrains ISP                 | on vmbr6                                     |

**ISP LXCs provide:** kea-dhcp6 (DHCPv6-PD /60), radvd (RA), nftables (masquerade to Comcast via vmbr0). IPv4 is static on VM 950 (no DHCPv4 server on ISP LXCs).

**Production vs Testbed comparison:**

| Component              | Production (vault)             | Testbed (suburban)                          |
| ---------------------- | ------------------------------ | ------------------------------------------- |
| MWAN VM                | 113                            | 950                                         |
| Failover LXC           | 116                            | 100                                         |
| OPNsense               | VM 101 (vault)                 | VM 101 (suburban)                           |
| Hypervisor             | vault                          | suburban                                    |
| Internal prefix        | `3d06:bad:b01:fe::/64`         | `3d06:bad:b01:201::/64`                     |
| Management prefix      | `3d06:bad:b01::/64`            | `3d06:bad:b01:200::/64`                     |
| AT&T interface         | `enatt0.3242` (802.1X + VLAN)  | `enatt0` (direct, no VLAN)                  |
| Webpass interface      | `enwebpass0` (igc passthrough) | `enwebpass0` (virtio)                       |
| Monkeybrains interface | `enmbrains0` (virtio)          | `enmbrains0` (virtio)                       |
| IPv4 WAN addressing    | Static (public IPs)            | Static (private `10.240.x.2/24`)            |
| IPv6 WAN addressing    | DHCPv6-PD from real ISPs       | DHCPv6-PD from ISP LXCs                     |
| NPT prefixes           | Real ISP PD prefixes           | Simulated `3d06:bad:b01:{220,230,240}::/60` |
| Internal link IPv4     | `10.250.250.0/29`              | `10.250.250.0/29` (same)                    |
| BGP ASN                | 4200000001                     | 4200000001 (same)                           |
| Config templates       | `mwan_servers.yml`             | `test_mwan_servers.yml`                     |
| Deploy playbook (MWAN) | `deploy-mwan.yml --limit mwan_servers` | `deploy-mwan.yml --limit test_mwan_servers` |
| Deploy playbook (failover) | `deploy-mwan-failover.yml --limit mwan_failover_servers` | `deploy-mwan-failover.yml --limit mwan_failover_test_servers` |
| Suburban-only extras   | n/a                            | `deploy-testbed.yml --limit suburban`       |

**Testbed-only infrastructure (no production equivalent):** ISP LXCs 200/201/202, suburban sysctl (`accept_ra=0` on vmbr4/5/6), suburban masquerade rules (vmbr1 to vmbr0/wg0).
