# INFRA.md

## Infrastructure Snapshot

_Last verified: 2026-03-28. Sources: live SSH to vault, router, and containers; `pct list`,
`qm list`, `systemctl`, KEA conf files, radvd conf, `wg show`, and `/conf/config.xml` on the
OPNsense router; Cloudflare API (`/client/v4/accounts`, `/cfd_tunnel`, `/dns_records`,
`/load_balancers`, `/load_balancers/pools`, zone settings). Treat any IP or service state
here as potentially stale; it reflects a point-in-time probe, not a live feed._

### Proxmox vault hypervisor

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
| 107  | ansible   | `::107` | Semaphore UI on `:3000`. Traefik health check confirms UP.                                                                                                                                              |
| 109  | mc        | `::109` | Crafty Controller, mod updater timer, Consul agent.                                                                                                                                                     |
| 110  | proxy     | `::110` | Traefik v3, SSHPiper on `[::]`:22, cloudflared v2025.11.1 (tunnel `home-proxy`). sshd on port 2222.                                                                                                     |
| 112  | adguard   | `::53`  | AdGuard Home v0.107.71, Consul agent. Upstream: NextDNS over QUIC. Disk 69% (7.8 GB).                                                                                                                   |

**QEMU VMs:**

| VMID | Name         | Notes                                                                                                                                                                                                                                                           |
| ---- | ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 101  | router       | OPNsense 25.7.11_2, FreeBSD 14.3-RELEASE-p7. 8 GB RAM, 4 cores. PCI passthrough `hostpci0: 0000:02:0a` (X710 VF for AT&T 802.1X on mwan VM).                                                                                                                    |
| 108  | freebsd-uefi | FreeBSD 14.3-RELEASE-p7, nginx + sshd. Cloud-init, 4 GB RAM. Traefik routes port 8080 to this host but no process was observed listening on 8080 at probe time.                                                                                                 |
| 113  | mwan         | Debian/Linux. Management `3d06:bad:b01::113/64`. 2 GB RAM, 2 cores. Running: cloudflared v2026.3.0 (tunnel `home-mwan`, 4/4 QUIC via IPv6, up since 2026-01-22), consul, mwan-health daemon, wpa_supplicant. msmtp 1.8.28 with `auth login` (fixed 2026-03-22). |

**Stopped VMs:** 200 (`test-vm`), 9000 (`debian-13-cloud-template`).

### Hosts not on vault Proxmox

| Host           | OS / Type                      | Network                                                                                      | Email setup                             | Ansible-managed? | Notes                                                                                                                                                                                                                                       |
| -------------- | ------------------------------ | -------------------------------------------------------------------------------------------- | --------------------------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| home-assistant | Home Assistant OS              | vlan0200, `10.250.2.3` / `3d06:bad:b01:2::3`                                                 | N/A (HAOS)                              | No               | KEA reservation confirmed. SSH on port 22222.                                                                                                                                                                                               |
| mini           | Ubuntu 24.04.4 LTS             | vlan0100, `10.250.1.2` / `3d06:bad:b01:1::2`                                                 | msmtp installed, no `/etc/msmtprc`      | Partial          | Has `scripts-updater.timer`. Needs `prep-guests.yml` run.                                                                                                                                                                                   |
| nas            | Ubuntu 24.04.3 LTS             | vlan0100, `3d06:bad:b01:1::3` (live)                                                         | msmtp installed, no `/etc/msmtprc`      | Partial          | SSH via `ssh nas`. OPNsense alias `nas_host` updated to `::3` (2026-03-23).                                                                                                                                                                 |
| vault          | Debian 13 (trixie), Proxmox VE | `3d06:bad:b01::254`                                                                          | No msmtp; uses Proxmox datacenter email | No               | `deploy-consul-external.yml` targets this host but has `consul_arch: arm64` bug.                                                                                                                                                            |
| suburban       | Debian 13 (trixie), Proxmox VE | `3d06:bad:b01:200::254`, Comcast NJ WAN, WG tunnel                                           | No msmtp installed                      | Partial          | Remote NJ hypervisor. Up 72+ days. WireGuard tunnel to home shows last handshake ~7 days ago despite 25s keepalive.                                                                                                                         |
| imac           | Unknown                        | Unknown (not in service_mapping)                                                             | Unknown                                 | No               | Not documented or discoverable from known inventory. Worth clarifying if this host exists.                                                                                                                                                  |
| berylax        | OpenWrt 24.10.5, GL.iNet       | `eth0`: Monkeybrains CG-NAT `100.64.155.249/24`; `br-lan`: `3d06:bad:b01:300::1/56` (static) | N/A                                     | No               | Same Monkeybrains L2 segment as mwan. `3d06:bad:b01:300::1/56` is statically set in UCI; OPNsense has no route for `3d06:bad:b01:300::/56`, so how this address is routable from the home network is not confirmed. No WireGuard installed. |
| jetkvm (x2)    | JetKVM (embedded Linux)        | Monkeybrains L2 segment                                                                      | Unknown                                 | No               | Two KVM-over-IP devices on the Monkeybrains segment (link-locals `fe80::8234:28ff:fe66:5ed7` and `fe80::3252:53ff:fe0d:6d08`). Not in any inventory.                                                                                        |

### MWAN WAN links (confirmed via SSH to `3d06:bad:b01::113`)

| Interface     | Provider      | IPv4                         | IPv6                                                                             | Route metric     | Notes                                                                                                         |
| ------------- | ------------- | ---------------------------- | -------------------------------------------------------------------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------- |
| `enwebpass0`  | Webpass       | `136.25.91.242/29`           | `2604:5500:c271:8000::72b` (working)                                             | 10 (primary)     | Google Fiber. RTT to `2001:4860:4860::8888` ~2.6 ms.                                                          |
| `enatt0.3242` | AT&T (802.1X) | `104.57.226.192/29` static   | WAN `/128` `2001:506:72f7:108c::1`; PD `/60` `2600:1700:2f71:c80::/60` confirmed | 1024 (secondary) | IPv6 gateway pings fine but `ping6 8.8.8.8` is 100% loss. NPT rule or PD routing issue suspected.             |
| `enmbrains0`  | Monkeybrains  | `100.64.155.253/24` (CG-NAT) | None (router solicitation times out)                                             | 5000 (tertiary)  | IPv6 absent since 2026-01-22 23:44 UTC (57-second window after last good probe). Gateway CPE likely replaced. |

### OPNsense network topology

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

**NPT mapping (Webpass `/56` `2604:5500:c271:bf00::/56`, granted 2025-10-08):**

| Internal (`3d06:bad:b01:x::/64`) | External Webpass (`2604:5500:c271:bfxx::/64`) | Segment                 |
| -------------------------------- | --------------------------------------------- | ----------------------- |
| `3d06:bad:b01::/64`              | `2604:5500:c271:bf00::/64`                    | vmnet / management      |
| `3d06:bad:b01:1::/64`            | `2604:5500:c271:bf01::/64`                    | priv (mini, NAS)        |
| `3d06:bad:b01:2::/64`            | `2604:5500:c271:bf02::/64`                    | guest / home automation |
| `3d06:bad:b01:64::/64`           | `2604:5500:c271:bf64::/64`                    | v6-only experiment      |

AT&T PD `/60`: `2600:1700:2f71:c80::/60`. AT&T static IPv4: `104.57.226.192/29`.

**WireGuard peers (five configured):**

| Peer         | Tunnel address                              | Handshake status                                                       |
| ------------ | ------------------------------------------- | ---------------------------------------------------------------------- |
| alexs-mba    | `10.250.10.8`, `::10::8`                    | Active                                                                 |
| alexs-iphone | `10.250.10.4`, `::10::4`                    | Active (periodic)                                                      |
| suburban     | `10.250.10.2`, `::10::240`, plus NJ subnets | Last handshake ~7 days ago despite 25s keepalive. Under investigation. |
| berylax      | `10.250.10.6`, `::10::6`                    | Never connected (no WireGuard installed on device)                     |
| alexs-mbp    | `10.250.10.3`, `::10::3`                    | Never connected                                                        |

**KEA DHCP reservations of note:** `mini` at `10.250.1.2` / `3d06:bad:b01:1::2`; `nas` at `3d06:bad:b01:1::3` (OPNsense alias updated 2026-03-23); `home-assistant` at `10.250.2.3` / `3d06:bad:b01:2::3`.

**DNS:** Unbound, forwarding to NextDNS. Private domains include `home.goodkind.io`, `goodkind.io`, `wg.goodkind.io`.

### Known failed systemd units (non-critical)

- `proxmox-regenerate-snakeoil.service`: fails on consul (106) and mc (109). Expected; these containers already have real certs.
- `scripts-updater.service`: fails on consul (106), mc (109), adguard (112), dns64 (103), grommunio (104). Likely missing deploy key for the scripts repo or stale repo URL.
- `motd-news.service`: fails on ansible (107) and adguard (112). Ubuntu MOTD fetcher fails in IPv6-only containers.

### Services that appear incomplete or not fully deployed

**NanoMDM:** Config, group vars, and a Traefik route (`mdm.home.goodkind.io` to `::103:9000`) all exist but no container runs MDM. VMID 103 is dns64, not an MDM host. The Traefik backend address appears stale. No MDM container was found at probe time.

**pvd (Proxmox Datacenter Manager):** Container 105 is running but no `deploy-pvd.yml` exists. It was likely provisioned manually. Do not attempt to re-provision via Ansible without understanding the original setup.

**grommunio:** Container is running (nginx, MariaDB, PHP-FPM). `deploy-grommunio.yml` exists but is not wired into any workflow. It may have drifted from the live config.

### Configs in repo and their deployment status

| Directory   | Status                | Notes                                                                             |
| ----------- | --------------------- | --------------------------------------------------------------------------------- |
| `mc/`       | Deployed, untracked   | `crafty.service`, `update-mods.*` confirmed on CT 109. `deploy-mc.yml` untracked. |
| `kea/`      | Actively used         | Rakefile is the live push mechanism for DHCP config. Not passive.                 |
| `bind/`     | Actively used         | `named.conf.options.j2` directly referenced by `deploy-dns64.yml`.                |
| `logstash/` | Retired               | No live instance anywhere. User confirmed retired.                                |
| `ups-nut/`  | Planning docs only    | NUT running on vault manually. `templates/` directory does not exist yet.         |
| `nanomdm/`  | Planned, not deployed | Only `enroll.mobileconfig.j2` present.                                            |
| `proxmox/`  | Likely superseded     | Static copies of watchdog files; `mwan/proxmox/` has the active Jinja2 versions.  |
| `common/`   | Actively deployed     | `package-updater.timer` confirmed on CTs 103, 106, 109, 110, 112.                 |

### Cloudflare account

_Queried via API 2026-03-28. Account: Alexander Goodkind (`ee7d7ca7d611ef8c2a07885e8362de0c`).
Zone `goodkind.io` is on the Pro plan, SSL mode strict, TLS 1.3 0-RTT, HTTP/3 on, IPv6 on,
`always_use_https` on, `min_tls_version` 1.0, `security_level` high. Nameservers:
`hank.ns.cloudflare.com`, `uma.ns.cloudflare.com`._

**Cloudflare Tunnels (9 active, all remotely managed, WARP routing enabled on all):**

| Tunnel name           | ID (prefix) | Connector host   | Origin IP(s)                      | Version   | Colos                      | Public hostname ingress                                                                                                                                                                                                                                              |
| --------------------- | ----------- | ---------------- | --------------------------------- | --------- | -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `home-proxy`          | `4b602332`  | proxy (CT 110)   | `2600:1700:2f71:c80::110`         | 2025.11.1 | lax08, sjc11, lax11        | `mdm.goodkind.io` -> `https://mdm.home.goodkind.io`; `home-assistant-ext.goodkind.io` -> `https://assistant.home.goodkind.io`; `cloudflared-opnsense-pkg.goodkind.io` -> `https://localhost`; `plane.goodkind.io` -> `https://plane.home.goodkind.io`; catch-all 404 |
| `home-mwan`           | `be52c73b`  | mwan (VM 113)    | `2604:5500:c271:8000::72b`        | 2025.11.1 | lax09, sjc10, sjc05        | WARP-only (no public hostnames)                                                                                                                                                                                                                                      |
| `home-mini`           | `fe0e094b`  | mini             | `136.25.91.242`, `104.57.226.193` | 2026.3.0  | sjc01, sjc06, sjc08, sjc10 | WARP-only (no public hostnames)                                                                                                                                                                                                                                      |
| `home-nas`            | `1fb61f17`  | nas              | `104.57.226.193`                  | 2025.11.1 | sjc01, sjc06, sjc11        | WARP-only (no public hostnames)                                                                                                                                                                                                                                      |
| `home-vault`          | `50453c03`  | vault            | `104.57.226.193`                  | 2025.11.1 | sjc06, sjc07               | `vault-test.goodkind.io` -> `https://localhost:8006`; catch-all 404                                                                                                                                                                                                  |
| `home-berylax`        | `4a216d14`  | berylax          | `158.247.70.1`                    | 2025.11.1 | sjc01, sjc08, sjc10        | WARP-only (no public hostnames)                                                                                                                                                                                                                                      |
| `suburban-hypervisor` | `e83d2644`  | suburban         | `174.166.126.204`                 | 2025.11.1 | ewr05, ewr11, ewr12, ewr16 | WARP-only (no public hostnames)                                                                                                                                                                                                                                      |
| `suburban-pikvm`      | `6e73b6d4`  | suburban (pikvm) | `174.166.126.204`                 | 2025.8.1  | ewr11, ewr12, ewr13, ewr16 | `suburban-pikvm.goodkind.io` -> `https://localhost:443`; catch-all 404                                                                                                                                                                                               |
| `suburban-mom`        | `2267fc65`  | suburban (mom)   | `174.166.126.204`                 | 2025.11.1 | ewr01, ewr05, ewr07, ewr15 | Catch-all 404 only                                                                                                                                                                                                                                                   |

Notes on tunnel deployment: `home-proxy` and `home-mwan` are deployed via Ansible
(`install-cloudflared.yml` tasks, token-based). Both run with `--edge-ip-version 6`.
The proxy connector's version is 2025.11.1 (outdated; dashboard warns to upgrade to 2026.3.0).
The `home-mini`, `home-nas`, `home-vault`, and `home-berylax` connectors are not deployed via
the Ansible playbooks in this repo; they appear to be standalone installs on those hosts.
Tunnel tokens are stored in Ansible Vault (`vault_cloudflared_tunnel_token`) for the proxy and
on the Semaphore controller (`/var/lib/semaphore/tokens/mwan/cloudflared/token`) for mwan.

**WARP tunnel routes (private network access via Cloudflare WARP client):**

| Network                                  | Tunnel                | Comment                  |
| ---------------------------------------- | --------------------- | ------------------------ |
| `10.250.0.0/16`                          | `home-mini`           | pound-lan (home network) |
| `10.250.0.110/32`                        | `home-proxy`          | proxy-v4-legacy          |
| `10.250.0.113/32`                        | `home-mwan`           | mwan-mgmt                |
| `10.250.250.1/32`                        | `home-mwan`           | mwan-wanbr               |
| `3d06:bad:b01::/56`                      | `home-mini`           | home v6 (entire /56)     |
| `3d06:bad:b01::110/128`                  | `home-proxy`          | proxy v6                 |
| `3d06:bad:b01::254/128`                  | `home-vault`          | vault v6                 |
| `3d06:bad:b01:1::3/128`                  | `home-nas`            | nas v6                   |
| `3d06:bad:b01:1:9ab7:85ff:fe22:251f/128` | `home-nas`            | nas SLAAC v6             |
| `3d06:bad:b01:fe::1/64`                  | `home-mwan`           | mwan-wanbr6              |
| `3d06:bad:b01:300::/56`                  | `home-berylax`        | berylax                  |
| `10.240.0.0/24`                          | `suburban-hypervisor` | suburban-net             |
| `10.240.0.57/32`                         | `suburban-pikvm`      | pikvm                    |
| `10.240.0.121/32`                        | `suburban-mom`        | Julia's iMac             |
| `10.240.10.0/24`                         | `suburban-hypervisor` | suburban-wg              |
| `10.240.240.0/24`                        | `suburban-hypervisor` | suburban-vmnet           |
| `2601:84:837c:a160::/60`                 | `suburban-hypervisor` | suburban v6 Xfinity      |
| `3d06:bad:b01:200::/56`                  | `suburban-hypervisor` | suburban-vmnet6          |
| `3eef::/48`                              | `suburban-hypervisor` | suburban-test-vmnet      |

**Cloudflare Load Balancers:**

| LB hostname            | Steering | Default pools                    | Fallback pool      |
| ---------------------- | -------- | -------------------------------- | ------------------ |
| `lb-home.goodkind.io`  | random   | `sf-webpass-1335`, `sf-att-1335` | `sf-mbrains6-1335` |
| `lb-home6.goodkind.io` | random   | `sf-1335-ipv6`                   | `sf-mbrains6-1335` |

Pool origins:

| Pool               | Origins                                                              |
| ------------------ | -------------------------------------------------------------------- |
| `sf-webpass-1335`  | `webpass-1335.goodkind.io` (`136.25.91.242`)                         |
| `sf-att-1335`      | `att-1335.goodkind.io` (`104.57.226.193`)                            |
| `sf-1335-ipv6`     | `att6-1335.goodkind.io`, `webpass6-1335.goodkind.io`                 |
| `sf-mbrains6-1335` | `mbrains6-1335.goodkind.io` (fallback; IPv6 absent since 2026-01-22) |
| `suburban-128-nj`  | `suburban.goodkind.io` (`10.240.0.148`), not used in any active LB   |

`home.goodkind.io` and `1335-sf.goodkind.io` both CNAME to `lb-home.goodkind.io`. The LBs
are not proxied; they resolve directly to WAN IPs for the home network. This provides
multi-WAN failover at the DNS layer, separate from the mwan VM's routing-level failover.

**Cloudflare Pages:**

| Site             | Pages subdomain            | Custom domain                    | Proxied |
| ---------------- | -------------------------- | -------------------------------- | ------- |
| `goodkind-io`    | `goodkind-io.pages.dev`    | `goodkind.io`, `www.goodkind.io` | Yes     |
| `go-goodkind-io` | `go-goodkind-io.pages.dev` | `go.goodkind.io`                 | Yes     |

**Workers:**

| Worker name                   | Created    | Purpose                                                    |
| ----------------------------- | ---------- | ---------------------------------------------------------- |
| `goodkind-io-catchall-worker` | 2026-01-10 | Email routing catch-all (stub `email()` handler, no logic) |

**Email routing:** A single catch-all rule drops all inbound email to the zone. The Worker's
`email()` handler is an empty stub. Outbound email uses Google Workspace MX records and
SMTP2GO for transactional mail (SPF, DKIM, DMARC configured).

**DNS records of note (73 total in zone `goodkind.io`):**

- Wildcard `*.home.goodkind.io` (A + AAAA) points to proxy (`10.250.0.110` / `3d06:bad:b01::110`), not proxied.
- Tunnel CNAMEs: `cloudflared-opnsense-pkg`, `home-assistant-ext`, `mdm`, `plane`, `vault-test`, `suburban-pikvm` all CNAME to `*.cfargotunnel.com` (proxied).
- Google Workspace: `calendar`, `docs`, `mail` CNAME to `ghs.googlehosted.com` (proxied). MX records point to `aspmx.l.google.com` and alternates.
- iCloud custom domain: two `apple-domain` TXT records for verification, `sig1._domainkey` CNAME for DKIM.
- SMTP2GO: `em805909`, `link`, `s805909._domainkey` CNAMEs for SPF/DKIM/tracking on both root and `mail.goodkind.io`.
- DMARC: `p=reject` on root and `mail.goodkind.io`; `p=none` on `old-email.goodkind.io`.
- JetKVM devices: `vault-jetkvm` and `nas-jetkvm` AAAA records point to Monkeybrains link-local-derived addresses.
- Suburban: `128-nj`, `hypervisor.suburban`, `router.suburban`, `jetkvm.suburban`, `mom6.suburban` records for the NJ site.
- `moto.goodkind.io` CNAMEs to `edge.sfo.the-cupcake-factory.com` (not proxied).
- `blog.goodkind.io` CNAMEs to `domains.tumblr.com` (not proxied).
- `66868087.goodkind.io` CNAMEs to `google.com` (Google domain verification, ttl 3600).

**Developer platform resources:** No KV namespaces, no R2 buckets, no D1 databases,
no Hyperdrive configs.

---

## Open Questions and Known Work Items

### Action required

**Make email work from all managed hosts.** The `prep-guests.yml` msmtprc template was updated
2026-03-22 to include `auth login` (required for msmtp 1.8.28+ against SMTP2GO). Running
`prep-guests.yml` against each host below deploys the account and config atomically.

| Host                              | Email state                                           | Action                                               |
| --------------------------------- | ----------------------------------------------------- | ---------------------------------------------------- |
| mwan (VM 113)                     | Working (fixed 2026-03-22)                            | None                                                 |
| dns64, consul, mc, proxy, adguard | msmtprc present but missing `auth login`              | Re-run `prep-guests.yml`                             |
| unifi, grommunio, pvd, ansible    | No msmtprc at all                                     | Run `prep-guests.yml` for first time                 |
| mini, nas                         | msmtp installed, no msmtprc; not in Ansible inventory | Add to inventory then run, or deploy manually        |
| vault                             | No msmtp; uses Proxmox datacenter email               | Configure via PVE datacenter settings                |
| home-assistant                    | HAOS; no msmtp                                        | Configure via HA notification integrations if needed |
| suburban                          | No msmtp                                              | Address after WireGuard is restored                  |

**Fix `consul_arch: arm64` in `consul_servers.yml` and `deploy-consul-external.yml`.** Both files
hardcode `arm64`; the actual target hosts are `amd64`.

**Reduce deployment friction for mwan scripts.** Running `deploy-mwan.yml` to push a single
updated script requires the full playbook, which is slow. Current workaround is manual `scp`
followed by `systemctl restart`. Options worth considering: a thin Makefile/Rake wrapper for
named components; moving static scripts into `/opt/scripts` so they are auto-updated via the
existing pull timer; or a pull-based render model for templated configs. The two-tier split
(static scripts in `/opt/scripts`, templated configs deployed by Ansible) is likely the right
long-term shape.

**Weekly "I am alive" digest emails from each managed host.** Each host with a working `send-email`
stack should send a weekly summary covering uptime, disk, memory, active and failed units, and
last package update date. A natural home is a `weekly-report.sh` in `/opt/scripts`, triggered
by a systemd timer deployed via `prep-guests.yml`.

**Centralised health and stats aggregator across all services.** Per-host monitoring today (mwan
emails on WAN transitions, systemd failed-unit check fires per-host) leaves no single place to
see aggregate state. Consul health checks are a natural fit since Consul is already deployed.
An alternative is a push model where each host posts JSON to a collector on the Ansible
container. The right choice depends on whether Consul port 8301 connectivity is stable first.

### Under investigation

**AT&T IPv6 outbound broken.** AT&T provides PD `/60` `2600:1700:2f71:c80::/60` and the gateway
(`fe80::a2f3:e4ff:fe70:e30`) pings fine. But `ping6 8.8.8.8` is 100% loss and traceroute stops
after 3 AT&T hops. The NPT rule for AT&T egress or the PD lease routing may not be active.
Worth checking `networkctl status enatt0.3242` and whether the nftables NPT rule for the AT&T
prefix is present.

**WireGuard suburban tunnel stale.** Suburban is reachable at `3d06:bad:b01:200::254` (confirmed
up 72+ days). The WireGuard tunnel shows the last handshake was ~7 days ago despite a 25-second
keepalive. One possibility is the OPNsense side stopped acknowledging suburban peer keepalives
after the handshake aged out. The endpoint suburban dials is `[2604:5500:c271:be00::1]:51820`
(Webpass NPT external).

**Monkeybrains IPv6 absent since 2026-01-22.** IPv6 was working at 23:43:36 UTC and gone 57
seconds later. Source: `mwan-debug.log` parsed on 2026-03-23. The previously-issued prefix was
`2607:f598:d3e8:3100::/56`. The CPE/gateway (`fe80::f61e:57ff:fe06:4983`) no longer responds
to NDP and no RA is sent on the segment. IPv4 via CG-NAT is fine. This is provider-side; no
mwan action is needed beyond retaining the historical prefix config.

**berylax `3d06:bad:b01:300::1/56` routability unconfirmed.** The address is statically set in
UCI on the device. OPNsense has no route for `3d06:bad:b01:300::/56`. How packets reach this
address from the home network has not been confirmed. The Cloudflare tunnel `home-berylax`
does route `3d06:bad:b01:300::/56` via WARP, so the prefix is reachable from WARP clients
even if not routable on the local network.

**cloudflared on mwan: connIndex=2 flapping.** As of 2026-03-28, tunnel `home-mwan` shows 4/4
connections active (sjc05, sjc10, lax09) per the API, but connIndex=2 periodically drops with
`"accept stream listener encountered a failure while serving"` and reconnects within ~2 minutes.
The tunnel has no public hostname ingress; it serves WARP routing only (mwan-mgmt `10.250.0.113/32`,
mwan-wanbr `10.250.250.1/32`, mwan-wanbr6 `3d06:bad:b01:fe::1/64`). The connector version is
2025.11.1, which is outdated (current: 2026.3.0). Upgrading may reduce the flapping.

**cloudflared on proxy: outdated version.** Proxy (CT 110) runs cloudflared 2025.11.1; the
dashboard warns to upgrade to 2026.3.0. The connector has been running since 2026-01-19 (over
2 months). `connIndex=3` also flaps periodically with timeout errors but reconnects quickly.

**UniFi controller Consul alias with unusual address.** OPNsense alias `unifi_controller` includes
`3d06:bad:b01:6465::102`. An earlier attempt to reach the UniFi controller via a NAT64-synthesized
address was explored but abandoned because the routing became too complex. This alias is likely
stale and should be cleaned up.

### Repo housekeeping

1. `logstash/` should be removed. Confirmed retired; no live Logstash process anywhere.
2. `nanomdm/` should be removed or a container should be stood up. The Traefik backend and
   group vars point to a stale address.
3. `grommunio` deploy situation needs clarifying. Container is live but playbook is not wired
   into any workflow.
4. `proxmox/` vs `mwan/proxmox/`: the top-level `proxmox/` holds static originals; no playbook
   deploys from it. Safe to remove after verifying no unique content.
5. Migrate remaining containers (102, 104, 105, 107, mwan VM) to `package-updater` timer.

---

## LLM Writing Guidelines

Treat every statement here as guidance for how to write and ingest material in this repo, not
as asserted fact about any specific host. If anything conflicts with a primary source (a file
in git, a man page, or output you reproduced), prefer the primary source and treat this
document as stale until someone updates it.

**Default stance:** Assume claims are uncertain until tied to evidence. Prefer "it appears" when
the basis is a single file or log snippet. Prefer "this suggests" when inferring from several
weak signals. State "no verifiable source is available" explicitly rather than filling gaps
with confidence.

**Evidence discipline:** For each non-trivial statement, tie it to a repo path, a command with
representative output, or an external URL. When treating something as proof, name the evidence
first, then give the conclusion qualified by that evidence.

**Investigatory tone:** Write as if the reader is joining an ongoing investigation. Prefer "it
may be worth checking" over "you must". Offer options and describe what to observe if someone
tries them.

**Conflicts between sources:** List disagreements without forcing resolution. Note which source
is usually authoritative for that layer only if a cited policy or comment in the repo supports
it. Suggest a single reproducible check that would break the tie if one exists.

**Staleness:** Every statement should carry implicit scope (environment, date or git ref if
known, and what would make the note obsolete). Infra drifts; "last verified" beats
"timeless truth".

**Secrets:** Never copy tokens, passwords, private keys, or session cookies. Refer to vault keys,
env var names, or rotation procedures. If a secret's location must be described, use path plus
permission model, not content.

**Shell and code notes:** For shell, note bash vs zsh when expansion or builtins differ. For
Ansible, follow the quality rules in `.cursor/rules/ansible-quality.mdc`. For mwan scripts,
follow the shell style rules in `.cursor/rules/mwan.mdc`.

---

_Maintainers: keep this file free of emdash and endash characters. Use commas, parentheses,
colons, or separate sentences instead._
