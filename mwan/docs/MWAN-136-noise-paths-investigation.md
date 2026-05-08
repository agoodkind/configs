# MWAN-136 noise-paths investigation

Slice D of the MWAN-132 email unification plan routes three persistent
WARN paths through the new `internal/notify` boundary so the
state-change machine bounds frequency to one email per transition plus
one per repeat-cadence window. The user explicitly asked that
underlying causes be investigated rather than masked. This document
records what was found for each of the three paths and where the
inline fix went or which follow-up ticket captured it.

## 1. `vsock-fallback` (watchdog.go:542 → routeChannelFallback)

**Underlying cause.** The watchdog calls `ops.GetConfigState` every
hash-check cycle. That call tries vsock first, then TCP. On testbed VM
950 vsock fails on every attempt because the VM has no
`vhost-vsock-pci` device. Verified with `ssh suburban 'qm config 950'`:
no `vhost-vsock-pci` line and no `args:` line that would inject one.
Production VM 113 has the device per MWAN-87 and does not see this
fallback in steady state. The WARN was firing every 5-minute hash check
cycle because nothing bounded it.

**Decision.** Route through `notify.Notify(kind="vsock-fallback",
key="<vmid>:<port>", level=WARN)` so the state-change boundary holds
the email cadence. Resolve fires on the next vsock success. The fix to
provision the device on VM 950 is filed as **MWAN-143** because it is a
Proxmox infra change outside the slice D scope and outside the mwan Go
codebase.

## 2. `ops-transport-failed` (ops.go:760)

**Underlying cause.** `logAttemptResult` fires WARN per failed attempt
across vsock and TCP. Tracing the call chain: when vsock fails and TCP
succeeds (the testbed steady state), the failed-vsock attempt logs
`ops transport failed` and immediately after, the caller in
`watchdog.checkConfigHash` logs `getConfigState: vsock unavailable,
used TCP fallback`. Both fire at WARN, both emit per cycle. They are
the same event surfaced twice. When all channels fail, the parent
caller logs its own WARN with the wrapped error, so
`ops-transport-failed` is again redundant.

**Decision.** Suppress the notify route for this path; do not register
a `notify.Notify(kind="ops-transport-failed", ...)`. Keep the existing
`log.WarnContext` so the per-attempt detail still lands in journald
for postmortem reading, but rely on `vsock-fallback` (path 1 above)
or the parent's wrapped-error WARN to drive the email path. Slice E
deletes the slog email handler outright, so the journald-only WARN
becomes truly journald-only at that point.

## 3. `deploy-file-missing` (agent/server.go:273)

**Underlying cause.** The agent reads `/var/run/mwan-last-deploy` on
every `GetConfigState` call. `/var/run` is tmpfs and is cleared on
every reboot. The Ansible `deploy-mwan` task repopulates the file, but
between reboot and the next Ansible run the file is missing and the
agent emits WARN every 5 minutes (driven by the watchdog hash-check
poll). The file location is structurally wrong: a "last deploy" record
that vanishes on reboot cannot serve as a deploy-staleness signal.

**Decision.** Route through `notify.Notify(kind="deploy-file-missing",
key=<path>, level=WARN)` plus `notify.Resolve` on the first successful
read. The persistent-storage fix (move the file to `/var/lib/mwan/`
or `/etc/mwan/`) plus the "deploy expected" gate for fresh hosts both
land in **MWAN-144** since they require Ansible-side and config-schema
changes that are outside the slice D Go-only scope.

## Summary

| Path | Inline fix in slice D | Follow-up |
|---|---|---|
| `vsock-fallback` | route through notify with Resolve on success | MWAN-143 (provision vsock on VM 950) |
| `ops-transport-failed` | suppress notify route (always co-fires with vsock-fallback or parent WARN); leave WARN log for journald | none |
| `deploy-file-missing` | route through notify with Resolve on success | MWAN-144 (persistent storage + "expected" gate) |

The overall effect on the testbed flurry: vsock-fallback now emits at
most one email per state transition plus one per repeat-cadence
window, deploy-file-missing the same. ops-transport-failed no longer
emits to email at all (slice E deletes the slog email handler). The
per-cycle WARN cascade that motivated MWAN-132 is closed at the
boundary.
