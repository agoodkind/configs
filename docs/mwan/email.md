# MWAN email and alert routing

Forward-looking design doc for MWAN-132. The unified routing described here lands across
slices A through F of that ticket. Until those slices merge to `main`, the live code
still has three independent email surfaces (slog `email_handler`, ifmgr `AlertManager`,
direct `email.Sender.Send` in watchdog) and the `internal/notify` package referenced
below may not exist on every branch yet. Where a path is forward-looking, it is called
out inline.

## Why

Email currently leaks out of three independent surfaces in [mwan/go/](../../mwan/go/). Two of them
bypass the per-(kind, key) state-change suppression that the ifmgr `AlertManager`
implements. The result on testbed is an every-five-minutes flurry of `vsock unavailable`,
`ops transport failed`, and `read deploy file` emails that obscures real signals. The
unification gives every email one chokepoint, one dedup policy, one repeat cadence, and
one place to migrate when the transport changes. It also unblocks the testbed-to-prod
parity work that gates the OPNsense 26.x upgrade (parent MWAN-13).

## Boundary

`internal/notify` defines the boundary. Forward-looking; lands in slice A.

```go
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

Files in the new package (forward-looking, slice A):

- `internal/notify/notify.go`: types, `Notifier` interface, `New(cfg, log, sender)` constructor.
- `internal/notify/manager.go`: concrete `Manager` with per-(kind, key) `active`, `lastEmit`, `lastLevel` plus `RepeatResolver`.
- `internal/notify/body.go`: `BuildEmailBody`, moved from `internal/logging/body.go`.
- `internal/notify/email.go`: `Sink` wrapping `internal/email.Sender` plus subject prefix.
- `internal/notify/null.go`: `NullNotifier` for when `[email]` is unconfigured.

## Sources

Each source below identifies the call site, the kind string used by `notify`, the shape
of the per-event key, the level, and which subsystem calls `Resolve` to clear the active
state. The "Slice" column points to the MWAN-132 slice that performs the migration.

| Source path | Kind | Key shape | Level | Resolve caller | Slice |
| --- | --- | --- | --- | --- | --- |
| `internal/ifmgr/modules/wg_health/*` | `wg-peer-stalled` | `<iface>:<pubkey>` | WARN | wg_health module on next healthy poll | B |
| `internal/ifmgr/modules/oobv6/oobv6.go:192` | `oobv6-renumber` | `<iface>` | WARN | oobv6 module when SLAAC stabilizes | B |
| `internal/watchdog/failover.go:43-51` | `bgp-failover` | `<vmid>` | ERROR | watchdog `triggerBGPRecovery` | C |
| `internal/watchdog/failover.go:94-102` | `bgp-failover-complete` | `<vmid>` | INFO | self-clearing on next failover transition | C |
| `internal/watchdog/failover.go:173-187` | `bgp-recovered` | `<vmid>` | INFO | self-clearing on next failover transition | C |
| `internal/watchdog/watchdog.go:542` | `vsock-fallback` | `<vmid>:<port>` | WARN | watchdog on first vsock success | D |
| `internal/ops/ops.go:760` | `ops-transport-failed` | `<channel>:<target>` | WARN | ops layer on next successful send (may be suppressed if it always co-fires with vsock-fallback; investigation in slice D decides) | D |
| `internal/agent/server.go:273` | `deploy-file-missing` | `<path>` | WARN | agent on first successful deploy-file read | D |

The kind strings above are the values planned in the MWAN-132 plan. The exact strings
land with the implementing slice; if a slice diverges, that slice is the source of truth.

## Config

Two TOML blocks govern routing. The non-secret fields live in `/etc/mwan/config.toml`
(rendered by Ansible from [config-vm.toml.j2](../../mwan/config/config-vm.toml.j2) or
[mwan-failover/config.toml.j2](../../mwan-failover/config.toml.j2)). The SMTP2GO API key is injected at runtime via a
systemd `EnvironmentFile`.

`[email]` block, today and after MWAN-132:

```toml
[email]
alert_email    = "alex@goodkind.io"
from           = "mwan@goodkind.io"          # testbed: "mwan-testbed@goodkind.io"
subject_prefix = "[MWAN-vault]"              # testbed: "[MWAN-suburban]"
bind_iface     = "enmbrains0"                # testbed: ""
min_level      = "ERROR"                     # testbed moves from WARN to ERROR in slice F
cooldown       = "5m"
```

`[notify]` block (forward-looking, slice A; may fold into `[email]` if the implementer
chooses):

```toml
[notify]
repeat_every = "1h"

[notify.per_kind]
"vsock-fallback"        = "30m"
"ops-transport-failed"  = "30m"
"deploy-file-missing"   = "1h"
```

Env-var injection contract (MWAN-131, first instance in MWAN-132 slice F):

- Vault stores `vault_smtp2go_api_key`.
- Ansible renders `/etc/mwan/secrets.env` with `SMTP2GO_API_KEY=...`, mode 0640 root:root.
- Systemd units (`mwan-agent.service`, `mwan-ifmgr.service`, plus the lxc-116 and lxc-100
  service files) gain `EnvironmentFile=/etc/mwan/secrets.env`.
- `internal/config/config.go:332` already reads `SMTP2GO_API_KEY` from the environment
  when the toml field is empty, so no code change is needed for the env-var read path.
- Result: lxc-116 and lxc-100 render non-secret email fields from
  [mwan-failover/config.toml.j2](../../mwan-failover/config.toml.j2). The secret stays out of the toml.

## Failure modes

Sender error (SMTP2GO returns 5xx, network blip, bind_iface gone). The sender returns an
error to `notify.Manager`. The manager logs the failure to journald via its embedded
`slog.Logger` and leaves the per-(kind, key) `active` flag set so the next state-change
emits at all. Repeat-cadence still fires on the next interval; if the transport stays
broken, every cadence tick logs a journald error rather than spamming retry email. There
is no on-disk retry queue.

Manager state lost on restart. The state map (`active`, `lastEmit`, `lastLevel`) is
in-memory only. After a process restart, the first event of any kind/key pair always
emits because the manager has no record of an active alert. That is intentional: it
errs toward emitting after a crash rather than silently swallowing a real alert. The
trade-off is that planned restarts (Ansible deploy, systemd reload) can produce one
extra email per active condition. The journald log records `notify.Manager: starting
with empty state` so an operator can correlate.

Unknown kind. `notify.Notify` accepts any non-empty kind string and treats unseen kinds
the same as known ones: per-(kind, key) state-change suppression with the default
repeat cadence from `[notify].repeat_every`. There is no allow-list of kinds. The
journald log includes the kind on every emit so an operator can spot typos.

Sender unconfigured. If `[email]` is absent or `alert_email` is empty, `notify.New`
returns a `NullNotifier`. Every `Notify` and `Resolve` call is a no-op apart from a
journald-level log line at the event's slog level. This is the path lxc-116 and lxc-100
ride today (no `[email]` block); slice F adds the block on both, after which they emit
through the same path as the rest.

## Pointers

- Plan and parent ticket: MWAN-132.
- Env-var injection standard: MWAN-131.
- Existing state-machine source (carved into `notify` in slice A):
  [mwan/go/internal/ifmgr/alerts.go](../../mwan/go/internal/ifmgr/alerts.go) and
  [mwan/go/internal/ifmgr/alerts_test.go](../../mwan/go/internal/ifmgr/alerts_test.go).
