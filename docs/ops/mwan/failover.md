# MWAN failover and rollback

Production failover is BGP-based. The agent embeds GoBGP, peers with OPNsense
FRR over iBGP, and announces a default route (`0.0.0.0/0`, `::/0`) when
healthy. OPNsense runs FRR (`os-frr`) with a route-map that prefers the primary
via higher local-pref. The watchdog withdraws routes via the agent's gRPC API
when health degrades; if the agent crashes, the BGP session drops and OPNsense
converges to the backup within the hold timer.

The `[bgp]` config section carries every BGP parameter: ASN, router ID,
neighbors, timers, and prefixes.

Failover decision matrix:

| Primary Internet | Failover LXC Internet | Cause                      | Watchdog action                          |
| ---------------- | --------------------- | -------------------------- | ---------------------------------------- |
| OK               | OK                    | Normal                     | No action                                |
| OK               | DOWN                  | Failover WAN issue         | Alert only                               |
| DOWN             | OK                    | Primary config or WAN down | Withdraw primary routes or force backup  |
| DOWN             | DOWN                  | Upstream outage            | Alert only                               |
| Agent down       | OK                    | Primary agent crash        | BGP session drops; OPNsense converges    |

`mwan watchdog failover` triggers the BGP failover path immediately. The
current failover path uses BGP route control.

## BGP graceful restart

BGP Graceful Restart (RFC 4724) lets the agent restart its BGP process without
flapping its routes in the helper. The helper retains the restarter's prefixes
for `restart_time` seconds and only flushes them if the session does not come
back. With GR off, an agent restart drops the WAN route until the session
returns. Every deploy reboots the gateway. Production and the testbed set GR
off, so OPNsense selects the backup peer during that reboot.

When GR is enabled the speaker negotiates the capability globally and per
peer, and allows graceful restart on stop. The agent shutdown path skips the
pre-emptive default-route withdraw when GR is on. An explicit WITHDRAW would
defeat GR: FRR would drop the route immediately. Pre-withdraw only runs when
GR is off.

The `[bgp.graceful_restart]` config section carries the settings, and the
loader bakes in the defaults so an empty block matches documented behaviour.

The OPNsense FRR side has its own toggle,
`OPNsense.quagga.bgp.graceful = '1'` in the router config. Production
operators flip it via the OPNsense GUI under Routing -> BGP -> General. The
testbed has no GUI from the controller, so the operator drives the
`mwan-opnsense` gRPC API to mutate the router config directly, then runs
`configctl quagga reload bgp`. Verify with:

```bash
vtysh -c 'show running-config router bgp' | grep 'bgp graceful-restart'
```

BFD is the natural follow-up. GR is only safe-by-default with BFD when a real
WAN link dies inside the GR window. Without BFD the helper holds stale
routes for the full `restart_time`. OPNsense carries a BFD stanza toward the
neighbor, but the mwan agent's embedded speaker does not participate yet, so no
BFD session forms; fast WAN failure detection relies on the watchdog gRPC
withdraw path.

## Deploy operation

The MWAN VM deploy changes the running gateway only at a reboot. A deployment
operation on the Proxmox host restores the gateway when that reboot breaks
connectivity.

The deploy validates the rendered network document and firewall with the
released binary on the Proxmox host before it creates a snapshot. The deploy
then creates the `pre-deploy-<trace id>` snapshot and runs
`mwan deploy-gate arm`. The arm command verifies the gateway's installed
files, the snapshot, and the inbound and downstream application checks from
the configured observers. The arm command then writes the operation record
and starts a watch unit on the Proxmox host. The watch repeats the application
checks until the operation status is committed or recovered.

The deploy writes the release and the gateway configuration after the arm
command succeeds. The running daemon reads none of those files. The deploy
then reboots the gateway, and the reboot starts every unit from the written
files. After the gateway reports a new boot ID, `mwan deploy-gate commit`
verifies the written files and the application checks and sets the operation
status to committed.

A failure after the arm command succeeds runs `mwan deploy-gate recover`. The
recover command stops the gateway, restores the snapshot, starts the gateway,
and waits for the previous files and the application checks. The watch runs
the same recovery when the application checks fail repeatedly or the
operation deadline passes. The watchdog runs the same recovery for an
unresolved record with no running watch.

A failure before the arm command succeeds runs no recovery, because the
gateway has no change. The deploy removes the snapshot when the Proxmox host
has no record for the operation or a record with status disarmed. The deploy
does not remove the snapshot of a record with any other status, because the
watchdog restores that snapshot for the record. The next arm command succeeds
only after the record status is recovered, committed, or disarmed.

A bootstrap deploy creates no snapshot and arms no operation. A check run
creates no snapshot, arms no operation, and ends before the reboot.

## Watchdog rollback design

The watchdog runs on the Proxmox host. It bases the rollback decision on
**whether config recently changed**, not on per-interface probes from inside
the VM. If config changed and connectivity then broke, config is the most
probable cause. If config has been stable and connectivity breaks, it is
probably external.

Two signals count as a recent config change:

1. **Deploy timestamp** (`/var/lib/mwan/last-deploy`), written by the deploy
   playbook before it writes the release to the gateway.
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
- Hash-only changes get no grace period. They should not cause reboots.

Hash-change recency window: a hash change is "recent" for
`DEPLOY_WINDOW_MINUTES` (default 30). Anything older is treated as external.

### Snapshots

Two snapshot types with different owners:

- **`pre-deploy-*`** snapshots are owned by the deploy playbook. The playbook
  creates `pre-deploy-<trace id>` before it arms the deployment operation.
  Without it, a fresh or recently changed VM may have no rollback
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

Pruning keeps at most `MAX_KNOWN_GOOD_SNAPSHOTS` (default 2) and
`MAX_TOTAL_SNAPSHOTS` (default 15), deleting oldest first. The total counts
`pre-deploy-*` snapshots, and the watchdog deletes only `known-good-*`
snapshots to meet it.

No deploy task and no watchdog pass deletes the `pre-deploy-*` snapshot of a
committed deploy. A recovery deletes the `pre-deploy-*` and `known-good-*`
snapshots taken after the snapshot it restores. The deploy removes its own
snapshot only when the arm command fails before the Proxmox host has a record
for the operation. Committed deploys therefore accumulate `pre-deploy-*`
snapshots until an operator deletes them.

Proxmox snapshot names are capped at 40 characters and longer names truncate
silently. Put the full intent in `--description` and keep the name short. Do
not save RAM in a snapshot. Rollback then resumes with stale networking and
clock state.
