# AGENTS

This is the infrastructure configuration repository for `goodkind.io`. It contains Ansible
playbooks for LXC/VM provisioning, network device configs (Traefik, KEA DHCP, BIND), the
multi-WAN load balancer setup, and operational docs for the homelab.

The primary deployment target is a single Proxmox VE host named `vault` in San Francisco at  
`3d06:bad:b01::254`, running all LXC containers and QEMU VMs. A secondary Proxmox host
named `suburban` runs test and auxiliary workloads in NJ.

## Sources of Truth

- **Infrastructure state** (IPs, bridges, services, tunnels, open issues): `docs/INFRA.md`
- **Container/VM hostnames and IPv6 addresses**: `ansible/inventory/group_vars/all/service_mapping.yml`
- **Static inventory and host groups**: `ansible/inventory/hosts`
- **Dynamic Proxmox inventory**: `ansible/inventory/proxmox.yml`
- **Per-service variables**: `ansible/inventory/group_vars/<service>_servers.yml`
- **Shared variables**: `ansible/inventory/group_vars/all/vars.yml`
- **Secrets** (encrypted): `ansible/inventory/group_vars/all/vault.yml`
- **SSH access, network topology, Cloudflare config**: `docs/INFRA.md`

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
lives at `~/.config/ansible/vault.pass`. To inspect vault variable names without
exposing decrypted values, run `python3 scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass ansible/inventory/group_vars/all/vault.yml`
from the repo root.

Playbooks live in `ansible/playbooks/` and follow a `deploy-<service>.yml` naming
convention. See `.cursor/commands/deploy-playbook.md` for the exact invocation. Use
`--limit <hostname>` to target a single host and `--check --diff` for a dry run.

## Surgical Change Protocol

Production hosts (vault, mwan, OPNsense, berylax) serve live traffic for non-technical
users who cannot recover from outages. Physical access to hardware is unavailable for months
at a time. Treat every change as potentially irreversible.

**Before any change to a production host:**

1. **Understand the current state.** SSH in and read live config, routes, rules, logs.
   Do not trust docs/INFRA.md or Ansible templates as ground truth; they drift.
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
- `mwan opnsense version` probes the OPNsense daemon through a configured gRPC target.
- `mwan opnsense host serve` runs the Proxmox host-side Unix socket bridge to the OPNsense VM's `mwanrpc` chardev.

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
`docs/mwan-email-routing.md`.

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
gRPC withdraw path for fast WAN failure detection. `gobgp/v4@v4.5.0` has BFD
primitives available for that work.

## MWAN deployment pointers

Current MWAN host topology, live unit names, repository layout state,
manual rollout order, stale binary inventory, and cleanup notes live in
`docs/INFRA.md`.

Keep `AGENTS.md` focused on rules and operating guidance. Put current topology,
host-specific layout details, and point-in-time infrastructure state in
`docs/INFRA.md`.

## Operational gotchas

These rules are current MWAN and OPNsense safety guidance. Keep them current-state only.

### Never take testbed snapshots with `--vmstate 1`

`qm snapshot <vmid> <name> --vmstate 1` saves the VM's RAM along with the disk. Rollback then resumes from that saved RAM: stale wall clock, dead TCP sockets the peer has long since torn down, in-memory caches the rest of the network has forgotten. We observed a 13 hour clock skew after rollback, BGP sessions stuck in `SYN_SENT:CLOSED` while `nc -vz peer 179` succeeded against the same address, and Unbound returning SERVFAIL because its cached upstream answers were stale. Take testbed snapshots without `--vmstate` so rollback equals a clean reboot, which matches prod recovery semantics. Tracked as MWAN-182.

### After any snapshot rollback, restart the VM and re-verify

Even without `--vmstate`, treat rollback as a state transition that needs verification. Confirm OS version, hostname, time skew within 60 seconds, interface inventory, expected addresses on each interface, default routes for both IP families, BGP peer state from both sides, outbound IPv4 reachability with `ping 8.8.8.8`, DNS with `drill @127.0.0.1 pkg.opnsense.org`, daemon status, `vtysh` startup, and `mwan opnsense version -target <unix-socket>` returning a build banner before trusting the post-rollback state.

### virtio-serial wedges on large stdin payloads

`mwan opnsense file push` and `qm guest exec --pass-stdin` both choke on large stdin payloads around the size of a prod-shaped OPNsense `config.xml`. After the timeout the mwan-opnsense daemon can stop responding to gRPC and QGA can hang along with it. The canonical config push path is `scp` to `/conf/backup/<basename>.xml` followed by `POST /api/core/backup/revertBackup/<basename>` against the OPNsense REST API. Tracked as MWAN-155.

### OPNsense REST API has no upload-and-replace endpoint

There is no path under `/api/core/backup/*` that accepts an XML body and replaces `/conf/config.xml` in one call. `revertBackup/{basename}` restores from a file already in `/conf/backup/`. The full source-level reasoning lives at `docs/opnsense-25.7-config-import-flow.md`. Anyone proposing `POST /api/core/backup/restore` with a multipart body is reading a stale runbook.

### `<lock>1</lock>` short-circuits the boot interface mismatch check

`is_interface_mismatch($locked=true)` in `console.inc` walks `legacy_config_get_interfaces` and returns `false` as soon as it sees any interface with `<lock>` set. One locked interface skips the entire check, including unrelated locked entries that reference missing kernel devices. Boot proceeds without dropping to the interactive console; the failure surfaces later as service reconfigure errors. The behavior is intentional but easy to miss when debugging "why did boot proceed past a missing device?".

### Duplicate `<if>device</if>` declarations silently drop the loser

`interfaces_configure` builds `$hardware[$ifcfg['if']] = $if`, keyed by device name. Two interface entries on the same untagged device cause the second to overwrite the first in the map. Iteration order is alphabetical by `<descr>` via `strnatcmp` in `config.inc:340`. The losing interface stays in the GUI config but binds no address to any kernel interface. Caught us when prod's `opt6` (VMNET, `<if>vtnet0</if>`) and `opt9` (MANAGEMENT, also `<if>vtnet0</if>` via the `iavf0`-to-`vtnet0` device_names mapping) both claimed `vtnet0`; VMNET sorts later than MANAGEMENT and silently dropped the MANAGEMENT address. The testbed substitutions transform now strips `opt6`.

### `pkg upgrade -y` must run BEFORE `pkg install`

The OPNsense install ISO ships one snapshot of the package set. The mirror has moved on by the time you install anything. Running `pkg update -f` then jumping straight to `pkg install os-frr` pulls a libyang2 built against `pcre2-10.47` onto a system that still has `pcre2-10.45`. `vtysh` then fails at startup with `ld-elf.so.1: /usr/local/lib/libpcre2-8.so.0: version PCRE2_10.47 required by libyang2.so.2 not defined`. Insert `pkg upgrade -y` between `pkg update -f` and the first `pkg install`. The runbook at `docs/runbooks/opnsense-serial-vm-from-scratch.md` reflects this.

### Proxmox restricts `args` qemu-server field to literal `root@pam`

Setting the `args` field (used by Tofu's `kvm_arguments`) returns HTTP 500 "only root can set 'args' config" for any API token, regardless of `privsep` or assigned role. The check is hard-coded in Proxmox, not policy-driven. For VMs that need `args` (any VM with a virtio-serial chardev, including the mwan-opnsense VMs): `qm create` manually as root via SSH, then `tofu import` the resulting VM. The pattern is documented in `opentofu/imports.md`. Long-term cleanup is to drop `kvm_arguments` from Tofu entirely and manage `args` via Ansible or a manual `qm set` (MWAN-154).

### Config import strips API keys

`revertBackup` swaps the entire `/conf/config.xml`, which includes the `<apikeys>` block. The testbed substitutions transform produces an XML with no API keys at all, so the freshly-imported OPNsense has no API access until you mint one. After every import, mint a fresh root API key via the PHP `OPNsense\Auth\API->createKey('root')` helper and write the resulting key and secret into `ansible/inventory/group_vars/all/vault.yml`. Snippet lives at `docs/runbooks/opnsense-serial-vm-from-scratch.md`. Tracked as MWAN-159.

### Hot-adding a NIC needs `configctl interface reconfigure`

`qm set <vmid> --netN ...` adds the NIC at the hypervisor level. OPNsense's kernel sees the new `vtnetN` device, but the in-OPNsense interface config does not auto-bind to it. The new device comes up `IFDISABLED` until `configctl interface reconfigure <wan|opt...>` runs on the guest. Run reconfigure for whichever OPNsense interface is supposed to bind to the new device.

### Proxmox snapshot name cap is 40 characters

Names longer than 40 characters truncate silently. `prod-shaped-25-7-baseline-v3-bgp-up-2026-05-08` (41 chars) becomes garbage. Put the full intent in `--description` and keep the name short.

### Each `Execute` retry creates an orphan prepare snapshot

`mwan opnsense upgrade execute` calls `prepare`, which takes a fresh Proxmox snapshot. A rollback only restores the leaf snapshot. After repeated retries, prune stacked `pre-upgrade-*` snapshots with `mwan opnsense upgrade reset` before starting another execute cycle (MWAN-179, the upgrade reset cleanup ticket).

### `git -C /path` is mandatory

Always invoke git with `-C /path/to/repo` because shell cwd is unreliable across worktrees and subshells. A bare `git push` or `git commit` can land in the wrong repo. The agent-gate hook blocks raw `git` invocations.

### Never grep or pipe vault contents anywhere that reaches chat

`ansible-vault view` output is sensitive. Do not run it in a way that can reach the
conversation log, and do not pipe it through `grep`, `awk`, or similar filters. If you
only need variable names, use `python3 scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass ansible/inventory/group_vars/all/vault.yml`.
Use `ansible-vault edit` or `ansible-vault rekey` only for manual vault maintenance
outside the transcript.

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
port. Full procedure and prerequisites are in `docs/INFRA.md` under "Emergency out-of-band (OOB)
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
