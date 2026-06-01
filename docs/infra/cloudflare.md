# Cloudflare

*Queried via API 2026-03-28. Account: Alexander Goodkind (`ee7d7ca7d611ef8c2a07885e8362de0c`).
Zone `goodkind.io` is on the Pro plan, SSL mode strict, TLS 1.3 0-RTT, HTTP/3 on, IPv6 on,
`always_use_https` on, `min_tls_version` 1.0, `security_level` high. Nameservers:
`hank.ns.cloudflare.com`, `uma.ns.cloudflare.com`.*

**Cloudflare Tunnels (9 active, all remotely managed, WARP routing enabled on all):**

| Tunnel name           | ID (prefix) | Connector host    | Public hostname ingress                                                                                                                                                                                                                                              |
| --------------------- | ----------- | ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `home-proxy`          | `4b602332`  | proxy (CT 110)    | `mdm.goodkind.io` -> `https://mdm.home.goodkind.io`; `home-assistant-ext.goodkind.io` -> `https://assistant.home.goodkind.io`; `cloudflared-opnsense-pkg.goodkind.io` -> `https://localhost`; `plane.goodkind.io` -> `https://plane.home.goodkind.io`; catch-all 404 |
| `home-mwan`           | `be52c73b`  | mwan (VM 113)     | WARP-only                                                                                                                                                                                                                                                            |
| `home-mini`           | `fe0e094b`  | mini              | WARP-only                                                                                                                                                                                                                                                            |
| `home-nas`            | `1fb61f17`  | nas               | WARP-only                                                                                                                                                                                                                                                            |
| `home-vault`          | `50453c03`  | vault             | `vault-test.goodkind.io` -> `https://localhost:8006`; catch-all 404                                                                                                                                                                                                  |
| `home-berylax`        | `4a216d14`  | berylax (offline) | WARP-only                                                                                                                                                                                                                                                            |
| `suburban-hypervisor` | `e83d2644`  | suburban          | WARP-only                                                                                                                                                                                                                                                            |
| `suburban-pikvm`      | `6e73b6d4`  | suburban (pikvm)  | `suburban-pikvm.goodkind.io` -> `https://localhost:443`; catch-all 404                                                                                                                                                                                               |
| `suburban-mom`        | `2267fc65`  | suburban (mom)    | Catch-all 404 only                                                                                                                                                                                                                                                   |

Notes on tunnel deployment: `home-proxy` and `home-mwan` are deployed via Ansible
([ansible/playbooks/tasks/install-cloudflared.yml](../../ansible/playbooks/tasks/install-cloudflared.yml)
tasks, token-based). Both run with `--edge-ip-version 6`.
The `home-mini`, `home-nas`, and `home-vault` connectors are not deployed via
the Ansible playbooks in this repo; they appear to be standalone installs on those hosts.
The `home-berylax` connector is indefinitely offline for now. Historical berylax
routing state lives in [berylax.md](berylax.md).
Tunnel tokens are stored in Ansible Vault: `vault_cloudflared_tunnel_token` for the proxy and
`vault_mwan_cloudflared_token` for mwan.

**WARP tunnel routes (private network access via Cloudflare WARP client):**

| Network                                  | Tunnel                | Comment                                      |
| ---------------------------------------- | --------------------- | -------------------------------------------- |
| `10.250.0.0/16`                          | `home-mini`           | pound-lan (home network)                     |
| `10.250.0.110/32`                        | `home-proxy`          | proxy-v4-legacy                              |
| `10.250.0.113/32`                        | `home-mwan`           | mwan-mgmt                                    |
| `10.250.250.1/32`                        | `home-mwan`           | mwan-wanbr                                   |
| `3d06:bad:b01::/56`                      | `home-mini`           | home v6 (entire /56)                         |
| `3d06:bad:b01::110/128`                  | `home-proxy`          | proxy v6                                     |
| `3d06:bad:b01::254/128`                  | `home-vault`          | vault v6                                     |
| `3d06:bad:b01:1::3/128`                  | `home-nas`            | nas v6                                       |
| `3d06:bad:b01:1:9ab7:85ff:fe22:251f/128` | `home-nas`            | nas SLAAC v6                                 |
| `3d06:bad:b01:fe::1/64`                  | `home-mwan`           | mwan-wanbr6                                  |
| `3d06:bad:b01:300::/64`                  | `home-berylax`        | berylax LAN (offline; historical WARP route) |
| `10.240.0.0/24`                          | `suburban-hypervisor` | suburban-net                                 |
| `10.240.0.57/32`                         | `suburban-pikvm`      | pikvm                                        |
| `10.240.0.121/32`                        | `suburban-mom`        | Julia's iMac                                 |
| `10.240.10.0/24`                         | `suburban-hypervisor` | suburban-wg                                  |
| `10.240.240.0/24`                        | `suburban-hypervisor` | suburban-vmnet                               |
| `provider v6 Xfinity (/60)`              | `suburban-hypervisor` | suburban v6 Xfinity                          |
| `3d06:bad:b01:200::/56`                  | `suburban-hypervisor` | suburban-vmnet6                              |
| `3eef::/48`                              | `suburban-hypervisor` | suburban-test-vmnet                          |

**Cloudflare Load Balancers:**

| LB hostname            | Steering | Default pools                    | Fallback pool      |
| ---------------------- | -------- | -------------------------------- | ------------------ |
| `lb-home.goodkind.io`  | random   | `sf-webpass-1335`, `sf-att-1335` | `sf-mbrains6-1335` |
| `lb-home6.goodkind.io` | random   | `sf-1335-ipv6`                   | `sf-mbrains6-1335` |

Pool origins:

| Pool               | Origins                                                              |
| ------------------ | -------------------------------------------------------------------- |
| `sf-webpass-1335`  | `webpass-1335.goodkind.io`                                           |
| `sf-att-1335`      | `att-1335.goodkind.io`                                               |
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
