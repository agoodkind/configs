# MWAN runtime overview

MWAN is a single-VM router that spreads outbound traffic across three wide-area networks and fails over between them, so OPNsense sees one upstream and none of the WAN complexity. MWAN owns the authentication, the policy routing, the address translation, the health checks, and the BGP-driven failover.

## Architecture

OPNsense sends all of its traffic to MWAN and treats it as the only upstream, for both IPv4 and IPv6.

Outbound IPv4 leaves in two hops. OPNsense translates its downstream private addresses into a small range on the MWAN link, and MWAN marks each new flow for one WAN and applies a one-to-one translation onto that WAN's delegated public block.

Outbound IPv6 works the same way with prefix translation. Downstream uses an internal-only IPv6 range, and MWAN marks each new flow for one WAN and rewrites its prefix onto that WAN's delegated prefix.

Inbound traffic to a WAN's public space is translated back on MWAN and forwarded to OPNsense.

Failover is driven by BGP. The primary MWAN VM and the failover LXC both peer with the OPNsense routing daemon and announce a default route while they are healthy, and OPNsense prefers the primary. The failover behavior is below, and the per-host roles are on the layout page.

## The monolith and its runtime services

All MWAN code is one Go binary, and its subcommands each run one service on the host whose role needs it.

`mwan agent` runs on the MWAN VM and the failover LXC. It hosts the gRPC surface over both a virtual socket and TCP, drives the embedded BGP speaker, and announces or withdraws the WAN default routes as health changes.

`mwan watchdog` runs on the Proxmox host, outside the VM. It probes connectivity from the host, and when a recent change has broken it, rolls the VM back to a known-good snapshot; otherwise it alerts. `mwan watchdog failover` forces the BGP failover path.

`mwan ifmgr`, the interface manager, runs on each MWAN host and applies the interface configuration its role calls for, read from `/etc/mwan/config.toml`.

`mwan health-check` is a one-shot connectivity probe, run by hand or as the worker the watchdog calls.

`mwan opnsense` runs on the OPNsense VM and edits `config.xml` over the serial channel, and `mwan opnsense host serve` runs on the Proxmox host as the bridge to that channel.

The BGP speaker inside `mwan agent` is the source of truth for failover. A set of shell hooks and safety-net services also converge the data plane, the policy routes and the dynamic translation table, and those are covered under data-plane convergence.

## Failover over BGP

The agent peers with the OPNsense routing daemon over internal BGP and announces a default route for both address families while it is healthy, and OPNsense prefers the primary by local preference. When health degrades, the watchdog withdraws the routes through the agent's gRPC interface, and if the agent crashes the BGP session drops and OPNsense converges to the backup within the hold timer. All BGP parameters live in the `[bgp]` section of the MWAN config.

The watchdog acts on the combination of the two hosts' internet health.

| Primary internet | Failover LXC internet | Cause | Watchdog action |
| --- | --- | --- | --- |
| Healthy | Healthy | Normal | No action |
| Healthy | Down | Failover WAN issue | Alert only |
| Down | Healthy | Primary config or WAN down | Withdraw primary routes or force backup |
| Down | Down | Upstream outage | Alert only |
| Agent down | Healthy | Primary agent crash | BGP session drops and OPNsense converges |

`mwan watchdog failover` triggers the failover path immediately.

### Graceful restart

The agent restarts on every deploy, and graceful restart keeps that restart from flapping the route in OPNsense. The OPNsense side holds the agent's prefixes for a configured restart window and flushes them only if the session does not return, so a deploy causes no WAN outage. With graceful restart off, the agent restart drops the route for a moment and the WAN blips.

The agent enables graceful restart on its BGP speaker and mirrors it onto every peer, and it skips the pre-emptive route withdrawal on shutdown while graceful restart is on, because an explicit withdrawal would make OPNsense drop the route at once and defeat the purpose. The restart window and the toggle live in the `[bgp.graceful_restart]` config.

OPNsense carries its own graceful-restart toggle. A production operator flips it in the OPNsense GUI under Routing, then BGP, then General. The testbed has no GUI, so the operator drives the serial daemon to edit `config.xml` and reloads BGP. Confirm it with:

```bash
vtysh -c 'show running-config router bgp' | grep 'bgp graceful-restart'
```

Catching a real WAN link failure inside the restart window would need bidirectional forwarding detection, which is not wired, so the watchdog's withdraw path is what catches a fast failure.

## Watchdog rollback

The watchdog decides to roll back on whether the config recently changed, not on probes from inside the VM. A broken connection right after a config change points at the change, and a broken connection during a stable period points at something external.

Two signals mark a recent config change: a deploy timestamp the deploy playbook writes before it pushes new config, and a change in the composite config hash the agent reports.

| Connectivity fails | Recent deploy | Recent hash change | Stable before | Action |
| --- | --- | --- | --- | --- |
| Yes | Yes, within the grace period | any | any | Wait for the reboot |
| Yes | Yes, past the grace period | any | any | Time out, then roll back |
| Yes | No | Yes, within the window | Yes | Time out, then roll back |
| Yes | No | No | Yes | Test the failover LXC, then fail over or wait |
| No | any | any | any | Healthy, normal monitoring |

A detected deploy gets a short grace period before the connectivity timeout begins, so a reboot during a deploy is not mistaken for a failure. A hash-only change gets no grace period, because it should not cause a reboot. A hash change counts as recent only within a configured window, and an older one is treated as external.

### Snapshots

The watchdog rolls back to a snapshot, and two kinds exist with different owners. The deploy playbook owns the pre-deploy snapshot and takes one before it pushes config, so a rollback target always exists; without it, a fresh VM has none until the watchdog earns a known-good snapshot over many healthy cycles. The watchdog owns the known-good snapshot and takes one automatically once the system has been healthy and stable for a sustained period. It rolls back to the latest pre-deploy snapshot first and the most recent known-good one second, and it alerts without recovering when neither exists. It prunes old snapshots and keeps a small bounded number.

MWAN snapshots follow the no-saved-RAM rule the OPNsense operations page owns, because a snapshot that saves RAM resumes with stale clock and network state on rollback. Proxmox truncates a snapshot name past forty characters silently, so keep the name short and put the intent in the description.

## Data-plane convergence

Alongside the BGP speaker, a set of shell hooks and services on the MWAN VM converge the data plane, meaning the policy routes and the dynamic IPv6 translation table.

`update-routes.sh` programs the routing rules and the per-WAN tables, and runs from the network dispatcher's routable hooks, from a boot safety-net service, and from the health daemon on every WAN health transition. `update-npt.sh` programs the IPv6 translation rules in the runtime table, and runs from the same hook, a boot safety-net service, and again after a deploy reloads the firewall. The health daemon owns the WAN health state, and a health transition calls the route programmer so the system converges back to a recovered WAN on its own.

A Go successor to the route programmer lives in `mwan ifmgr` as the `wan_routes` module. It watches each WAN over netlink and reconciles the tables and rules on every default-route change and on a periodic tick, so it does not depend on the one-shot dispatcher hook and does not miss a late router-advertisement default route. Production keeps the shell authoritative and runs the module gated off, while the testbed has validated it in a shadow mode that logs intended operations without applying them and in a dual-write mode that coexists with the shell rules without thrash. The shell triggers stay until production cuts over.

The health daemon keeps two state files: a runtime file the route programmer and the status command read, and a persistent file that remembers the last-known WAN states across the daemon's own restarts. On start it seeds the runtime file from the persistent one, so a restart does not report every WAN as a fresh transition and does not false-alert. Lock files serialize the writers, so the dispatcher hooks, the safety-net services, and the health daemon never collide on the routing or firewall state.

Two failure modes are worth knowing. An empty IPv6 translation table means the runtime programming did not run or was flushed, and the fix is the boot safety-net service or running the translation programmer by hand for the affected WAN. Late boot ordering is expected, because the virtual devices and the AT&T authentication can come up after the dispatcher has already fired, so the boot safety-net services are mandatory.

Describe WAN state as healthy, unhealthy, or unknown, and never as up or down, which collides with the interface's administrative state.

## Email and alert routing

Every outbound email from MWAN code exits through one chokepoint, the `notify` package, which suppresses repeats of the same alert and paces how often an unresolved alert repeats. The mail provider's API key is injected into the process from a systemd environment file rather than written into the config, following the secret-handling rule the ansible secrets page owns. The planned unification of the remaining email surfaces is on the email page.

## Tracing

MWAN emits structured JSON logs to `/var/log/mwan-debug.log` when debug logging is on, and each line carries a trace id so events across the network stack, the route and translation programmers, and the health check correlate. A boot service and the deploy playbook both write the current trace id to a known file. Read it and the recent log with:

```bash
cat /run/mwan-trace-id
tail -n 200 /var/log/mwan-debug.log
```

## Operational quick reference

Check the running state on the MWAN VM:

```bash
ssh root@mwan.home.goodkind.io
wpa_cli status
systemctl status wpa_supplicant-mwan systemd-networkd networkd-dispatcher \
  nftables mwan-health cloudflared
mwan health-check --status
/usr/local/bin/mwan-debug
```

Check the IPv6 routing and translation state:

```bash
ip -6 route show table 100
ip -6 route show table 200
ip -6 rule show
nft -a list chain ip6 nat postrouting
nft -a list chain ip6 nat prerouting
```
