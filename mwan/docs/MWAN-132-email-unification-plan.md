# Plan: Unify MWAN email surfaces and bring testbed to prod parity

## Context

The long-term goal is the OPNsense 26.x upgrade on prod (parent MWAN-13). The path to get there is to upgrade testbed first, hit the real issues there, mediate them, and only then upgrade prod. Testbed exists for exactly this: constant prodding and nuking. That path only works if testbed config is the closest possible match to prod, so the issues we surface on testbed are the issues prod will hit.

Testbed-to-prod parity has multiple axes. The OPNsense `config.xml` shape is the largest one and is tracked under MWAN-117, MWAN-118, MWAN-119, MWAN-127. Email and alerting behavior is another axis, and it has been silently drifting. This plan closes that gap.

Email is currently three surfaces in the code. The post-MWAN-121..125 deploy made ifmgr alerts state-change driven, but two other surfaces still bypass that path. The current testbed flurry (vsock-unavailable, ops-transport-failed, read-deploy-file) confirms it. Those three repeat every 5 minutes because they go through the raw slog and email_handler cooldown path, not through AlertManager.

Prod and testbed configs have drifted on `min_level`, `bind_iface`, `from`. The failover LXC configs (lxc-116 and lxc-100) have no `[email]` block at all and emit only to journald. None of that survives a 26.x upgrade test cleanly because alert noise on testbed obscures the actual upgrade signals.

Outcome:

1. Every email exits through one boundary (`internal/notify`) with consistent state-change plus repeat-cadence semantics.
2. Watchdog failover and recovery emails get the same dedup as ifmgr alerts.
3. Three persistent-WARN paths get downgraded to Info/Debug, eliminating the testbed flurry at the source.
4. The oobv6 double-emit (raw `log.Warn` plus `Alerts.Notify` for the same event) is fixed.
5. Failover LXC configs gain `[email]` blocks. Testbed gains the missing `[ifmgr]` section. Testbed `min_level` matches prod (ERROR).
6. Ansible templates and group_vars are kept in lockstep so the playbook stays reproducible. No `ansible-playbook` run in this plan.

## Scope

In: `mwan/go/internal/notify/` (new), `internal/ifmgr/alerts.go`, `internal/watchdog/failover.go` and friends, three persistent-WARN sites, `internal/ifmgr/modules/oobv6/oobv6.go:192`, all four config locations, related Ansible templates and group_vars, AGENTS.md.

Out: BGP/route logic, OPNsense FRR config, BGP graceful restart (MWAN-130), OPNsense 26.x config import (MWAN-117/118/119/127), `internal/logging` extraction (MWAN-56/57), converting lxc-116/lxc-100 configs to `.j2` (MWAN-22). The j2 conversion is replaced by an env-var path for the smtp2go API key (see slice F).

## Audit findings

### Email surfaces in mwan/go (3 paths today)

1. slog handler at `mwan/go/internal/logging/email_handler.go`. Threshold by `cfg.Email.MinLevel`. Cooldown by `r.Message` string (5m default). Body: `BuildEmailBody` in `internal/logging/body.go`.
2. AlertManager at `mwan/go/internal/ifmgr/alerts.go`. `Notify` and `Resolve` emit through `a.log.Log(nil, level, msg, ...)` and ride on top of the slog handler. Owns per-(kind, key) state-change suppression and per-(kind, key) repeat cadence (`RepeatResolver`, MWAN-121).
3. Direct `email.Sender.Send` in `mwan/go/internal/watchdog/failover.go` at lines 51, 102, 187. Bypasses slog and AlertManager.

Per-subcommand wiring: agent and ifmgr use the slog handler only. Watchdog uses both the slog handler and a direct `email.NewSender`. Opnsense daemons have no email.

### Persistent-WARN paths firing every iteration (testbed source)

These are not silenced. They route through `notify` with their proper severity so the state-change boundary bounds frequency to one email per transition plus one per repeat-cadence window. The subagent for slice D also investigates each underlying cause and either fixes it inline or files a follow-up.

| File:line | Message | Action in slice D |
| --- | --- | --- |
| `internal/watchdog/watchdog.go:542` | `getConfigState: vsock unavailable, used TCP fallback` | route through `notify.Notify(kind="vsock-fallback", key="<vmid>:<port>", level=WARN)`. Resolve on first vsock success. Investigate: is vsock provisioned on testbed VM 950? Prod VM 113 has it (MWAN-87). If absent, file a follow-up ticket to provision vsock on VM 950. |
| `internal/ops/ops.go:760` | `ops transport failed` | route through `notify.Notify(kind="ops-transport-failed", key="<channel>:<target>", level=WARN)`. Likely co-fires with `vsock-fallback`. If redundant in practice, suppress this one and keep `vsock-fallback` only. The investigation result documents this. |
| `internal/agent/server.go:273` | `read deploy file: open /var/run/mwan-last-deploy: no such file or directory` | route through `notify.Notify(kind="deploy-file-missing", key=path, level=WARN)`. Resolve once a real deploy creates the file. Investigate: should the agent gate this read on a "deploy info expected" config flag so a fresh host that never had Ansible deploy does not warn? If yes, the gating fix lands in slice D. If the deploy file is expected on every host, the route-through-notify is the only change. |

### oobv6 double-emit

`internal/ifmgr/modules/oobv6/oobv6.go:192` calls `log.Warn` AND on the next line calls `Alerts.Notify` for the same SLAAC renumber. The `log.Warn` line is removed in slice B.

### Config drift

| Field | prod (j2) | testbed (j2) | lxc-116 (literal) | lxc-100 (literal) |
| --- | --- | --- | --- | --- |
| `[email]` block exists | yes | yes | NO | NO |
| `from` | `mwan@goodkind.io` | `mwan-testbed@goodkind.io` | n/a | n/a |
| `min_level` | ERROR | WARN | n/a | n/a |
| `bind_iface` | `enmbrains0` | empty | n/a | n/a |
| `subject_prefix` | `[MWAN-vault]` | `[MWAN-suburban]` | n/a | n/a |
| `[ifmgr]` block | yes | NO | yes | yes |

Decision (user-confirmed): testbed `min_level` moves to ERROR (matches prod). The notify boundary makes per-event state-change drive emails regardless of slog level, so the WARN setting is no longer needed for visibility on testbed.

`SMTP2GO_API_KEY` env var path is supported at `internal/config/config.go:332`. Slice F uses it to add `[email]` blocks to lxc-116 and lxc-100 without converting them to `.j2` templates: secrets stay in `/etc/mwan/secrets.env` (rendered by Ansible from vault), the static toml has the non-secret email fields, the systemd unit gets `EnvironmentFile=/etc/mwan/secrets.env`.

## Approach: one notification boundary

Introduce `mwan/go/internal/notify/` with the following shape:

```
package notify

type Event struct {
    Now        time.Time
    Level      slog.Level
    Kind       string         // required, e.g. "wg-peer-stalled", "bgp-failover", "vsock-unavailable"
    Key        string         // optional sub-key, e.g. peer pubkey, vmid, iface
    Message    string         // single-line subject-friendly summary
    Fields     []slog.Attr    // structured body fields
    IsRecovery bool           // true for Resolve, false for Notify
}

type Notifier interface {
    Notify(ctx context.Context, ev Event)
    Resolve(ctx context.Context, kind, key, msg string, fields ...slog.Attr)
    Active(kind, key string) bool
}
```

Files in the new package:

- `internal/notify/notify.go`: types, `Notifier` interface, `New(cfg, log, sender)` constructor.
- `internal/notify/manager.go`: concrete `Manager`. State map carved verbatim from `internal/ifmgr/alerts.go` (per-(kind, key) `active` plus `lastEmit` plus `lastLevel`). Owns `RepeatResolver`.
- `internal/notify/body.go`: `BuildEmailBody`, moved from `internal/logging/body.go`.
- `internal/notify/email.go`: `Sink` that wraps `internal/email.Sender` plus subject prefix.
- `internal/notify/null.go`: `NullNotifier` for when `cfg.Email` is unconfigured.
- `internal/notify/notify_test.go` and `internal/notify/manager_test.go`: tests carved from `alerts_test.go` (captureHandler pattern works because `Manager` keeps a `slog.Logger` for journald emit).

Migration:

- AlertManager becomes `type AlertManager = notify.Manager` (alias) for one release, then deletes. Existing callers in ifmgr modules keep their `Notify` and `Resolve` call sites with no change.
- Watchdog `triggerBGPFailover` and `triggerBGPRecovery` build `notify.Event` with kinds `bgp-failover`, `bgp-failover-complete`, `bgp-recovered`. The 3 direct `SendEmail` call sites go away.
- The slog email handler (`email_handler.go`) and `BuildEmailBody` move into `notify`. The slog-side path is deleted in slice E once slices B, C, D have landed. Reason: keeping it as a "safety net" reintroduces the testbed-noise bug because any future `log.Warn` becomes an email.
- oobv6 line 192 `log.Warn` is removed; `Alerts.Notify` on line 193 stays.

## Slices (parallel-safe subagent worktrees)

Each slice is one branch off `origin/main` in its own worktree under `.claude/worktrees/notify-slice-<x>/`. Same pattern as MWAN-121..125. Slices A and F are independent foundations; B, C, D run in parallel after A; E gates on B, C, D.

### Slice A: notify package skeleton

- New: `internal/notify/notify.go`, `manager.go`, `body.go`, `email.go`, `null.go`, `notify_test.go`, `manager_test.go`.
- Modified: `internal/config/config.go` (add `Notify` config block or fold into `Email`; expose `RepeatEvery` plus `PerKind` map keyed by alert kind).
- Tests: copy state-change semantics tests verbatim from existing `alerts_test.go`.
- `make check`, `make test`.

### Slice B: ifmgr migrates to notify, oobv6 fix

- Modified: `internal/ifmgr/alerts.go` (becomes alias to `notify.Manager`; state map deleted).
- Modified: `internal/ifmgr/daemon.go` (constructor swap).
- Modified: `cmd/mwan/ifmgr_linux.go:109` and `:212` (`buildAlertRepeatPolicy` returns a `notify.Notifier`).
- Modified: `internal/ifmgr/modules/oobv6/oobv6.go:192` (delete the `log.Warn`).
- Modified: `internal/ifmgr/alerts_test.go` (move into `notify/manager_test.go`).
- Depends on: A.
- `make check`, `make test`, `make build-linux`.

### Slice C: watchdog failover migration

- Modified: `internal/watchdog/failover.go` lines 43-51, 94-102, 173-187 (replace `w.ops.SendEmail` with `w.notify.Notify` plus matching `Resolve`).
- Modified: `internal/watchdog/main.go:99-108` (construct `notify.Notifier` in place of bare `email.NewSender`).
- Modified: `internal/watchdog/failover.go:265,279` (drop the duplicate sender construction).
- Modified: `internal/ops/ops.go:75-77,93,118,719-721` (delete `SysOps.SendEmail`).
- Modified: `internal/watchdog/dryrun.go:70` (delete `dryRunOps.SendEmail`).
- Modified: `internal/watchdog/watchdog_test.go:174` (delete `mockOps.SendEmail` from the mock, update assertions).
- Modified: `internal/redteam/scenarios.go:314-315` (drop the SendEmail trigger).
- Depends on: A.
- `make check`, `make test`, `make build-linux`.

### Slice D: route persistent-WARN paths through notify, investigate underlying causes

- Modified: `internal/watchdog/watchdog.go:542` (route `vsock unavailable` through `notify.Notify(kind="vsock-fallback", key="<vmid>:<port>", level=WARN)` plus matching `Resolve` when vsock recovers).
- Modified: `internal/ops/ops.go:760` (route `ops transport failed` through `notify.Notify(kind="ops-transport-failed", key="<channel>:<target>", level=WARN)`. If investigation finds it always co-fires with `vsock-fallback`, suppress this one).
- Modified: `internal/agent/server.go:273` (route `read deploy file` through `notify.Notify(kind="deploy-file-missing", key=path, level=WARN)`).
- Modified: `internal/agent/main.go:47` and `cmd/mwan/ifmgr_linux.go:109` (swap `logging.EmailFromConfig` for `notify.FromConfig`).
- Investigation deliverable in this slice: for each of the three paths, the subagent reports the underlying cause and either applies an inline fix (if small and obviously correct) or files a follow-up ticket via `mcp__tack__tack_create_issue` with the investigation notes. Examples of likely follow-ups: provision vsock on testbed VM 950 (matching MWAN-87 on VM 113); gate the deploy-file read on a config flag for fresh hosts; reconcile redundant ops-transport vs vsock-fallback signals.
- Depends on: A.
- `make check`, `make test`, `make build-linux`.

### Slice E: logging cleanup

- Deleted: `internal/logging/email_handler.go`, `internal/logging/email_handler_test.go`, `internal/logging/body.go`, `internal/logging/body_test.go`, `internal/logging/handlers.go`.
- Modified: `internal/logging/factory.go` (drop the email-handler reference).
- Depends on: B, C, D.
- `make check`, `make test`, `make build-linux`, `make build-mwan-opnsense`.

### Slice F: config plus Ansible parity (no deploy)

- Modified: `mwan/config/suburban-testbed.toml.j2` lines 9-16 (set `min_level = "ERROR"`, set `bind_iface` from `mwan_email_bind_iface` Ansible variable). Append a full `[ifmgr]` section mirroring `production.toml.j2` lines 153-198 with role-appropriate values (read `mwan_testbed_servers.yml` for the testbed role; if not present, document the gap in this slice and stop short of inventing values).
- Modified: `mwan/config/production.toml.j2` lines 9-16 (parameterize `from`, `subject_prefix`, `bind_iface`, `min_level`, `cooldown` via Ansible variables; values stay the same).
- Modified: `mwan/production/lxc-116/config.toml` (add `[email]` block: `alert_email`, `from = "mwan@goodkind.io"`, `subject_prefix = "[MWAN-vault-failover]"`, `bind_iface = ""`, `min_level = "ERROR"`, `cooldown = "5m"`. Do NOT include `smtp2go_api_key`; it comes from env).
- Modified: `mwan/testbed/lxc-100/config.toml` (add `[email]` block with `from = "mwan-testbed@goodkind.io"`, `subject_prefix = "[MWAN-suburban-failover]"`, `min_level = "ERROR"`, `cooldown = "5m"`. Same env-var approach for the secret).
- Modified: `ansible/inventory/group_vars/mwan_servers.yml` (add `mwan_email_min_level`, `mwan_email_subject_prefix`, `mwan_email_bind_iface`, `mwan_email_cooldown`, `mwan_email_from`. Confirm `mwan_smtp2go_api_key` already exists in vault).
- New (or modified): `ansible/inventory/group_vars/mwan_testbed_servers.yml` (testbed-specific overrides for the same variables, plus `mwan_failover_subject_prefix` for lxc-100).
- New (Ansible task in `ansible/playbooks/deploy-mwan.yml` or its included tasks): render `/etc/mwan/secrets.env` from vault with `SMTP2GO_API_KEY`, mode 0640 root:root.
- New (or modified): the systemd units for mwan-agent and mwan-ifmgr add `EnvironmentFile=/etc/mwan/secrets.env`. Locations: `mwan/go/cmd/mwan/mwan-agent.service`, `mwan/go/cmd/mwan/mwan-ifmgr.service`, `mwan/production/lxc-116/mwan-ifmgr.service`, `mwan/testbed/lxc-100/mwan-ifmgr.service`.
- No `ansible-playbook` run in this slice. Verification is `ansible-playbook playbooks/deploy-mwan.yml --check --diff --limit <group>` only (read-only).
- Depends on: nothing (pure config + Ansible). Can run in parallel with A through E.
- No `make` targets apply.

### Slice G: documentation

- Modified: `AGENTS.md` (new section "Email and alert routing" describing `internal/notify` as the chokepoint; one-paragraph pointer at slice F's env-var contract).
- New: `mwan/docs/mwan-email-routing.md` (one-page diagram plus table of kinds, keys, levels, and the slice each one was migrated in. Read-the-doc for context recovery).
- Depends on: nothing (independent).

## Tack tickets

To be filed via `mcp__tack__tack_create_issue` after plan approval. No pre-picked numbers; tack assigns IDs.

Parent: "Unify email-emitting paths across mwan and bring testbed to prod email parity".

Children:

1. notify package skeleton with state-change semantics and BuildEmailBody.
2. Migrate ifmgr AlertManager to notify and remove oobv6 double-emit.
3. Migrate watchdog failover emails to notify and remove SysOps.SendEmail.
4. Route three persistent-WARN paths through notify, investigate and fix or file follow-ups for the underlying causes.
5. Remove internal/logging email path after migration.
6. Bring testbed config and Ansible vars to parity with prod (no deploy).
7. Document email routing in AGENTS.md and mwan/docs/.

Comments on the parent track each slice's commit SHA per the MWAN-121..125 pattern.

## Verification

Per slice: `make check` and `make test`. Slices touching Go code also run `make build-linux`. Slice E adds `make build-mwan-opnsense` because deletion impacts cross-compile.

After integration on a `email-unify-golden` branch:

- `make check && make test && make build-linux && make build-mwan-opnsense` all clean.
- Deploy golden binary to suburban testbed using existing AGENTS.md "Manual rollout" recipe (no Ansible run).
- Confirm the testbed flurry stops within one repeat-cadence window: zero emails for `vsock unavailable`, `ops transport failed`, `read deploy file`.
- Manually trip a `wg_health` peer-stalled and confirm: one email at first transition, one recovery email at original Notify level on Resolve.
- Trip a watchdog-style failover in dry-run and confirm: one email at failover, one at completion, one at recovery, all keyed and deduped.

Ansible parity check (slice F): `cd ansible && ansible-playbook playbooks/deploy-mwan.yml --check --diff --limit mwan_testbed_servers`. Inspect the rendered diff for `/etc/mwan/config.toml` and `/etc/mwan/secrets.env`. Same for prod with `--limit mwan_servers`. No `--check`-less invocation from any subagent.

## Risk callouts

1. AlertManager test coverage. Five existing tests in `alerts_test.go` cover state-change-only, `RepeatEvery`, `RepeatResolver`, recovery-at-original-level, and the no-double-prefix rule. They move verbatim into `notify/manager_test.go` so behavior is preserved.
2. Watchdog two parallel email paths. Removing `RealOps.SendEmail` is safe only after `failover.go` lines 51, 102, 187 are switched. Slice C lands as one commit so the deletion and the new call sites are atomic.
3. Slog email handler downgrade. Slice E removes it. Keeping it as a safety net reintroduces the testbed-noise bug. The journald path inside `notify.Manager` is the new safety net for events not surfaced through email.
4. Worktree merge order. Slice A first. Then B, C, D in any order (no shared files). Then E. Slice F merges anytime since it touches only configs and Ansible. Slice G last so docs can reference final ticket numbers.
5. Writing discipline. Each sentence is one finished thought. Two ideas glued with a dash means the thinking is unfinished; split them into two sentences. The agent-gate hook blocks em-dash and double-hyphen-as-prose because those are the symptoms of fused half-thoughts. Use commas, colons, periods, parentheses for genuine multi-clause sentences.

## Critical files for the implementer

- `mwan/go/internal/ifmgr/alerts.go` (state machine source; carved into `notify` in slice A).
- `mwan/go/internal/logging/email_handler.go`, `body.go`, `handlers.go` (slog email path; deleted in slice E).
- `mwan/go/internal/watchdog/failover.go:51,102,187,265,279` (direct sender call sites).
- `mwan/go/internal/watchdog/watchdog.go:542` (vsock-unavailable WARN).
- `mwan/go/internal/ops/ops.go:75-77,93,118,719-721,760` (`SysOps.SendEmail` removal site, `ops transport failed`).
- `mwan/go/internal/agent/server.go:273` (read deploy file WARN).
- `mwan/go/internal/agent/main.go:47`, `cmd/mwan/ifmgr_linux.go:109,212` (per-subcommand wiring).
- `mwan/go/internal/ifmgr/modules/oobv6/oobv6.go:192` (double-emit).
- `mwan/go/internal/config/config.go:38-46,251-253,332,436,452` (Email and IfMgrAlerts config; env-var path).
- `mwan/config/production.toml.j2`, `mwan/config/suburban-testbed.toml.j2`.
- `mwan/production/lxc-116/config.toml`, `mwan/testbed/lxc-100/config.toml`.
- `ansible/inventory/group_vars/mwan_servers.yml`, `ansible/inventory/group_vars/mwan_testbed_servers.yml`.
- Systemd units: `mwan/go/cmd/mwan/mwan-agent.service`, `mwan-ifmgr.service`, `mwan/production/lxc-116/mwan-ifmgr.service`, `mwan/testbed/lxc-100/mwan-ifmgr.service`.

## Hard rules for every subagent

1. Operates only inside its worktree path. No edits outside its assigned files.
2. No `ansible-playbook` run. Slice F may run `ansible-playbook --check --diff` for verification only.
3. No `systemctl restart` on any host. No `scp` to any host. Local repo only.
4. No commits to `main` or any branch other than the slice's own branch.
5. Each sentence must be one complete thought with its own subject and verb. Do not fuse two half-formed ideas with a dash to make them look fluent. If a thought needs two clauses, finish each one as its own sentence. The agent-gate hook enforces this by blocking em-dash characters and double-hyphen used as prose dashes; that enforcement is downstream of the writing rule, not the rule itself. Command flags like `--help` are allowed in command contexts.
7. `make check` and `make test` must pass on the slice's branch before reporting done.
8. Cross-compile checks (`make build-linux`, `make build-mwan-opnsense`) must pass for slices touching Go.

## Recovery anchor

If this session's context is lost, resume from these points:

- Plan file: `/Users/agoodkind/.claude/plans/let-us-plan-to-goofy-starfish.md` (this file).
- Persisted copy: `mwan/docs/MWAN-<assigned>-email-unification-plan.md` after the parent ticket is filed.
- Tack tickets: search for parent "Unify email-emitting paths across mwan and bring testbed to prod email parity". Children are filed sequentially after the parent.
- Branch naming convention: `notify-slice-a`, `notify-slice-b-ifmgr`, `notify-slice-c-watchdog`, `notify-slice-d-noise`, `notify-slice-e-cleanup`, `notify-slice-f-config-parity`, `notify-slice-g-docs`. Each in its own worktree under `.claude/worktrees/`.
- Resume: read this plan, run `git worktree list`, identify which slices are merged versus in-flight, pick up the unfinished slice. The slice tables here are authoritative on file paths and dependencies.
- User-stated active constraints: subagents in isolated worktrees; track via tack with no pre-picked IDs; Ansible kept in lockstep but NOT deployed; preserve and conserve context; only respond to questions asked.
- Adjacent prior plans: `MWAN-130` BGP graceful restart at `mwan/docs/MWAN-130-bgp-graceful-restart-plan.md`. The 26.x parent is `MWAN-13`. The path to it is testbed-first: bring testbed config to prod parity (this plan handles the email axis; MWAN-117/118/119/127 handle the OPNsense `config.xml` axis), then upgrade testbed to 26.x, capture issues, mediate, then upgrade prod. Testbed is freely nukable. Prod is not.

## Out of scope

- BGP graceful restart (`MWAN-130`).
- OPNsense 26.x config import (`MWAN-117/118/119/127`).
- Extracting `internal/logging` into a shared module (`MWAN-56`, `MWAN-57`).
- Refactoring `internal/logging.Config` to handler-list shape (`MWAN-56`).
- Converting lxc-116 and lxc-100 configs to `.j2` templates (`MWAN-22`). Replaced by the env-var injection pattern tracked under `MWAN-131` (Adopt env-var injection for secrets, drop .j2 templating where possible). First instance of that pattern lands in slice F of this plan; the broader sweep belongs to `MWAN-131`.
- Adding new alert kinds. Migration only.
- Replacing `email.Sender` with a different transport.
