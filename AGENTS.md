## AGENTS

This is the infrastructure configuration repository for `goodkind.io`. It contains Ansible
playbooks for LXC/VM provisioning, network device configs (Traefik, KEA DHCP, BIND), the
multi-WAN load balancer setup, and operational docs for the homelab.

The primary deployment target is a single Proxmox VE host named `vault` in San Francisco at  
`3d06:bad:b01::254`, running all LXC containers and QEMU VMs. A secondary Proxmox host
named `suburban` runs test and auxiliary workloads in NJ.

## Sources of Truth

- **Infrastructure state** (IPs, bridges, services, tunnels, open issues): `INFRA.md`
- **Container/VM hostnames and IPv6 addresses**: `ansible/inventory/group_vars/all/service_mapping.yml`
- **Static inventory and host groups**: `ansible/inventory/hosts`
- **Dynamic Proxmox inventory**: `ansible/inventory/proxmox.yml`
- **Per-service variables**: `ansible/inventory/group_vars/<service>_servers.yml`
- **Shared variables**: `ansible/inventory/group_vars/all/vars.yml`
- **Secrets** (encrypted): `ansible/inventory/group_vars/all/vault.yml`
- **SSH access, network topology, Cloudflare config**: `INFRA.md`

## Deployment Workflow

**New containers are provisioned by OpenTofu** (see `opentofu/`). Run `tofu apply` from
that directory first, then run the corresponding Ansible playbook to configure the
container. Existing containers (pre-OpenTofu) are still created by Ansible's
`create-ct.yml` until they are migrated. The Plane container (VMID 115) is the current
pilot; its `deploy-plane.yml` no longer imports `setup-service-ct.yml` because OpenTofu
owns provisioning.

OpenTofu state is stored in Consul at `opentofu/state`. Credentials go in
`opentofu/terraform.tfvars` (gitignored; see `terraform.tfvars.example`).

Ansible runs **locally** from `ansible/` on the controller (this machine). Vault password
lives at `~/.config/ansible/vault.pass`.

Playbooks live in `ansible/playbooks/` and follow a `deploy-<service>.yml` naming
convention. See `.cursor/commands/deploy-playbook.md` for the exact invocation. Use
`--limit <hostname>` to target a single host and `--check --diff` for a dry run.

## Surgical Change Protocol

Production hosts (vault, mwan, OPNsense, berylax) serve live traffic for non-technical
users who cannot recover from outages. Physical access to hardware is unavailable for months
at a time. Treat every change as potentially irreversible.

**Before any change to a production host:**

1. **Understand the current state.** SSH in and read live config, routes, rules, logs.
   Do not trust INFRA.md or Ansible templates as ground truth; they drift.
2. **Form a testable hypothesis.** State what you expect the change to do and what would
   prove it worked.
3. **Test surgically.** Apply the smallest possible change, verify with a specific command,
   then remove it. Example: add one ip6 rule, verify route lookup changed, run one ping,
   remove the rule.
4. **Verify no regression.** After confirming the fix, check that forwarded traffic, load
   balancing, and other paths still work before making anything permanent.
5. **Then codify.** Only after the live test passes, write the change into the Ansible
   template or script in the repo.
6. **Never bulk-change production.** No `ansible-playbook` runs against mwan without
   verifying each component independently first. No `systemctl restart` of networking
   services without a rollback plan.

**Things that have gone wrong before:**
- Watchdog emailing on every probe cycle because gRPC port was firewalled (port 50052
  missing from nftables input chain).
- PD-sourced traffic misrouting via wrong WAN because source-based ip6 rules were missing
  from update-routes.sh.
- IA_NA addresses having partial reachability (some destinations unreachable) which is
  normal and does not affect PD-based forwarding.

## Monolith Architecture

All Go infrastructure code lives in one binary built from `mwan/go/cmd/mwan/`. The
linux/amd64 build is `mwan` (renamed `mwan-linux` in `mwan/go/bin/` for the local
host); the freebsd/amd64 build is `mwan-opnsense` and runs only on OPNsense, where it
auto-dispatches into the `opnsense` daemon based on its argv[0].

Subcommands as defined in `cmd/mwan/main.go` (HEAD `4c754f4`):

- `mwan agent` runs the gRPC agent (vsock + TCP) inside the MWAN VM. Source: `internal/agent`.
- `mwan watchdog` runs the connectivity / rollback daemon. Source: `internal/watchdog`.
  `mwan watchdog failover` is the BGP-aware failover variant.
- `mwan ifmgr` runs the per-host interface manager. Role is read from
  `[ifmgr].role` in `/etc/mwan/config.toml`. Source: `internal/ifmgr`.
- `mwan health-check` is a one-shot probe. Source: `internal/healthcheck`.
- `mwan opnsense` is the FreeBSD config daemon (config.xml mutation over virtio
  serial). It is reached either via the explicit subcommand or by invoking the
  binary as `mwan-opnsense`. Source: `internal/opnsense*`.
- `mwan opnsense-host` runs on the Proxmox host as a unix-socket bridge that
  proxies gRPC to the OPNsense VM's mwanrpc chardev. Source: `cmd/mwan/opnsense_host*.go`.
- `mwan opnsense-probe` is a one-shot health probe against an `opnsense-host` socket.

There are NO separate Go binaries. New tools become subcommands of this monolith.
Shared code lives under `internal/config`, `internal/email`, `internal/logging`,
`internal/ops`, `internal/bgp`, `internal/alert`, `internal/tracing`, `internal/mwn1`,
`internal/rollback`. `internal/cmd/cutover` and `internal/cmd/cutover2` from earlier
versions of the binary have been removed; the remaining `mwan-cutover` and
`mwan-unfuck` files left on production hosts are stale wrappers from that era and
should be cleaned up.

### HA Failover: Embedded BGP (replacing keepalived/VRRP)

The agent embeds a GoBGP v4 speaker (`internal/bgp/`). Each MWAN host peers with
OPNsense via iBGP and announces a default route (0.0.0.0/0 and ::/0) when healthy.
OPNsense runs FRR (os-frr plugin) with route-maps to prefer the primary (local-pref).
The watchdog withdraws routes via gRPC when health degrades. If the agent crashes, the
BGP session drops and OPNsense converges to the backup within the hold timer.

This replaced keepalived/VRRP. No VIP, no VMAC, no macvlan, no DAD conflicts.
All BGP parameters (ASN, router ID, neighbors, timers, prefixes) are in `[bgp]`
section of config.toml.

## Email and alert routing

Forward-looking section. The target state described here lands across slices A through F
of MWAN-132. Until those slices merge, the live code still has three email surfaces and
the `internal/notify` package may not yet exist on every branch.

`internal/notify` is the single chokepoint for every outbound email. The contract: every
email exits through `notify.Notifier`, which owns per-(kind, key) state-change suppression
and per-kind repeat cadence. Direct calls to `email.Sender.Send` and the slog
`email_handler` path are removed by slice E.

Three sources currently funnel through (or migrate into) `notify.Manager`:

- ifmgr alerts (`internal/ifmgr/alerts.go`), one alert per (kind, key) state transition.
  Wg-peer-stalled, oobv6 SLAAC renumber, and similar per-interface conditions.
- watchdog failover (`internal/watchdog/failover.go`), one email at failover trigger, one
  at completion, one at recovery, all keyed and deduped.
- persistent-WARN downgrades (`watchdog.go`, `ops.go`, `agent/server.go`), routed at WARN
  level with explicit `Resolve` calls when the underlying condition clears.

`SMTP2GO_API_KEY` is injected via systemd `EnvironmentFile=/etc/mwan/secrets.env` rather
than templated into config.toml. That env-var injection contract is the standard tracked
under MWAN-131; slice F of MWAN-132 is the first instance.

For full routing details, kind catalog, and failure modes, see
`mwan/docs/mwan-email-routing.md` and the plan at
`mwan/docs/MWAN-132-email-unification-plan.md`.

## BGP graceful restart

BGP Graceful Restart (RFC 4724) lets a speaker restart its BGP process without
flapping its routes in the helper. The helper retains the restarter's prefixes for
`restart_time` seconds and only flushes them if the session does not come back. We
care about this because the agent restarts on every deploy. The 2026-05-07 deploy
measured a 1.7s WAN outage at agent restart with GR off, so GR is the path to
zero-flap deploys.

The wiring lives in `mwan/go/internal/bgp/speaker.go`. It is fed by the
`BGPGracefulRestart` config struct in `mwan/go/internal/bgp/config.go`, which mirrors
the loader struct of the same name in `mwan/go/internal/config/config.go`. Both were
introduced in slice 1 of MWAN-130 (commit `f0a4847`). When GR is enabled the speaker
attaches `GracefulRestart` to the GoBGP global config, sets `MpGracefulRestart` on
each AFI/SAFI, mirrors `GracefulRestart` onto every peer, and passes
`AllowGracefulRestart=true` on `Stop`. The agent shutdown path in
`mwan/go/internal/agent/main.go` skips the pre-emptive `WithdrawDefault` call when GR
is on. An explicit WITHDRAW would defeat GR because FRR would see it and drop the
route immediately, so pre-withdraw only runs when GR is off.

Configuration lives in the `[bgp.graceful_restart]` TOML block, added in slice 3 of
MWAN-130 under MWAN-146. Three knobs: `enabled` (bool, default `true`),
`restart_time` (uint32 seconds, default `30`, capped at `600` by the loader),
`notification_enabled` (bool, default `true`). The defaults are baked into
`config.BGPDefaults` so an empty `[bgp.graceful_restart]` block matches the documented
behaviour.

The OPNsense FRR side has its own toggle. The setting is
`OPNsense.quagga.bgp.graceful = '1'` in `/conf/config.xml`. For production the
operator flips it via the OPNsense GUI under Routing -> BGP -> General. The testbed
has no GUI access from this controller, so the operator drives an LLM session against
the `mwan-opnsense` gRPC API to mutate `config.xml` directly. After the toggle the
operator runs `configctl quagga reload bgp` (or the matching reconfigure call) to
apply it. To verify FRR has GR active, SSH to OPNsense and run
`vtysh -c 'show running-config router bgp' | grep 'bgp graceful-restart'`.

BFD is the natural follow-up. GR is only safe-by-default with BFD when a real WAN
link dies inside the GR window, because without BFD the helper holds stale routes
for the full `restart_time`. We do not have BFD wired today and rely on the watchdog
gRPC withdraw path for fast WAN failure detection. `gobgp/v4@v4.5.0` shipped BFD
primitives, so a future ticket can wire BFD into the speaker without an upstream
bump.

## MWAN deployment topology

Live state captured 2026-05-07 against current main (`4c754f4`). Mgmt addresses are
the values used in `[main].mwan_mgmt_addr` of each host's config.toml; reachability
notes describe how this controller reaches each host.

| Host                | OS               | Subcommand(s)                        | Unit file(s) on host                                                          | Repo source unit                                            | Config template                            | role                       | mwan_vmid | Mgmt addr                  |
|---------------------|------------------|--------------------------------------|-------------------------------------------------------------------------------|-------------------------------------------------------------|--------------------------------------------|----------------------------|-----------|----------------------------|
| vault (Proxmox SF)  | Linux/amd64      | `mwan ifmgr`, `mwan watchdog`        | `mwan-ifmgr.service`, `mwan-watchdog.service` (active); `mwan-oob.service` (disabled, stale) | `mwan/go/cmd/mwan/mwan-ifmgr.service` (ifmgr); watchdog unit lives only on host | `mwan/config/production.toml.j2`            | `vault-oob`                | 113       | `3d06:bad:b01::254` (host) |
| mwan VM 113         | Linux/amd64      | `mwan agent`                         | `mwan-agent.service` (active); `mwan-health.service` (legacy shell, active)    | `mwan/go/cmd/mwan/mwan-agent.service`                       | `mwan/config/production.toml.j2`            | (agent, no ifmgr role)     | 113       | `3d06:bad:b01::113`        |
| mwan-failover LXC 116 (on vault) | Linux/amd64 | `mwan agent`, `mwan ifmgr` | `mwan-agent.service`, `mwan-ifmgr.service`                                     | `mwan/go/cmd/mwan/mwan-agent.service`, `mwan/production/lxc-116/mwan-ifmgr.service` | `mwan/production/lxc-116/config.toml`     | `lxc-failover-backup`      | 116 (CT)  | reachable only via vault `pct exec 116` from this controller |
| OPNsense            | FreeBSD 14.3     | `mwan opnsense serve` (rc daemon)    | `/usr/local/etc/rc.d/mwan_opnsense` enabled via `/etc/rc.conf.d/mwan_opnsense` | `mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense`      | (no `/etc/mwan/`; settings in `rc.conf.d`) | n/a                        | n/a       | `agoodkind@3d06:bad:b01::1` (via vault ProxyJump) |
| suburban (Proxmox NJ testbed) | Linux/amd64 | `mwan ifmgr`, `mwan opnsense-host serve`, `mwan watchdog` | `mwan-ifmgr.service`, `mwan-opnsense-host.service` (with `upstream.conf` drop-in pointing at VM 101 chardev), `mwan-watchdog-testbed.service` | `mwan/go/cmd/mwan/mwan-ifmgr.service`, `mwan/go/cmd/mwan/mwan-opnsense-host.service`; watchdog-testbed unit lives only on host | `mwan/config/suburban-testbed.toml.j2`   | `suburban-wg`              | 950       | `suburban` SSH alias       |
| testbed VM 950      | Linux/amd64      | `mwan agent`                         | `mwan-agent.service`                                                          | `mwan/go/cmd/mwan/mwan-agent.service`                       | `mwan/testbed/vm-950/config.toml`           | (agent, no ifmgr role)     | 950       | `3d06:bad:b01:200::950` (via suburban ProxyJump) |
| testbed LXC 100 (on suburban) | Linux/amd64 | `mwan agent`, `mwan ifmgr`     | `mwan-agent.service`, `mwan-ifmgr.service`                                    | `mwan/go/cmd/mwan/mwan-agent.service`, `mwan/testbed/lxc-100/mwan-ifmgr.service` | `mwan/testbed/lxc-100/config.toml`        | `lxc-failover-backup`      | 100 (CT)  | reachable only via suburban `pct exec 100` |
| testbed LXCs 200/201/202/203 | Linux/amd64 | none (ISP simulators + proxy)     | none                                                                          | n/a                                                         | n/a                                        | n/a                        | n/a       | reachable only via suburban `pct exec` |
| tack LXC 117        | Linux/amd64      | none                                 | none                                                                          | n/a                                                         | n/a                                        | n/a                        | n/a       | `tack` SSH alias            |

### Repo drift to clean up

- `mwan/services/mwan-health.service` ships in the repo but the live `mwan-health.service` on VM 113 is the legacy shell health check; it is not derived from the Go binary and should either be retired or rewritten to call `mwan health-check`.
- `mwan/services/mwan-trace-boot.service`, `mwan-update-att-pinned-dests.service`, `mwan-update-npt.service`, `mwan-update-routes.service` all run shell scripts under `/usr/local/bin/`; they predate the monolith and are out of scope for the binary rollout but are worth flagging for a separate cleanup sweep.
- `mwan-cutover` and `mwan-cutover2` subcommands no longer exist in the binary on suburban/VM 950 (HEAD), but the older binaries still installed on vault, VM 113, LXC 116, and the testbed LXC 100 still advertise them in their usage line. That is the proof those hosts have not been refreshed.

### Stale binaries to clean up

The same `/usr/local/bin/mwan-*` artefacts left over from the cutover era:

- vault: `mwan-cutover` (Apr 9), `mwan-unfuck` (Apr 9). Surgical cleanup only.
- mwan VM 113: `mwan-agent` (Mar 28), `mwan-change-detect` (Mar 28). Surgical cleanup.
- LXC 116: clean (only `/usr/local/bin/mwan` plus the active service files).
- suburban: `mwan-cutover` (Apr 9), `mwan-watchdog` (Mar 28), `mwan-watchdog-test` (Mar 28), `mwan-unfuck` (Apr 8), `mwan-opnsense-host` (May 5, superseded by current `mwan` binary). Suburban is the testbed and may be reprovisioned freely (`bomb and redo` per the testbed rule), so cleanup here can be aggressive.
- VM 950: clean.
- testbed LXC 100: clean.
- OPNsense: two timestamped backup copies of `mwan-opnsense.pre-*` left in `/usr/local/sbin/` from previous self-deploy runs, plus a backup `mwan_opnsense.pre-*` rc.d script. The self-deploy preflight in `mwan_opnsense` handles its own `.previous` rollback file, so the `pre-*` artefacts are leftovers and can be removed.

## Manual rollout of a new mwan binary

The new artefacts live at `mwan/go/bin/mwan-linux` (linux/amd64) and
`mwan/go/bin/mwan-opnsense` (freebsd/amd64). Local `main` is at `4c754f4`.

Order: testbed first (suburban host, then VM 950 and LXC 100, then OPNsense
testbed), verify healthy, then production (LXC 116 first as the backup, then
mwan VM 113, then vault, then production OPNsense). Always copy `mwan` aside as
`mwan.prev` before swap so `cp -a /usr/local/bin/mwan.prev /usr/local/bin/mwan`
is the rollback.

### Testbed: suburban (NJ Proxmox host)

```bash
scp mwan/go/bin/mwan-linux suburban:/tmp/mwan.new
ssh suburban 'cp -a /usr/local/bin/mwan /usr/local/bin/mwan.prev \
  && install -m0755 /tmp/mwan.new /usr/local/bin/mwan \
  && systemctl restart mwan-ifmgr mwan-opnsense-host mwan-watchdog-testbed \
  && systemctl --no-pager status mwan-ifmgr mwan-opnsense-host mwan-watchdog-testbed'
```

Health: `journalctl -u mwan-ifmgr -u mwan-opnsense-host -u mwan-watchdog-testbed -n 50 --no-pager`
on suburban; `mwan opnsense-probe` against the listen socket if reachable.

If the testbed wedges, the user said "bomb it and redo": rebuild VM 950 and LXC
100 from `mwan/testbed/vm-950/` and `mwan/testbed/lxc-100/` via the existing
`mwan/testbed/deploy.sh` and `mwan/testbed/isp-lxc-setup.sh` scripts, then run
the relevant Ansible playbooks afresh. Production is not allowed to be reset
this way.

### Testbed: VM 950 (mwan agent VM)

```bash
scp mwan/go/bin/mwan-linux root@3d06:bad:b01:200::950:/tmp/mwan.new \
  -o ProxyJump=suburban
ssh -J suburban root@3d06:bad:b01:200::950 \
  'cp -a /usr/local/bin/mwan /usr/local/bin/mwan.prev \
   && install -m0755 /tmp/mwan.new /usr/local/bin/mwan \
   && systemctl restart mwan-agent \
   && systemctl --no-pager status mwan-agent'
```

Health: `mwan opnsense-probe -upstream unix:///var/run/mwan-opnsense.sock` from
suburban, or `mwan health-check` on the VM directly.

### Testbed: LXC 100

```bash
scp mwan/go/bin/mwan-linux suburban:/tmp/mwan.new
ssh suburban 'pct push 100 /tmp/mwan.new /usr/local/bin/mwan.new \
  && pct exec 100 -- bash -c "cp -a /usr/local/bin/mwan /usr/local/bin/mwan.prev \
       && install -m0755 /usr/local/bin/mwan.new /usr/local/bin/mwan \
       && systemctl restart mwan-agent mwan-ifmgr \
       && systemctl --no-pager status mwan-agent mwan-ifmgr"'
```

### Production: LXC 116 (do this first; it is the backup speaker)

```bash
scp mwan/go/bin/mwan-linux vault:/tmp/mwan.new
ssh vault 'pct push 116 /tmp/mwan.new /usr/local/bin/mwan.new \
  && pct exec 116 -- bash -c "cp -a /usr/local/bin/mwan /usr/local/bin/mwan.prev \
       && install -m0755 /usr/local/bin/mwan.new /usr/local/bin/mwan \
       && systemctl restart mwan-agent mwan-ifmgr \
       && systemctl --no-pager status mwan-agent mwan-ifmgr"'
```

Health: from vault, `pct exec 116 -- journalctl -u mwan-agent -u mwan-ifmgr -n 50 --no-pager`,
plus a BGP-session check on OPNsense (`vtysh -c 'show bgp ipv6 summary'`).

### Production: mwan VM 113

```bash
scp mwan/go/bin/mwan-linux root@3d06:bad:b01::113:/tmp/mwan.new
ssh root@3d06:bad:b01::113 \
  'cp -a /usr/local/bin/mwan /usr/local/bin/mwan.prev \
   && install -m0755 /tmp/mwan.new /usr/local/bin/mwan \
   && systemctl restart mwan-agent \
   && systemctl --no-pager status mwan-agent'
```

Health: `journalctl -u mwan-agent -n 100 --no-pager` on the VM, plus the BGP
session check on OPNsense.

### Production: vault (Proxmox host)

vault runs both `mwan ifmgr` and `mwan watchdog`. Restart ifmgr first; only restart
the watchdog when ifmgr is healthy, because the watchdog will react to ifmgr churn.

```bash
scp mwan/go/bin/mwan-linux vault:/tmp/mwan.new
ssh vault 'cp -a /usr/local/bin/mwan /usr/local/bin/mwan.prev \
  && install -m0755 /tmp/mwan.new /usr/local/bin/mwan \
  && systemctl restart mwan-ifmgr \
  && sleep 5 && systemctl --no-pager status mwan-ifmgr \
  && systemctl restart mwan-watchdog \
  && systemctl --no-pager status mwan-watchdog'
```

Health: `journalctl -u mwan-ifmgr -u mwan-watchdog -n 100 --no-pager` on vault.

### Production: OPNsense (FreeBSD)

The freebsd build at `mwan/go/bin/mwan-opnsense` lives at
`/usr/local/sbin/mwan-opnsense` on the router. The rc.d daemon is `mwan_opnsense`.
The daemon already implements its own `.previous` revert path inside
`/usr/local/etc/rc.d/mwan_opnsense` (preflight runs `cp -f mwan-opnsense.previous
mwan-opnsense.current` if a pending-verify marker is present and health was not
reported ok), so use that contract: keep a hand-made `.prev` as a belt-and-braces
copy and let the rc.d preflight handle the structured revert.

```bash
scp -J vault mwan/go/bin/mwan-opnsense agoodkind@3d06:bad:b01::1:/tmp/mwan-opnsense.new
ssh -J vault agoodkind@3d06:bad:b01::1 'sudo sh -c "
  cp -a /usr/local/sbin/mwan-opnsense /usr/local/sbin/mwan-opnsense.prev \
  && install -m0755 /tmp/mwan-opnsense.new /usr/local/sbin/mwan-opnsense \
  && service mwan_opnsense restart \
  && service mwan_opnsense status"'
```

Health: `service mwan_opnsense status` (it returns the daemon's own JSON status
including `health`), plus `tail /var/log/mwan-opnsense.log` and
`mwan opnsense-probe` from suburban or vault against the host-side socket.



## Operational gotchas

These rules are load-bearing. Each one was learned the hard way during the OPNsense 26.x upgrade rehearsal arc on 2026-05-07 through 2026-05-10. Skipping any of them reintroduces a class of failure that already cost hours to diagnose.

### Never take testbed snapshots with `--vmstate 1`

`qm snapshot <vmid> <name> --vmstate 1` saves the VM's RAM along with the disk. Rollback then resumes from that saved RAM: stale wall clock, dead TCP sockets the peer has long since torn down, in-memory caches the rest of the network has forgotten. We observed a 13 hour clock skew after rollback, BGP sessions stuck in `SYN_SENT:CLOSED` while `nc -vz peer 179` succeeded against the same address, and Unbound returning SERVFAIL because its cached upstream answers were stale. Take testbed snapshots without `--vmstate` so rollback equals a clean reboot, which matches prod recovery semantics. Tracked as MWAN-182.

### After any snapshot rollback, restart the VM and re-verify

Even without `--vmstate`, treat rollback as a state transition that needs verification. Walk Section A of the rehearsal runbook at `mwan/docs/runbooks/opnsense-upgrade-rehearsal-vm102.md` before trusting the post-rollback state: OS version, hostname, time skew within 60 s, interface inventory, expected addresses on each interface, default routes both families, BGP peer state from both sides, `ping 8.8.8.8`, `drill @127.0.0.1 pkg.opnsense.org` answer under 2 s, daemons running, `vtysh` loads without dynamic linker errors, `mwan opnsense-probe` responds in under 5 s.

### virtio-serial wedges on large stdin payloads

`mwan opnsense-probe --stdin-file` and `qm guest exec --pass-stdin` both choke on payloads around 219 KB, which is what a prod-shaped OPNsense `config.xml` weighs. After the timeout the mwan-opnsense daemon stops responding to gRPC and QGA can hang along with it. The canonical config push path is `scp` to `/conf/backup/<basename>.xml` followed by `POST /api/core/backup/revertBackup/<basename>` against the OPNsense REST API. Tracked as MWAN-155.

### OPNsense REST API has no upload-and-replace endpoint

There is no path under `/api/core/backup/*` that accepts an XML body and replaces `/conf/config.xml` in one call. `revertBackup/{basename}` restores from a file already in `/conf/backup/`. The full source-level reasoning lives at `mwan/docs/opnsense-25.7-config-import-flow.md`. Anyone proposing `POST /api/core/backup/restore` with a multipart body is reading a stale runbook.

### `<lock>1</lock>` short-circuits the boot interface mismatch check

`is_interface_mismatch($locked=true)` in `console.inc` walks `legacy_config_get_interfaces` and returns `false` as soon as it sees any interface with `<lock>` set. One locked interface skips the entire check, including unrelated locked entries that reference missing kernel devices. Boot proceeds without dropping to the interactive console; the failure surfaces later as service reconfigure errors. The behavior is intentional but easy to miss when debugging "why did boot proceed past a missing device?".

### Duplicate `<if>device</if>` declarations silently drop the loser

`interfaces_configure` builds `$hardware[$ifcfg['if']] = $if`, keyed by device name. Two interface entries on the same untagged device cause the second to overwrite the first in the map. Iteration order is alphabetical by `<descr>` via `strnatcmp` in `config.inc:340`. The losing interface stays in the GUI config but binds no address to any kernel interface. Caught us when prod's `opt6` (VMNET, `<if>vtnet0</if>`) and `opt9` (MANAGEMENT, also `<if>vtnet0</if>` via the `iavf0`-to-`vtnet0` device_names mapping) both claimed `vtnet0`; VMNET sorts later than MANAGEMENT and silently dropped the MANAGEMENT address. The testbed substitutions transform now strips `opt6`.

### `pkg upgrade -y` must run BEFORE `pkg install`

The OPNsense install ISO ships one snapshot of the package set. The mirror has moved on by the time you install anything. Running `pkg update -f` then jumping straight to `pkg install os-frr` pulls a libyang2 built against `pcre2-10.47` onto a system that still has `pcre2-10.45`. `vtysh` then fails at startup with `ld-elf.so.1: /usr/local/lib/libpcre2-8.so.0: version PCRE2_10.47 required by libyang2.so.2 not defined`. Insert `pkg upgrade -y` between `pkg update -f` and the first `pkg install`. The runbook at `mwan/docs/runbooks/opnsense-serial-vm-from-scratch.md` reflects this.

### Proxmox restricts `args` qemu-server field to literal `root@pam`

Setting the `args` field (used by Tofu's `kvm_arguments`) returns HTTP 500 "only root can set 'args' config" for any API token, regardless of `privsep` or assigned role. The check is hard-coded in Proxmox, not policy-driven. For VMs that need `args` (any VM with a virtio-serial chardev, including the mwan-opnsense VMs): `qm create` manually as root via SSH, then `tofu import` the resulting VM. The pattern is documented in `opentofu/imports.md`. Long-term cleanup is to drop `kvm_arguments` from Tofu entirely and manage `args` via Ansible or a manual `qm set` (MWAN-154).

### Config import strips API keys

`revertBackup` swaps the entire `/conf/config.xml`, which includes the `<apikeys>` block. The testbed substitutions transform produces an XML with no API keys at all, so the freshly-imported OPNsense has no API access until you mint one. After every import, mint a fresh root API key via the PHP `OPNsense\Auth\API->createKey('root')` helper and write the resulting key and secret into `ansible/inventory/group_vars/all/vault.yml`. Snippet lives at `mwan/docs/runbooks/opnsense-serial-vm-from-scratch.md`. Tracked as MWAN-159.

### Hot-adding a NIC needs `configctl interface reconfigure`

`qm set <vmid> --netN ...` adds the NIC at the hypervisor level. OPNsense's kernel sees the new `vtnetN` device, but the in-OPNsense interface config does not auto-bind to it. The new device comes up `IFDISABLED` until `configctl interface reconfigure <wan|opt...>` runs on the guest. Run reconfigure for whichever OPNsense interface is supposed to bind to the new device.

### Proxmox snapshot name cap is 40 characters

Names longer than 40 characters truncate silently. `prod-shaped-25-7-baseline-v3-bgp-up-2026-05-08` (41 chars) becomes garbage. Put the full intent in `--description` and keep the name short.

### Each `Execute` retry creates an orphan prepare snapshot

`mwan opnsense-upgrade execute` calls `prepare` which takes a fresh Proxmox snapshot. A rollback only restores the leaf snapshot. After three retries the snapshot tree carries three stacked `pre-upgrade-*` snapshots that have to be cleaned by hand. `mwan opnsense-upgrade reset` will walk and prune the chain when it lands (MWAN-179).

### `git -C /path` is mandatory

Always invoke git with `-C /path/to/repo` because shell cwd is unreliable across worktrees and subshells. A bare `git push` or `git commit` can land in the wrong repo. The agent-gate hook blocks raw `git` invocations.

### Never grep or pipe vault contents anywhere that reaches chat

`ansible-vault view` output is sensitive. Do not pipe it through `grep`, `awk`, or anything whose stdout reaches the conversation log. Use mode-600 tmpfiles plus `shred -u` cleanup. Earlier in this session a leak via `ansible-vault view | grep` burned nine secret values that the operator explicitly chose not to rotate.

### When the runbook says STOP, stop

Capture forensics, do not improvise, do not retry, surface to the operator. The most expensive failures in this arc came from patching forward through ambiguous state instead of resetting and restarting from a known-good baseline.

## Build rules for implementation agents

Every implementation agent, whether dispatched as a subagent or running inline, must apply these:

- **Start from evidence.** Read the relevant source before changing code. Read this file and any local design doc the change touches. Do not assume architecture from names alone.
- **Respect the boundary.** Generic layers stay generic. Provider-specific or platform-specific behavior lives behind the provider boundary. Preserve exact user-visible values unless an external boundary requires escaping or translation.
- **Implement the real behavior.** Wire features into the real runtime path, not only into tests or fallback code. Prefer one source of truth over compatibility crutches. Reconcile related state immediately when the user-facing contract says values should stay in sync. Avoid deferred cleanup.
- **Avoid shortcuts.** No baseline edits to hide lint findings. No `//nolint` without explicit operator authorization. No synthetic references, dummy logs, or marker-method calls to satisfy reachability tools. No no-op closers or empty lifecycle methods. No compile-only or log-only tests presented as behavioral coverage.
- **Keep types tight.** Avoid `any`, `interface{}`, and loose maps unless required at a real external boundary. Convert untyped input to concrete types as early as possible.
- **Write useful tests.** Test the real contract. Add regression coverage for the failure mode that motivated the change. Avoid tests that only prove compilation, only log output, or assert implementation trivia.
- **Preserve project hygiene.** Keep edits inside scope. Do not revert unrelated work. Update comments and docs when they would otherwise describe the old contract.
- **Verify before reporting.** Run the project's real gates: `make check`, `make test`, `make build-linux`, `make build-mwan-opnsense`. State exactly what was run and whether it passed. If a gate could not be run, state why.
- **Report honestly.** State what changed. State the verification commands. State residual risks. Do not claim files, symbols, commits, or behavior that was not verified. Every factual claim must trace to a command run in this session with the output cited verbatim. No "likely", "probably", or "should" without a verifying command.

## Prose rule

Prose reads cleanly as a linear record of the thing itself. Each sentence is a full sentence with a concrete subject, a concrete verb, and enough context to sound natural when spoken aloud. Each new sentence adds useful information in the same direction as the sentence before it, with low cognitive load and no hidden context the reader must reconstruct. Paragraphs move forward by accumulation, with no setup, interruption, reversal, or correction.

## Go Code Standards

These rules apply to all Go code in `mwan/go/`. Violations block merge.

- **Single TOML config.** All subcommands read `/etc/mwan/config.toml`. No env-var-based
  config loading. Env vars override secrets only (`SMTP2GO_API_KEY`, `PVE_TOKEN_SECRET`).
- **No globals.** Config is passed explicitly through function arguments. No package-level
  `var` for config, state, or singletons.
- **DRY.** No duplicated structs, no bridge/adapter types that mirror another struct
  field-by-field. If two things need the same data, they share one type.
- **Small files.** No file over 500 lines. If a file exceeds this, split by responsibility.
- **Separated concerns.** Config loading, business logic, I/O, and CLI parsing live in
  separate files. No function that parses flags AND runs business logic.
- **One email sender.** One `EmailSender` type, parameterized at construction. No
  per-subcommand email implementations.
- **One logger factory.** One `newLogger()` function parameterized by subcommand name, log
  paths, and optional email handler. No per-subcommand logger setup files.
- **No hardcoded values.** IPs, paths, timeouts, email addresses, hostnames come from TOML
  config. Validation errors loudly if a required field is missing.
- **Comments explain WHY, not WHAT.** Do not add comments that restate the code. Do not add
  `// Foo does X` when the function name already says X.
- **Secrets in Ansible Vault.** TOML templates use `{{ mwan_smtp2go_api_key }}` Jinja2
  variables. Never commit plaintext secrets. The `.j2` suffix signals a template.
- **Linting enforced.** `make lint` (golangci-lint) must pass. Config in `mwan/go/.golangci.yml`.
- **Cutover is complete.** The `mwan cutover` and `mwan cutover2` subcommands have
  been removed from the binary. Ongoing failover is handled by `mwan watchdog failover`.

## Rules for Changes

1. Before editing any playbook or template, check the Ansible quality rules in
  `.cursor/rules/ansible-quality.mdc`. It documents common pitfalls around single-bracket
   tests, `set_fact` concurrency, folded block scalars in URLs, and guard clause patterns.
2. Shell scripts in `mwan/scripts/` must use `[[ ]]` for tests, full `if/then/fi` blocks
  with no inline ternaries, and pass `shellcheck --severity=error`. The full style
   requirements are in `.cursor/rules/mwan.mdc`.
3. Secrets go in `ansible/inventory/group_vars/all/vault.yml` (Ansible Vault encrypted).
  Never commit plaintext secrets anywhere in the repo. For new services provisioned via
   OpenTofu, per-service generated secrets (db passwords, secret keys) may use Ansible's
   `lookup('password', ...)` plugin, which caches values in `<service>/.secrets/`
   (gitignored) on the Ansible controller.
4. IPv6 is P0. The diagnosis workflow is in `.cursor/rules/ipv6-dhcp-diagnosis.mdc`.
5. The `kea/` Rakefile is the live mechanism for pushing DHCP config to the router.
  Do not modify KEA config files without understanding the Rake deploy step first.

## Emergency OOB access

When vault's network is down (MWAN VM stopped, routing broken), SSH to vault is unavailable.
The fallback is a USB-serial cable from berylax (`/dev/ttyUSB0`) to vault's physical serial
port. Full procedure and prerequisites are in `INFRA.md` under "Emergency out-of-band (OOB)
access".

**Preferred tool: `serial-exec`** ([github.com/agoodkind/serial-exec](https://github.com/agoodkind/serial-exec)).
Rust CLI that runs on berylax (static arm64 musl binary, no dependencies). Uses a
sentinel-based protocol for reliable output capture and exit code extraction over serial.

```bash
ssh berylax '/tmp/serial-exec run vault "qm list" --json'
ssh berylax '/tmp/serial-exec shell vault'
ssh berylax '/tmp/serial-exec ping vault'
```

Config on berylax: `~/.config/serial-exec/hosts.toml`

```toml
[hosts.vault]
device = "/dev/ttyUSB0"
baud = 115200
prompt = '(?m)[#$] $'
user = "root"
```

If `serial-exec` is unavailable, fall back to `screen /dev/ttyUSB0 115200` on berylax.

---
