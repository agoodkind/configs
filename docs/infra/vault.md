# Vault Proxmox Hypervisor

`3d06:bad:b01::254`, 12-core i7-1255U, 94 GB RAM, kernel 6.17.4-2-pve.

**LXC containers (all on `3d06:bad:b01::/64`):**

| VMID | Name      | IPv6    | Services observed running                                                                                                                                                                               |
| ---- | --------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 100  | debianct  | `::100` | GitHub Actions runner, Chrome Xpra, CUPS, Fail2Ban, rclone NAS mount, Consul agent. Developer LXC; not managed by Ansible.                                                                              |
| 102  | unifi     | `::102` | UniFi controller v10.0.162, Consul agent. `consul members` returned empty at probe; Consul join may be broken.                                                                                          |
| 103  | dns64     | `::64`  | BIND DNS64, Consul agent. Disk 47% (3.9 GB).                                                                                                                                                            |
| 104  | grommunio | `::104` | MariaDB, nginx, PHP-FPM, Consul agent.                                                                                                                                                                  |
| 105  | pvd       | `::105` | Proxmox Datacenter Manager v1.0.2, Postfix (local only), Consul agent. `remotes.cfg` absent; no remote PVE nodes connected. Deployed but not configured beyond base install.                            |
| 106  | consul    | `::106` | Consul server v1, single-node, `bootstrap_expect=1`, datacenter `home`, domain `int`. Occasional `dial tcp [::113]:8301: i/o timeout` from mwan (resolved 2026-03-22 by adding nftables rules on mwan). |
| 109  | mc        | `::109` | Crafty Controller, mod updater timer, Consul agent.                                                                                                                                                     |
| 110  | proxy     | `::110` | Traefik v3, SSHPiper on `[::]`:22, cloudflared tunnel `home-proxy`. sshd on port 2222.                                                                                                                  |
| 112  | adguard   | `::53`  | AdGuard Home v0.107.71, Consul agent. Upstream: NextDNS over QUIC. Disk 69% (7.8 GB).                                                                                                                   |

**QEMU VMs:**

| VMID | Name         | Notes                                                                                                                                                                                                                                                                                                                                                                                                        |
| ---- | ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 101  | router       | OPNsense. 8 GB RAM, 4 cores. PCI passthrough `hostpci0: 0000:02:0a` (X710 VF for AT&T 802.1X on mwan VM).                                                                                                                                                                                                                                                                                                    |
| 108  | freebsd-uefi | FreeBSD 14.3-RELEASE-p7, nginx + sshd. Cloud-init, 4 GB RAM. Traefik routes port 8080 to this host but no process was observed listening on 8080 at probe time.                                                                                                                                                                                                                                              |
| 113  | mwan         | Debian/Linux. Management `3d06:bad:b01::113/64`. 2 GB RAM, 2 cores. Running: `mwan agent` (monolith binary, gRPC on vsock `50051` and TCP fallback `:50052`), cloudflared tunnel `home-mwan`, consul, mwan-health daemon, wpa_supplicant. nftables allows port 50052 on enmgmt0. `mwan-change-detect.path` is still active and should be removed once the Go watchdog hash check fully owns drift detection. |

**Stopped VMs:** 200 (`test-vm`), 9000 (`debian-13-cloud-template`).

**Host services on vault:** `mwan-watchdog.service` runs the monolith binary
(`/usr/local/bin/mwan watchdog`). It uses vsock to the agent on VM 113 first,
with TCP on `[3d06:bad:b01::113]:50052` as fallback. Live logs on 2026-05-02
show the vsock channel healthy.
