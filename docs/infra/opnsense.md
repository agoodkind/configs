# OPNsense Network Topology

OPNsense is QEMU VM 101 on vault, not the WAN edge. All WAN traffic flows through the MWAN VM.

**Interfaces:**

| Interface          | Role                             | IPv4                               | IPv6                                                           |
| ------------------ | -------------------------------- | ---------------------------------- | -------------------------------------------------------------- |
| `vtnet0` LAN       | Management LAN (containers)      | `10.250.0.1/24`                    | `3d06:bad:b01::1/64`                                           |
| `vtnet1` WAN       | Uplink to MWAN VM                | `10.250.250.2/29`                  | `3d06:bad:b01:fe::2/64`                                        |
| `iavf0`            | IoT / UniFi management           | `10.250.4.1/24`                    | `3d06:bad:b01:4::1/64`                                         |
| `vlan0100`         | Physical devices (mini, NAS)     | `10.250.1.1/24`                    | `3d06:bad:b01:1::1/64`                                         |
| `vlan0200`         | Home automation (Home Assistant) | `10.250.2.1/24`                    | `3d06:bad:b01:2::1/64`                                         |
| `vlan0300` CAPTIVE | Guest / captive portal           | `10.250.3.1/24`                    | None (intentionally absent)                                    |
| `wg0`              | WireGuard hub                    | `10.250.10.1/24`, `10.240.10.2/24` | `3d06:bad:b01:10::1/64`, `3d06:bad:b01:a::1/64`                |
| `nat64`            | Tayga NAT64                      | `10.250.46.1 -> 10.250.64.1`       | `3d06:bad:b01:64::ffff:1/128`; prefix `3d06:bad:b01:6464::/96` |

**Default routes:** IPv4 via `10.250.250.1` on `vtnet1`. IPv6 via `fe80::be24:11ff:fe72:c1` on `vtnet1`. OPNsense delegates all upstream routing to the MWAN VM.

**Internal prefix:** All internal addresses use `3d06:bad:b01::/48`. This is a stable internal prefix that is NOT delegated by any ISP. It is treated as ULA-equivalent but GUA-shaped so clients prefer IPv6. The mwan VM performs NPT to map this to ISP-delegated prefixes for outbound reachability.

**NPT mapping (provider `/56`, granted 2025-10-08):**

| Internal (`3d06:bad:b01:x::/64`) | External provider prefix (dynamic) | Segment                 |
| -------------------------------- | ---------------------------------- | ----------------------- |
| `3d06:bad:b01::/64`              | dynamic (not recorded)             | vmnet / management      |
| `3d06:bad:b01:1::/64`            | dynamic (not recorded)             | priv (mini, NAS)        |
| `3d06:bad:b01:2::/64`            | dynamic (not recorded)             | guest / home automation |
| `3d06:bad:b01:64::/64`           | dynamic (not recorded)             | v6-only experiment      |

AT&T PD `/60`: not recorded (provider-provided, verify live). AT&T IPv4 is `dynamic`.

**WireGuard peers (five configured):**

| Peer         | Tunnel address                              | Handshake status                                                       |
| ------------ | ------------------------------------------- | ---------------------------------------------------------------------- |
| alexs-mba    | `10.250.10.8`, `::10::8`                    | Active                                                                 |
| alexs-iphone | `10.250.10.4`, `::10::4`                    | Active (periodic)                                                      |
| suburban     | `10.250.10.2`, `::10::240`, plus NJ subnets | Healthy (verified live 2026-04-08).                                     |
| berylax      | `10.250.10.6`, `::10::6`                    | Never connected (no WireGuard installed on device)                     |
| alexs-mbp    | `10.250.10.3`, `::10::3`                    | Never connected                                                        |

**KEA DHCP reservations of note:** `mini` at `10.250.1.2` / `3d06:bad:b01:1::2`; `nas` at `3d06:bad:b01:1::3` (OPNsense alias updated 2026-03-23); `home-assistant` at `10.250.2.3` / `3d06:bad:b01:2::3`.

**DNS:** Unbound, forwarding to NextDNS. Private domains include `home.goodkind.io`, `goodkind.io`, `wg.goodkind.io`.

## Known Failed Systemd Units

- `proxmox-regenerate-snakeoil.service`: fails on consul (106) and mc (109). Expected; these containers already have real certs.
- `scripts-updater.service`: fails on consul (106), mc (109), adguard (112), dns64 (103), grommunio (104). Likely missing deploy key for the scripts repo or stale repo URL.
- `motd-news.service`: fails on ansible (107) and adguard (112). Ubuntu MOTD fetcher fails in IPv6-only containers.
