# MWAN runtime overview

Single-VM multi-WAN load balancer for AT&T (802.1X + VLAN) and Webpass on
goodkind.io, with optional Monkeybrains failover. This page describes how
production MWAN looks today. Go and binary rules live in
[docs/mwan/go-standards.md](go-standards.md); shell and OPNsense script
conventions live in [docs/mwan/script-style.md](script-style.md); per-host
layout lives in [docs/infra/mwan-layout.md](../infra/mwan-layout.md);
runtime-correctness gotchas live in
[docs/opnsense/operational-notes.md](../opnsense/operational-notes.md).

## Architectural shape

OPNsense sees one upstream: MWAN. MWAN owns all WAN complexity (authentication,
policy routing, NAT44 1:1, NPTv6, health checks, and BGP-driven HA).

- **Outbound IPv4**: OPNsense SNATs downstream RFC1918 into a small MWAN-side
  range; MWAN marks new flows per WAN and applies 1:1 SNAT to each WAN's
  delegated public /29.
- **Outbound IPv6**: downstream uses an internal-only `/60`; MWAN marks new
  flows per WAN and applies NPT to each WAN's DHCPv6-PD `/60`.
- **Inbound**: traffic to either WAN's public space is translated on MWAN
  (DNAT or reverse-NPT) and forwarded to OPNsense.
- **Failover**: BGP-based. Both the primary MWAN VM and the failover LXC peer
  with OPNsense FRR and announce default routes when healthy. OPNsense uses a
  route-map to prefer the primary. See [HA failover](#ha-failover-bgp) below.

For per-host details (which guest carries which role, internal prefix, BGP
ASN, interface naming), see
[docs/infra/mwan-layout.md](../infra/mwan-layout.md). For exact public IPv4
mappings, addressing, and ISP-level detail, see
[docs/infra/overview.md](../infra/overview.md).

## The monolith and its runtime services

All Go code is one binary built from [mwan/go/cmd/mwan/](../../mwan/go/cmd/mwan/).
The full subcommand list and ownership boundary live in
[docs/mwan/go-standards.md](go-standards.md#monolith-contract). The runtime
units that matter day-to-day:

- **`mwan agent`** runs inside the MWAN VM and the failover LXC. It hosts the
  gRPC surface (vsock + TCP), drives the embedded GoBGP speaker, and applies
  health-driven announce or withdraw decisions for the WAN default routes.
- **`mwan watchdog`** runs on the Proxmox host. It probes connectivity from
  the host, compares hash and deploy-timestamp signals, and either alerts or
  rolls the VM back to a known-good snapshot. `mwan watchdog failover` forces
  the BGP failover path.
- **`mwan ifmgr`** runs on each MWAN host (the VM and the failover LXC) and
  applies interface-mode-specific config based on `[ifmgr].role` in
  `/etc/mwan/config.toml`.
- **`mwan health-check`** is a one-shot probe used both interactively and as
  the worker the watchdog calls into.
- **`mwan opnsense`** runs on the OPNsense VM (FreeBSD build) and mutates
  `config.xml` over virtio-serial via gRPC. **`mwan opnsense host serve`**
  runs on the Proxmox host as the Unix-socket bridge to the OPNsense VM's
  `mwanrpc` chardev.

Shell-era control flow (per-interface `networkd-dispatcher` hooks plus
[update-routes.sh](../../mwan/scripts/update-routes.sh) and
[update-npt.sh](../../mwan/scripts/update-npt.sh)) still exists on the MWAN VM for
data-plane convergence (policy routes and the dynamic `ip6 nat` table). Those
are described in [data-plane convergence](#data-plane-convergence) below. They
are not the source of truth for failover; the BGP speaker inside `mwan agent`
is.

## HA failover (BGP)

Production failover is BGP-based. The agent embeds GoBGP, peers with OPNsense
FRR over iBGP, and announces a default route (`0.0.0.0/0`, `::/0`) when
healthy. OPNsense runs FRR (`os-frr`) with a route-map that prefers the primary
via higher local-pref. The watchdog withdraws routes via the agent's gRPC API
when health degrades; if the agent crashes, the BGP session drops and OPNsense
converges to the backup within the hold timer.

All BGP parameters (ASN, router ID, neighbors, timers, prefixes) live in the
`[bgp]` section of `/etc/mwan/config.toml`.

Failover decision matrix:

| Primary Internet | Failover LXC Internet | Cause                      | Watchdog action                          |
| ---------------- | --------------------- | -------------------------- | ---------------------------------------- |
| OK               | OK                    | Normal                     | No action                                |
| OK               | DOWN                  | Failover WAN issue         | Alert only                               |
| DOWN             | OK                    | Primary config or WAN down | Withdraw primary routes or force backup  |
| DOWN             | DOWN                  | Upstream outage            | Alert only                               |
| Agent down       | OK                    | Primary agent crash        | BGP session drops; OPNsense converges    |

`mwan watchdog failover` triggers the BGP failover path immediately. There is
no keepalived, VRRP, VIP, VMAC, or macvlan in the failover path; that work was
removed when BGP replaced VRRP.

### BGP graceful restart

BGP Graceful Restart (RFC 4724) lets the agent restart its BGP process without
flapping its routes in the helper. The helper retains the restarter's prefixes
for `restart_time` seconds and only flushes them if the session does not come
back. The agent restarts on every deploy, so GR is the path to zero-flap
deploys; the 2026-05-07 deploy measured a 1.7s WAN outage at agent restart
with GR off.

The wiring lives in [mwan/go/internal/bgp/speaker.go](../../mwan/go/internal/bgp/speaker.go),
fed by `BGPGracefulRestart` in
[mwan/go/internal/bgp/config.go](../../mwan/go/internal/bgp/config.go), which
mirrors the loader struct in
[mwan/go/internal/config/config.go](../../mwan/go/internal/config/config.go).
When GR is enabled the speaker attaches `GracefulRestart` to the GoBGP global
config, sets `MpGracefulRestart` on each AFI/SAFI, mirrors `GracefulRestart`
onto every peer, and passes `AllowGracefulRestart=true` on `Stop`. The agent
shutdown path skips the pre-emptive `WithdrawDefault` call when GR is on,
because an explicit WITHDRAW would defeat GR (FRR would drop the route
immediately); pre-withdraw only runs when GR is off.

Configuration lives in `[bgp.graceful_restart]` in `/etc/mwan/config.toml`:
`enabled` (default `true`), `restart_time` (uint32 seconds, default `30`,
capped at `600` by the loader), `notification_enabled` (default `true`). The
defaults are baked into `config.BGPDefaults` so an empty
`[bgp.graceful_restart]` block matches documented behaviour.

The OPNsense FRR side has its own toggle:
`OPNsense.quagga.bgp.graceful = '1'` in `/conf/config.xml`. Production
operators flip it via the OPNsense GUI under Routing -> BGP -> General. The
testbed has no GUI from the controller, so the operator drives the
`mwan-opnsense` gRPC API to mutate `config.xml` directly, then runs
`configctl quagga reload bgp`. Verify with:

```bash
vtysh -c 'show running-config router bgp' | grep 'bgp graceful-restart'
```

BFD is the natural follow-up. GR is only safe-by-default with BFD when a real
WAN link dies inside the GR window, because without BFD the helper holds stale
routes for the full `restart_time`. There is no BFD wired today; fast WAN
failure detection relies on the watchdog gRPC withdraw path.

## Watchdog rollback design

The watchdog runs on the Proxmox host. It bases the rollback decision on
**whether config recently changed**, not on per-interface probes from inside
the VM. If config changed and connectivity then broke, config is the most
probable cause; if config has been stable and connectivity breaks, it is
probably external.

Two signals count as a recent config change:

1. **Deploy timestamp** (`/var/run/mwan-last-deploy`), written by the deploy
   playbook before pushing new config.
2. **Config hash change**, detected by `checkConfigHash` when the composite
   hash reported by `mwan-agent` changes.

Decision matrix:

| Connectivity fails? | Recent deploy timestamp? | Recent hash change? | Stable before? | Action                              |
| ------------------- | ------------------------ | ------------------- | -------------- | ----------------------------------- |
| Yes                 | Yes (within 60s)         | -                   | -              | Grace period; wait for reboot       |
| Yes                 | Yes (past 60s grace)     | -                   | -              | Connectivity timeout, then rollback |
| Yes                 | No                       | Yes (within window) | Yes            | Connectivity timeout, then rollback |
| Yes                 | No                       | No                  | Yes            | Test LXC, then failover or wait     |
| No                  | -                        | -                   | -              | Healthy; normal monitoring          |

Grace period:

- Deploy timestamp detected: 60s grace, then the normal connectivity timeout
  (`CONNECTIVITY_TIMEOUT_SECONDS`, default 30s) begins.
- Hash-only changes get no grace period because they should not cause reboots.

Hash-change recency window: a hash change is "recent" for
`DEPLOY_WINDOW_MINUTES` (default 30). Anything older is treated as external.

### Snapshots

Two snapshot types with different owners:

- **`pre-deploy-*`** snapshots are owned by the deploy playbook. The playbook
  must create `pre-deploy-<unix-timestamp>` before pushing any config to the
  MWAN VM. Without it, a fresh or recently changed VM may have no rollback
  target until a `known-good-*` snapshot is created (which takes many healthy
  probe cycles).
- **`known-good-*`** snapshots are owned by the watchdog and taken
  automatically after the system has been healthy and stable for a sustained
  period.

Rollback target order is: latest `pre-deploy-*`, then most recent
`known-good-*`. If neither exists, the watchdog alerts but does not recover.

`known-good-*` is taken when all are true:

1. Healthy for `SNAPSHOT_HEALTHY_THRESHOLD` consecutive probe cycles
   (default 20).
2. Config hash stable for `DEPLOY_WINDOW_MINUTES`.
3. No recent deploy timestamp (outside the deploy window).
4. At least `MIN_SNAPSHOT_INTERVAL_SECONDS` (default 300s) since the previous
   snapshot.

Pruning keeps at most `MAX_KNOWN_GOOD_SNAPSHOTS` (default 3) and
`MAX_TOTAL_SNAPSHOTS` (default 15), deleting oldest first.

Proxmox snapshot names are capped at 40 characters and longer names truncate
silently. Put the full intent in `--description` and keep the name short. See
[docs/opnsense/operational-notes.md](../opnsense/operational-notes.md) for the
`--vmstate 1` rule for testbed snapshots, which applies equally to MWAN
snapshots: do not save RAM, because rollback then resumes with stale
networking and clock state.

## Data-plane convergence

These pieces live on the MWAN VM and converge the data plane independently of
the BGP speaker:

- `/usr/local/bin/update-routes.sh` programs `ip rule` and per-WAN routing
  tables. It is called by `networkd-dispatcher` "routable" hooks, by
  `mwan-update-routes.service` as a boot safety net, and by
  `mwan health-check --daemon` when a WAN transitions healthy or unhealthy.
- `/usr/local/bin/update-npt.sh` programs the IPv6 NPT and DNPT rules in the
  runtime `table ip6 nat`. It is called by the dispatcher hook, by
  `mwan-update-npt.service` as a boot safety net, and again after deploy-time
  `nftables` reloads.
- `mwan-health.service` (running `mwan health-check --daemon`) is the source
  of WAN health state at `/var/run/mwan-health.state`. WAN health transitions
  call [update-routes.sh](../../mwan/scripts/update-routes.sh) so the system
  converges back to the healthy WAN automatically once it recovers.

Lock files in `/run/...` serialise writers, so dispatcher hooks, safety-net
services, and the health daemon cannot collide on `ip rule`, `ip route`, or
`nft` updates.

Failure modes worth knowing:

- **Empty `table ip6 nat`** means runtime programming did not happen or was
  flushed. The recovery is `mwan-update-npt.service` or running
  [update-npt.sh](../../mwan/scripts/update-npt.sh) with `<wan-if> <wan-pd>` directly.
- **Boot ordering**: PCI/virtio devices and AT&T 802.1X authentication can be
  late, and `networkd-dispatcher` is event-driven (no replay). The
  safety-net services are mandatory, not optional.

For terminology, prefer **healthy / unhealthy / unknown** for WAN state. Avoid
**up / down** for health, because that conflicts with `ip link` administrative
state.

## Email and alert routing

[mwan/go/internal/notify/](../../mwan/go/internal/notify/) is the single
chokepoint for outbound email from MWAN code.
The contract: every email exits through `notify.Notifier`, which owns
per-(kind, key) state-change suppression and per-kind repeat cadence. Direct
calls to `email.Sender.Send` and the slog `email_handler` path are removed by
the relevant MWAN-132 slices.

`SMTP2GO_API_KEY` is injected via systemd `EnvironmentFile=/etc/mwan/secrets.env`
rather than templated into `config.toml`. That env-var injection contract is
tracked under MWAN-131 and is also documented in
[docs/ansible/secrets.md](../ansible/secrets.md).

In-flight plan and full routing detail: see
[docs/plans/mwan-email-routing.plan.md](../plans/mwan-email-routing.plan.md).

## Tracing

MWAN scripts emit structured JSON logs to `/var/log/mwan-debug.log` when
`mwan_debug_logging: true`. Each log line includes a `traceId` so events
across `systemd-networkd`, `networkd-dispatcher`,
[update-routes.sh](../../mwan/scripts/update-routes.sh),
[update-npt.sh](../../mwan/scripts/update-npt.sh), and `health-check` can be
correlated.

Trace ID sources:

- `mwan-trace-boot.service` writes `/run/mwan-trace-id` and
  `/var/lib/mwan/trace-id` at boot.
- The deploy playbook writes the same files at the start of deploy.

Quick check on MWAN:

```bash
cat /run/mwan-trace-id
tail -n 200 /var/log/mwan-debug.log
```

## Operational quick reference

On MWAN:

```bash
ssh root@mwan.home.goodkind.io
wpa_cli status
systemctl status wpa_supplicant-mwan systemd-networkd networkd-dispatcher \
  nftables mwan-health cloudflared
mwan health-check --status
/usr/local/bin/mwan-debug
```

IPv6 sanity checks:

```bash
ip -6 route show table 100
ip -6 route show table 200
ip -6 rule show
nft -a list chain ip6 nat postrouting
nft -a list chain ip6 nat prerouting
```

For troubleshooting AT&T 802.1X, Webpass DHCP, virtio-serial wedges,
OPNsense REST behaviour, and the upgrade-snapshot pitfalls, see
[docs/opnsense/operational-notes.md](../opnsense/operational-notes.md).
