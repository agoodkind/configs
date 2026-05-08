# MWAN-152: OPNsense upgrade rollback wired into the Go monolith

Tracking ticket: `MWAN-152`. This document is a design artifact only. It does not implement the subcommand. The implementation lives in a follow-up ticket suggested at the end of this document.

Sibling planning artifacts referenced throughout:

- `mwan/docs/MWAN-140-config-xml-transform-spec.md` (config.xml transform shape, `internal/opnsense/configxform/` precedent for sibling packages).
- `mwan/docs/runbooks/opnsense-testbed-config-import.md` (the every-change gate already enforced for MWAN-13/MWAN-127 rehearsal work).
- `mwan/go/internal/ops/ops.go` (existing `SysOps` interface with `VMSnapshot`, `VMRollback`, `VMSnapshots`, `VMDelSnapshot`).
- `mwan/go/internal/rollback/state.go` (existing rollback state file plus `SnapshotsAfter` helper).
- `mwan/go/internal/notify/` (unified email/state-change boundary from MWAN-132).
- `mwan/go/pkg/pveapi/client.go` (Proxmox QGA `GuestExec` client).
- `mwan/go/internal/opnsense/rpc_typed.go` (typed mwan-opnsense RPC: `Version`, `Exec`, `ReadConfigXML`, etc.).

## 1. Why rollback matters

The OPNsense upgrade from 25.7.x to 26.x is a destructive operation against the single L3 boundary for the home network. Three failure shapes have already surfaced during MWAN-119 v1 and v2 attempts and during the MWAN-13/MWAN-127 testbed rehearsals, captured in `mwan/docs/runbooks/opnsense-testbed-config-import.md`:

- The runbook's section 7 ("Failure Rule") states "If any baseline check fails, stop. Roll back immediately." It assumes a working snapshot to roll back to. The MWAN-119 v1 and v2 candidate-config import attempts both ended at this gate. The runbook now requires "Restore the candidate config onto a wiped or freshly restored OPNsense testbed baseline" because layered mutations on top of mutated state were unrecoverable without a snapshot.
- The auto-memory entry `feedback_opnsense_xml.md` records that hand-editing `config.xml` with `sed` "fails silently." A failed upgrade that lands a partially rewritten `config.xml` produces the same shape: services start but with malformed config. The validation step (section 6 of the runbook) has to catch this, and the rollback path has to exist.
- The auto-memory entry `feedback_cutover_failure.md` describes a production cutover that failed because the live `nftables` forward chain and the IPv6 NDP cache were not what the repo template said they were. Translated to upgrade rollback: the live OPNsense state at the moment of upgrade may diverge from any pre-recorded baseline, so the rollback artifact has to capture the actual live state at `prepare` time and not trust template-derived expectations.

The runbook's section 6 ("Post-Change Validation") enumerates the in-guest checks that are already used as a rollback gate: QGA, serial console, SSH, web UI port 443, DNS NOERROR, `configctl`, interface device bindings, default routes, pf rule load, NAT sanity, FRR/BGP state, and MWN1 `Version` plus `ReadConfigXML`. The design below treats this list as the contract that `validate` must enforce.

## 2. Snapshot mechanism choices

Four candidates, with pros and cons grounded in what the existing code and infrastructure already support.

### 2.1 Proxmox VM-level snapshot via `qm snapshot`

Pros:

- Atomic at the VM disk plus QEMU device state level. Rollback restores both the disk and the running guest state, so a half-applied package install on the OPNsense UFS or ZFS root is reverted without per-file reasoning.
- Already wired into the Go monolith. `internal/ops/ops.go` exposes `VMSnapshot(ctx, vmid, snapName)`, `VMRollback(ctx, vmid, snap)`, `VMSnapshots(ctx, vmid)`, and `VMDelSnapshot(ctx, vmid, snapName)`. `internal/rollback/state.go` already has `ExtractLatestSnapshot` and `SnapshotsAfter` helpers, plus the watchdog uses these to delete child snapshots before rolling back to a leaf.
- Already in use for testbed rollbacks. The auto-memory entry `feedback_testbed_operations.md` records that the suburban testbed uses pre-flight snapshots like `pre-cutover2-v2` for MWAN-119 rollbacks, and the watchdog uses `pre-deploy-*` and `known-good-*` snapshots in `mwan/proxmox/scripts/mwan-watchdog.sh` (per `mwan/go/internal/rollback/state.go` regex set).
- `qm snapshot --vmstate` captures memory state, so the rolled-back VM resumes mid-flight rather than rebooting. For OPNsense upgrade rollback this matters less than for live deploys, since the upgrade reboots the VM anyway, but `--vmstate` is the existing convention.

Cons:

- Snapshot creation pauses the VM briefly. On a multi-GB OPNsense VM this is seconds, not minutes, but it is non-zero. Time the pause against the link-loss tolerance of upstream BGP peers. The MWAN-130 graceful-restart work documents the BGP convergence window, and the snapshot pause has to fit inside it or the upstream peers will withdraw routes.
- Rollback is synchronous and brings the VM down for the duration of the disk revert.
- Storage-backend dependent. On ZFS-backed Proxmox storage the snapshot is a near-instant ZFS clone; on LVM-thin it is a thin-volume snapshot; on directory storage it is qcow2 internal snapshots. The cost and latency profile differs per backend. Verify the prod vault VM 101 disk backend before assuming costs.

### 2.2 ZFS dataset snapshot at the host level

Pros:

- Faster than `qm snapshot` because it skips QEMU device state serialization. Pure block-level snapshot of the underlying dataset.
- Independent of Proxmox, so a Proxmox bug or PVE API outage does not block snapshot or restore.

Cons:

- Requires the VM disk to live on a ZFS dataset. The vault prod host runs Debian 13 with Proxmox VE; its zpool layout for the prod OPNsense VM 101 disk needs verification before this path is viable. Per `INFRA.md`, suburban runs the same Debian/Proxmox pattern, but the storage-backend mapping for VM 101 is not documented in the file as I read it on 2026-05-08.
- Does not capture QEMU device or memory state. A rollback resets the VM to "as if powered off" at snapshot time, then the VM has to boot from the rolled-back disk. For an upgrade rollback this is acceptable, since the upgrade itself reboots the VM.
- No existing wiring in the Go monolith. We would have to shell out to `zfs snapshot` and `zfs rollback` from inside `internal/ops/`, plus add a code path that picks ZFS over `qm` based on detected backend.

### 2.3 In-guest OPNsense `boot-environments`

Pros:

- OPNsense's own native upgrade-rollback primitive. Survives the upgrade itself, and OPNsense's boot menu lets a stuck guest pick the prior environment without host involvement.
- No host-side state required.

Cons:

- Requires the OPNsense root filesystem to be ZFS. UFS-rooted installs do not get boot environments. Verify the vault prod VM 101 root filesystem before assuming.
- Source: not confirmed for OPNsense 26.x. The OPNsense 25.7 line documents `bectl` and `opnsense-bootenv` but I do not have a verifiable source on 26.x parity, so flag as an assumption (open question 9.2).
- In-guest only. If the upgrade leaves the guest unable to boot any environment (kernel panic from a botched loader config), the host has no recovery lever beyond a Proxmox snapshot. Boot environments are a defense in depth, not a sufficient rollback mechanism on their own.
- Requires SSH or console to invoke. The runbook section 5 ("During Reload") already mandates serial console observation, so this fits the pattern, but it adds a manual step that VM-level snapshot does not.

### 2.4 Config-only snapshot (just `/conf/config.xml`)

Pros:

- Cheap. Single file copy via `mwan-opnsense` `BackupConfigXML` RPC (already implemented in `internal/opnsense/rpc_typed.go`).
- Perfect for "the upgrade succeeded but the new config.xml is broken" scenarios, where the binaries are fine but the auto-migrated config is wrong.

Cons:

- Does not protect against package upgrade failures, kernel panics, or filesystem corruption. The 26.x upgrade ships new pf rules, new daemons, and new defaults; a config.xml from 25.7 may not even parse on 26.x.
- Does not protect against partial upgrade state where some packages installed and others did not.

### 2.5 Recommendation in section 3

Section 3 picks Proxmox VM-level snapshot as primary, with config-only snapshot as a complementary belt-and-suspenders artifact captured in the same `prepare` phase.

## 3. Recommended approach

Primary mechanism: Proxmox VM-level snapshot via `qm snapshot --vmstate 1`. Complementary artifact: `BackupConfigXML` capture of `/conf/config.xml` plus a hash record of pre-upgrade state.

Justification:

- The Go monolith already has the `qm` wiring (`internal/ops/ops.go`), already has the snapshot lifecycle helpers (`internal/rollback/state.go`), and the existing watchdog rollback path proves the pattern works end-to-end on both vault prod and suburban testbed (per the watchdog script and the MWAN-119 testbed history).
- The rollback contract is symmetric with what the watchdog already uses: snapshot before deploy, optional manual delete of the snapshot once the deploy is committed. Operators recognize the pattern; the design does not invent a new lifecycle.
- The complementary `config.xml` capture is cheap and lands in two places: it goes into the Proxmox snapshot disk image, and it is captured separately on the operator workstation so a partial host-side disaster (vault disk fails during upgrade) still lets the operator hand-rebuild a 25.7 OPNsense and restore the captured `config.xml`.
- ZFS-host snapshots are deferred. They become attractive only if the prod VM 101 disk turns out to live on a ZFS dataset and the snapshot pause from `qm snapshot --vmstate 1` is too long for the upstream BGP graceful-restart window. The follow-up ticket should add a backend probe in `prepare` and surface the latency to the operator before deciding.
- Boot environments are deferred. They are a defense in depth that the operator can opt into manually if the OPNsense 26.x install includes them. The Go subcommand does not depend on them.

The snapshot name follows the existing convention: `pre-upgrade-26x-<unix-timestamp>`. The watchdog conventions `pre-deploy-*` and `known-good-*` are reserved for the agent/watchdog flow, so the upgrade flow uses its own prefix to keep the regex sets in `internal/rollback/state.go` from conflating the two lifecycles.

## 4. Go subcommand design

Entry point: `mwan opnsense-upgrade <subcommand> [flags]`. The subcommand lives at `mwan/go/cmd/mwan/opnsense_upgrade.go`, with the underlying logic in a new sibling package `mwan/go/internal/opnsense/upgrade/`. This mirrors the placement convention from `MWAN-140-config-xml-transform-spec.md` section 6, where the transform package sits at `mwan/go/internal/opnsense/configxform/` rather than bloating the RPC client.

Surface:

```
mwan opnsense-upgrade prepare  --vmid 101 --target 26.7
mwan opnsense-upgrade execute  --vmid 101 --target 26.7
mwan opnsense-upgrade validate --vmid 101 --target 26.7 [--accept-partial]
mwan opnsense-upgrade rollback --vmid 101 [--snapshot pre-upgrade-26x-1746657600]
mwan opnsense-upgrade commit   --vmid 101 [--snapshot pre-upgrade-26x-1746657600]
mwan opnsense-upgrade run      --vmid 101 --target 26.7 [--unattended]
```

Top-level flags shared by every subcommand: `--vmid`, `--node` (Proxmox node, defaults to `cfg.PVE.Node`), `--state-dir` (defaults to `/var/lib/mwan/upgrade/`).

### 4.1 prepare

Inputs: `vmid`, `target`, `state_dir`.

Behavior:

1. Load `cfg` via `config.Load()` so `pveapi`, `notify`, and `ops` are available.
2. Connect to mwan-opnsense via the existing `internal/opnsense/rpc_typed.go` `RPC` client. Call `Version` to record the running OPNsense version. Fail the prepare if `Version` does not match the operator-declared `--from` (default: whatever the running guest reports, but warn loudly if the operator did not pin a `--from`).
3. Capture pre-upgrade state under `<state_dir>/<vmid>/<deploy-id>/`:
   - `version.txt` from `Version`.
   - `config.xml.pre` from `BackupConfigXML`.
   - `config.xml.pre.sha256` for tamper detection.
   - `bgp_status.json` from `ops.GetBGPStatus`.
   - `interfaces.json` from a new helper that calls `Exec` with `ifconfig -l` plus `netstat -rn -f inet` and `netstat -rn -f inet6`. The runbook's section 6 lists "Interface device bindings" and "Default routes" as gate items; capturing them at `prepare` lets `validate` diff against them.
   - `metadata.json`: deploy ID, target version, snapshot name, timestamps.
4. Take the Proxmox snapshot via `ops.VMSnapshot(ctx, vmid, "pre-upgrade-26x-"+ts)`. The same `qm snapshot --vmstate 1` path the watchdog uses.
5. Write the rollback state file via the existing `rollback.WriteState` (extended with a `phase` field, see section 4.7). The file lives at `<state_dir>/<vmid>/state.txt` so a separate process (or a re-invocation of the subcommand) can read it.
6. Emit a `notify.Notify` event with kind `opnsense_upgrade`, key `prepared`, level `Info`. The unified notify boundary handles state-change suppression and email delivery per MWAN-132.

Failure path: if any step before `VMSnapshot` fails, the subcommand exits non-zero and does nothing. If `VMSnapshot` fails specifically, the subcommand emits an `opnsense_upgrade`/`prepare_failed` alert at `Error` and exits non-zero.

### 4.2 execute

Inputs: `vmid`, `target`, `state_dir`.

Behavior:

1. Load state from `<state_dir>/<vmid>/state.txt`. Refuse to execute if `phase` is not `prepared`.
2. Run the OPNsense upgrade in the guest. The mechanism is open question 9.3; primary candidate is `pveapi.Client.GuestExec` with `["opnsense-upgrade", "-r", target]` because QGA bypasses any networking dependencies that the upgrade itself might break. Alternative: the mwan-opnsense `Exec` RPC, which has the advantage of going over the existing vsock channel but the disadvantage that the upgrade is likely to kill or restart the daemon mid-execution, which would close the vsock channel and orphan the call.
3. Stream stdout and stderr from `GuestExecStatus` into `<state_dir>/<vmid>/<deploy-id>/upgrade.log`.
4. Apply a watchdog timeout. The upgrade has a known maximum duration on the prod VM 101 hardware that needs measurement on the testbed first; until then, the subcommand uses `cfg.Watchdog.UpgradeTimeout` defaulting to 30 minutes. If the timeout fires, transition to phase `execute_hung` and trigger automatic rollback (section 6).
5. On clean exit, write `phase=executed` to the state file and emit `opnsense_upgrade`/`executed`.

Failure path: non-zero exit code from the guest exec puts the state file at `phase=execute_failed`. The subcommand does not auto-rollback from this state; the operator runs `validate` first to decide whether the failure is recoverable.

### 4.3 validate

Inputs: `vmid`, `target`, `state_dir`, optional `accept-partial`.

Behavior:

1. Load state. Refuse unless `phase` is `executed` or `execute_failed`.
2. Run the runbook section 6 check matrix. Each check returns `pass` or `fail` with a free-form note. The matrix is implemented as a slice of named checks so the test plan in section 8 can stub individual checks.
3. The check set, mirroring the runbook:
   - `qga_responsive`: `pveapi.GuestExec` returns within 5 seconds.
   - `serial_console_responsive`: stretch goal; deferred to MWAN-95 OOB serial wiring.
   - `ssh_reachable`: `Exec` of `true` over the mwan-opnsense channel.
   - `web_ui_443`: `pveapi.GuestExec` of `nc -z 127.0.0.1 443`.
   - `dns_noerror`: `pveapi.GuestExec` of `drill @127.0.0.1 home.goodkind.io.` (or `dig` if `drill` is gone in 26.x).
   - `configctl_basic`: `pveapi.GuestExec` of `configctl interface list`.
   - `interface_bindings_match_pre`: diff against the captured `interfaces.json`.
   - `default_routes_sane`: presence of an IPv4 default and an IPv6 default.
   - `pf_rules_loaded`: `pfctl -sr | wc -l` is non-zero.
   - `nat_sanity`: `pfctl -sn` exits zero.
   - `frr_state`: `vtysh -c "show ip bgp summary"` exits zero and shows established sessions for the configured peers.
   - `mwn1_version`: `RPC.Version` returns a 26.x build identifier.
   - `mwn1_read_config_xml`: `RPC.ReadConfigXML` returns without error.
4. Aggregate results. If every check passes, write `phase=validated` and emit `opnsense_upgrade`/`validated` at `Info`. If any check fails, write `phase=validate_failed` with the failing check names as fields, and emit `opnsense_upgrade`/`validate_failed` at `Error`.
5. Partial pass handling: if some checks pass and others fail, the default behavior is `validate_failed`. `--accept-partial` overrides this, prompts the operator interactively, and if confirmed records `phase=validated_partial`. The state machine treats `validated_partial` as a terminal "manual" state that neither `commit` nor `rollback` will touch without explicit operator intent.

### 4.4 rollback

Inputs: `vmid`, optional `snapshot`.

Behavior:

1. Load state. Refuse unless `phase` is one of `executed`, `execute_failed`, `execute_hung`, `validate_failed`, or `validated_partial`. Refuse if `phase` is `validated` or `committed` (use a dedicated `revert-committed` flow for those, out of scope here).
2. Resolve the target snapshot. Default: the snapshot recorded in the state file from `prepare`. Override: `--snapshot` flag (operator can roll back to an older snapshot if a prior upgrade also went bad and the current state file points to the wrong target).
3. Delete child snapshots in newest-first order via `rollback.SnapshotsAfter` plus `ops.VMDelSnapshot`. The watchdog already does this in `executeRollbackVM`; the upgrade flow reuses the same logic.
4. `ops.VMRollback(ctx, vmid, snap)`. This stops the VM, reverts the disk and `--vmstate` memory, then leaves the VM stopped or running depending on the `--vmstate` flag at snapshot time.
5. `ops.VMStart(ctx, vmid)` if needed.
6. Wait for the QGA to come back via a polling loop with a 60-second deadline.
7. Re-run the `validate` matrix as a post-rollback sanity check. If it passes, write `phase=rolled_back` and emit `opnsense_upgrade`/`rolled_back` at `Warn`. If it fails, write `phase=rollback_failed` and emit `opnsense_upgrade`/`rollback_failed` at `Error` (section 6).
8. Do not delete the pre-upgrade snapshot. The operator commits the rollback by running `commit` with the rollback-target snapshot, or leaves the snapshot in place for forensic inspection.

### 4.5 commit

Inputs: `vmid`, optional `snapshot`.

Behavior:

1. Load state. Refuse unless `phase` is `validated` or `rolled_back`.
2. Resolve the snapshot to delete. Default: the snapshot from the state file.
3. `ops.VMDelSnapshot(ctx, vmid, snap)`. This is the "release the safety net" step. After commit, the upgrade (or rollback) is final and the prepare-phase snapshot no longer occupies storage.
4. Write `phase=committed` and emit `opnsense_upgrade`/`committed` at `Info`.

`commit` is idempotent: running it twice is a no-op.

### 4.6 run (unattended mode)

`run` is `prepare` then `execute` then `validate`, with automatic `rollback` on a failure that the design considers safe to auto-revert.

Auto-rollback triggers:

- `prepare` failed before `VMSnapshot`: nothing to roll back, exit non-zero with the prepare error.
- `execute_hung` (watchdog timeout): auto-rollback, then re-validate.
- `execute_failed` AND a subsequent `validate` returns zero passing checks: auto-rollback.
- `validate_failed` with all-or-nothing failure: auto-rollback.

Manual-only (do not auto-rollback):

- `validate` returns a partial pass (some checks pass, some fail). The state machine pauses at `validate_failed` and the operator decides. This matches the runbook's caution about not calling a reload "hung without console or log evidence."

`run` emits a final `opnsense_upgrade`/`run_complete` event at the highest severity reached during the run.

### 4.7 State file extensions

The existing `rollback.WriteState` writes deploy_timestamp, rollback_done, rollback_timestamp, snapshot, rollback_attempts. The upgrade flow adds:

- `phase`: one of `prepared`, `executed`, `execute_failed`, `execute_hung`, `validated`, `validated_partial`, `validate_failed`, `rolled_back`, `rollback_failed`, `committed`.
- `target`: the version string from `--target`.
- `snapshot`: reused (already present, repurposed for upgrade snapshot name).
- `deploy_id`: a UUID per `prepare` invocation, used as the directory name under `<state_dir>/<vmid>/`.

Backward-compatible: the existing watchdog flow does not write `phase` and continues to use `rollback_done` as before. The upgrade flow writes both `rollback_done` and `phase` so a downgrade-back-to-old-mwan-binary scenario does not break the watchdog.

## 5. State machine

```
+----------+    prepare    +-----------+    execute    +----------+
|  empty   |-------------->| prepared  |-------------->| executed |
+----------+               +-----------+               +----+-----+
                                |                           |
                                |                           |
                                | execute fail/hang         | validate
                                v                           v
                         +---------------+         +-------------------+
                         | execute_failed|         |    validated      |
                         |  execute_hung |         +-------------------+
                         +-------+-------+                 |
                                 |                         | commit
                                 |                         v
                                 |                  +-------------+
                                 |                  |  committed  |
                                 |                  +-------------+
                                 |
                                 |               (validate)
                                 v                         v
                         +---------------+         +-------------------+
                         | validate_     |         | validated_partial |
                         | failed        |         | (manual decision) |
                         +-------+-------+         +-------------------+
                                 |
                                 | rollback
                                 v
                         +-------------+   re-validate ok    +-------------+
                         | rolled_back |-------------------->|  committed  |
                         +-------+-----+                     +-------------+
                                 |
                                 | re-validate fail
                                 v
                         +-----------------+
                         | rollback_failed |
                         | (alert, manual) |
                         +-----------------+
```

Notes on transitions:

- `validated_partial` is the explicit "manual intervention required" state the spec calls for. Neither `commit` nor `rollback` runs from this state without an explicit operator flag (`--from validated_partial` for commit, `--force` for rollback).
- Every transition except `commit` and `validated_partial` is reversible via `rollback`. `commit` is the point of no return for that snapshot.
- `rollback_failed` is the alarm state. It does not auto-retry. The operator decides whether to retry the rollback, take a different snapshot, or drop to manual recovery (Proxmox web UI, OPNsense console, or restoring `config.xml.pre` to a freshly rebuilt VM).

## 6. Failure mode handling

| Failure | Phase reached | Auto action | Human action |
| --- | --- | --- | --- |
| Snapshot creation fails | `prepare` aborts before writing state | None. Subcommand exits non-zero. | Inspect Proxmox storage health. Re-run `prepare` once cleared. |
| Upgrade execution fails | `execute_failed` | None. State recorded. | Run `validate` to assess; run `rollback` if validate confirms damage. |
| Upgrade execution hangs | `execute_hung` after watchdog timeout | Auto-rollback in `run` mode. Manual in single-step mode. | Review `upgrade.log`. Decide whether to file a bug against OPNsense 26.x. |
| Validate partial pass | `validate_failed` (or `validated_partial` with `--accept-partial`) | None. | Operator inspects each failing check. Either accept-partial (rare) or rollback. |
| Validate full fail | `validate_failed` | Auto-rollback in `run` mode. Manual in single-step mode. | Confirm rollback succeeded. |
| Rollback itself fails | `rollback_failed` | None. Loud alert via `notify` at `Error`. | Operator drops to OOB access (`root@3d06:bad:b01:ff::1` per `project_oob_access.md`) and recovers manually. |

The "loud alert" path uses `notify.Manager.Notify` with kind `opnsense_upgrade`, key `rollback_failed`. The MWAN-132 unification means the same code path that emails for watchdog failover events emails for upgrade rollback failures, with the existing repeat-cadence and state-change suppression.

For `rollback_failed`, the alert payload includes:

- The deploy ID and target version.
- The snapshot name that the rollback aimed at.
- The output of `qm listsnapshot <vmid>` at the time of failure.
- The log path under `<state_dir>` so the operator has a fixed location to grep.

## 7. Integration with existing mwan internals

| Concern | Existing package | Use |
| --- | --- | --- |
| Proxmox snapshot lifecycle (`qm snapshot`, `qm rollback`, `qm listsnapshot`, `qm delsnapshot`) | `internal/ops` (`SysOps` interface, `RealOps` impl) | Direct call. No new client code. |
| Snapshot helpers (`SnapshotsAfter`, `ExtractLatestSnapshot`, regex sets) | `internal/rollback` | Reused. Add a third regex `UpgradeSnapRE` matching `pre-upgrade-26x-*`. |
| State file (`rollback.WriteState`, `rollback.AlreadyDone`, parser) | `internal/rollback` | Extended with `phase`, `target`, `deploy_id`. Existing parser preserves backward compat. |
| Email and state-change tracking | `internal/notify` | All `opnsense_upgrade`/* events route through `notify.Notifier`. Suppression and repeat cadence are handled there per MWAN-132. |
| In-guest commands during `execute` | `pkg/pveapi.GuestExec` | Primary path because the upgrade is likely to interrupt the mwan-opnsense daemon and break the vsock channel. |
| In-guest commands during `validate` and `prepare` | `internal/opnsense.RPC` (typed mwan-opnsense client) | `Version`, `ReadConfigXML`, `BackupConfigXML`, `Exec`. |
| In-guest commands as a fallback when the daemon is down | `pkg/pveapi.GuestExec` | Validate the guest came back from rollback before trying the typed RPC. |
| Subcommand registration | `cmd/mwan/main.go` | Add `subcmdOPNsenseUpgrade subcommand = "opnsense-upgrade"` and a dispatch case. |

The subcommand needs no new transport. Every wire-level call (Proxmox API, QGA, mwan-opnsense RPC) already has a client. The new code is composition.

## 8. Test plan

Three layers, mirroring the section 5 layout in `MWAN-140-config-xml-transform-spec.md`.

### 8.1 Unit tests

Per-phase logic with `SysOps` interface fakes (already used by watchdog tests). Each phase function takes `SysOps`, `notify.Notifier`, `rpc.RPC`-shaped interface, and a `state_dir`. The tests stub each dependency and assert:

- `prepare` writes the expected state and snapshot name.
- `execute` returns the expected phase based on stubbed exit codes and timeouts.
- `validate` aggregates check results correctly, including partial-pass.
- `rollback` deletes children before rolling back, calls `VMRollback` with the right snapshot, and re-runs `validate`.
- `commit` deletes the right snapshot and refuses bad phases.

### 8.2 Integration test on suburban testbed

The first end-to-end run lives on suburban VM 101 (the OPNsense testbed). Steps:

- Pin VM 101 to a known-good 25.7 baseline using a fresh snapshot named `pre-mwan152-test`.
- Run `mwan opnsense-upgrade run --vmid 101 --target 26.7 --unattended` (or `--target` set to whichever target the operator wants to exercise; the test does not need a real 26.x release. A dummy target that the upgrade tool will reject is enough to exercise `execute_failed` and `rollback`).
- Verify `phase=rolled_back` and that the post-rollback `validate` passes.
- Verify the state file contents match the expected lifecycle: `prepared` -> `executed`/`execute_failed` -> `validate_failed` -> `rolled_back`.
- Repeat with a stubbed-success target to exercise the `validated` -> `committed` path. Either modify the upgrade tool args to a no-op (e.g. `opnsense-upgrade -c` for "check only"), or add a `--dry-run-execute` flag at the subcommand level. Open question 9.4.

A throwaway VM dedicated to upgrade-rehearsal (suggested as VM 102 in the handoff brief, contingent on MWAN-149) would let this run without touching the testbed router VM 101 that other slices depend on. Open question 9.5 covers whether the design should mandate a dedicated rehearsal VM or whether sharing VM 101 is acceptable given the snapshot guarantees.

### 8.3 Pre-prod gate

Before the prod run on vault VM 101:

- The integration test on suburban must pass for both the success and failure paths.
- The runbook `opnsense-testbed-config-import.md` section 6 every-change gate must pass on the testbed VM 101 after a full upgrade-rollback cycle, proving that the post-rollback state is byte-equal to the pre-upgrade `config.xml.pre` and that no pf rule, gateway, or DHCP scope drifted.
- The MWAN-130 BGP graceful-restart timing must accommodate the snapshot pause measured on suburban. If the pause is longer than the GR window, defer the prod run pending GR-window tuning or the ZFS snapshot path (section 2.2).

## 9. Open questions

Listed numbered so the follow-up ticket can answer them in order.

1. **Snapshot pause budget.** Measure `qm snapshot --vmstate 1` pause duration on suburban VM 101 with realistic memory pressure. Compare against the MWAN-130 BGP graceful-restart window. If the pause exceeds the GR window, the design should fall back to ZFS snapshots (section 2.2) or skip `--vmstate`.
2. **Boot environments on OPNsense 26.x.** Does the 26.x release ship `bectl`/`opnsense-bootenv` parity with 25.7? If yes, expose an opt-in `--use-boot-environment` flag that captures a BE alongside the Proxmox snapshot for defense in depth. If no, document the gap.
3. **Upgrade execution channel.** Use Proxmox QGA `GuestExec` (primary) or mwan-opnsense `Exec` RPC (alternative)? The design recommends QGA to avoid the vsock-channel-dies-mid-upgrade hazard, but the operator may have a reason to prefer the typed RPC.
4. **Dry-run execute mode.** Should the subcommand expose a `--dry-run-execute` flag that runs `opnsense-upgrade -c` (check-only) instead of the real upgrade? It would let `run` exercise the full state machine without committing the upgrade.
5. **Dedicated rehearsal VM.** MWAN-149 proposes a VM 102 on suburban for upgrade rehearsals. Should `mwan opnsense-upgrade` mandate a dedicated VM in its `--vmid` validation, or allow any VM that QGA sees?
6. **State directory ownership.** `<state_dir>` defaults to `/var/lib/mwan/upgrade/`. Confirm this fits the systemd unit file conventions in `cmd/mwan/mwan-agent.service` and friends, and that the unit's `StateDirectory=` covers it.
7. **Notify kind name.** The design uses `opnsense_upgrade` as the alert kind. Confirm this does not collide with any kind already registered in `internal/notify/` after MWAN-132.
8. **Snapshot retention.** Does `commit` always delete the pre-upgrade snapshot, or does the operator want a "keep the last N upgrade snapshots" policy? The watchdog-managed snapshots (`pre-deploy-*`, `known-good-*`) already have retention rules in `internal/rollback/state.go`; the upgrade snapshot prefix (`pre-upgrade-26x-*`) currently has none.
9. **Vault prod VM 101 disk backend.** What storage backend backs the prod VM 101 disk on vault? The answer determines `qm snapshot` cost and whether the ZFS path in section 2.2 is even available.

## 10. Follow-up implementation ticket suggestion

Title: `MWAN-152 slice 1: implement mwan opnsense-upgrade subcommand (Go)`

Scope:

- Add `subcmdOPNsenseUpgrade` to `cmd/mwan/main.go` and the dispatch case.
- Add `cmd/mwan/opnsense_upgrade.go` with the flag parsing for `prepare`, `execute`, `validate`, `rollback`, `commit`, `run`.
- Add `internal/opnsense/upgrade/` package with the per-phase functions, the state file extensions, and the `UpgradeSnapRE` regex addition to `internal/rollback/`.
- Reuse `internal/ops`, `internal/notify`, `internal/rollback`, `internal/opnsense` (RPC client), and `pkg/pveapi`.
- Ship unit tests per section 8.1.
- Ship a one-shot integration script under `mwan/scripts/` that drives the suburban VM 101 end-to-end test in section 8.2.
- Update `mwan/docs/runbooks/opnsense-testbed-config-import.md` to cross-reference the new subcommand once it lands.
- Out of scope: running the upgrade on prod vault VM 101. That step is gated on the section 8.3 pre-prod gate and on the open-question answers in section 9.

Acceptance: unit tests pass, integration test passes on suburban VM 101 in both success and failure paths, `make check` is green, runbook cross-reference lands.
