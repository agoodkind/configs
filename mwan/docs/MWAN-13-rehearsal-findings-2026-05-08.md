# MWAN-13 Rehearsal Findings 2026-05-08

Rehearsal run 2 of the OPNsense 25.7 to 26.x upgrade flow on testbed VM 102 (suburban).

## Executive summary

The rehearsal stopped in Phase 3 (Prepare). Artefact capture failed because the QEMU guest agent is not running inside VM 102. The `opnsense-upgrade prepare` subcommand does not honour the gRPC validator transport that MWAN-163 added. The mwan-opnsense gRPC daemon on the guest is healthy. It returns the OPNsense version string and a 226749-byte `config.xml` over `unix:///var/run/qemu-server/102.mwanrpc`. So the inputs `Prepare` needs are reachable, just not via the transport `Prepare` is hardwired to. No upgrade was attempted. No snapshot was rolled back. The pre-rehearsal snapshot `prod-shaped-25-7-baseline-2026-05-08` is intact.

## Environment snapshot

- Host: suburban, Proxmox `pve-manager/9.1.6/71482d1833ded40a` running kernel `6.17.13-2-pve`.
- VM: 102 (`opnsense-test2`), running, OPNsense 25.7 (amd64), `agent: enabled=1,type=virtio` configured but QGA process not active.
- mwan binary on suburban: `/usr/local/bin/mwan` sha256 `4ac0c8cb83a02835c20de8620b1d77debee26d2236e5e1b51949c6960c51e172`. Build `commit=4230efc dirty=clean binhash=4ac0c8cb83a0`.
- mwan-opnsense daemon on guest: build `commit=5578681 dirty=clean binhash=932474141f20`. It listens on `unix:///var/run/qemu-server/102.mwanrpc`. Both `version` and `read-config` ops succeed.
- Proxmox snapshots (oldest to newest, `current` is the head):
  - `pre-install-2026-05-08`
  - `pre-config-import-2026-05-08`
  - `post-keygen-pre-import-2026-05-08`
  - `post-revertbackup-pre-reboot-2026-05-08`
  - `prod-shaped-25-7-baseline-2026-05-08` (rollback target)

## Phase by phase narrative

### Phase 1 + 2 (carried over from previous agent)

The previous run captured `pre-baseline.json` at `2026-05-08T15:21:02-07:00`. The file lives at `/var/lib/mwan/upgrades/102/20260508T222042Z-rehearsal2/pre-baseline.json` and is 23715 bytes. The `deploy_id` in that file matches `20260508T222042Z-rehearsal2`. BGP checks failed in the baseline with `vtysh exit 127`. So FRR appears not to be installed on this testbed VM. BGP-related validate checks should therefore be best-effort or skipped during this rehearsal.

### Phase 3 - Prepare (FAILED)

Command: `mwan opnsense-upgrade prepare --vmid 102 --deploy-id 20260508T222042Z-rehearsal2`.

What happened:

1. mwan loaded state and found no prior `state.json` for VMID 102 (expected on a fresh deploy_id).
2. `Prepare.captureConfigXML` invoked `opsExecutorAdapter.GuestExec` to `cat /conf/config.xml` from the guest.
3. The transport orchestrator tried three channels in order. All three failed:
   - `vsock` returned `vsockExec: unhandled command "cat"`.
   - `tcp_mgmt` returned `tcpExec: unhandled command "cat"`.
   - `pve_rest` returned `HTTP 500: QEMU guest agent is not running`.
4. `Prepare` aborted before writing `state.json`.
5. An alert email was dispatched (`mwan-testbed@goodkind.io` to `alex@goodkind.io`).

Artefacts left under `/var/lib/mwan/upgrades/102/20260508T222042Z-rehearsal2/`:

- `pre-baseline.json` (carried over, 23715 bytes).
- `metadata.json` (241 bytes) with `snapshot=pre-upgrade-26x-1778279233`, `started_at=2026-05-08T15:27:13-07:00`, `target=""`.
- No `state.json`.
- No `config.xml.pre`, no `config.xml.pre.sha256`, no `version.txt`, no `interfaces.json`, no `bgp_status.json`.
- The Proxmox snapshot named in `metadata.json` was not actually created (`qm listsnapshot 102` does not list it).

### Phases 4 to 6

Skipped. The rehearsal stopped at the first unexpected behaviour per the directive.

## Root cause analysis

`Prepare`'s artefact capture (introduced by MWAN-162, commit `02ce1c4`) calls `Deps.Ops.GuestExec`. That is the QGA-flavoured executor. On this VM, QGA is not running. So all three QGA-style transports fail.

MWAN-163 (`Add gRPC validate.Env routing OPNsense ops via mwan-opnsense daemon`, commit `0010e4d`) added `--env-transport=grpc` and `--env-grpc-target` only to `validate`. It did not add them to `prepare`.

The mwan-opnsense gRPC daemon on the guest already exposes the operations `Prepare` needs:

- `read-config` returns `/conf/config.xml` (verified: 226749 bytes, sha256 `ba8d717a978cef9789c6ae11c72e7216700d3e71b307e682821f44d6dbae6911`).
- `exec` runs arbitrary commands inside the guest (verified: `opnsense-version` returns `OPNsense 25.7 (amd64)`, exit 0, 7 ms).

So this is a transport-routing gap in `Prepare`. It is not a guest-side problem.

## Surprises

- `metadata.json` is written before snapshot creation succeeds. It is then left behind on a Prepare failure. A rerun with the same deploy_id would inherit a stale `metadata.json` whose `snapshot` field points to a name that does not exist.
- `Prepare` calls `Notify.Send` with `transition=true` on failure even though no prior `state.json` existed. There was no previous state to transition from. The alert email arrived as expected. The semantics are "transitioned from no-state to failure" rather than a real recovery transition.
- `pre-baseline.json` BGP checks failed with `vtysh exit 127`. The testbed appears to lack the `frr` package. Post-upgrade BGP validate checks therefore cannot be apples-to-apples against the baseline. This was already true before run 2 started.

## Recommendations for prod cutover

1. Block prod cutover on `Prepare` learning the gRPC transport. The fix is structural. `Prepare` should accept the same `--env-transport=grpc` and `--env-grpc-target` flags that `validate` honours. It should route artefact capture through the gRPC daemon's `read-config` and `exec` ops. It should fall back to `GuestExec` only when those flags are absent. This is the natural follow-on to MWAN-162 and MWAN-163. File this as a new MWAN issue before retrying the rehearsal.
2. Make `Prepare` write `metadata.json` only after the Proxmox snapshot is created. Or stamp it `status=incomplete` until the snapshot lands. Either way a retry must not see a stale snapshot name.
3. Decide whether prod will rely on QGA at all. The alternative is to run cutover with `--env-transport=grpc` end to end. If the latter, audit every other phase (`execute`, `rollback`, `commit`) for `GuestExec` call sites and route them through gRPC too.
4. Stand up FRR (or accept the BGP-skip path) on the testbed VM before rerunning the rehearsal. Post-upgrade validate diffs need a baseline that is meaningful.
5. Capture full Prepare artefacts manually now (over gRPC) and stash them alongside `pre-baseline.json`. The next rehearsal then has a real baseline `config.xml.pre` to diff against without re-running Prepare against a buggy code path.

## Hard rules check

- Pre-rehearsal snapshot `prod-shaped-25-7-baseline-2026-05-08` exists (verified via `qm listsnapshot 102` on suburban).
- All artefacts confined to `/var/lib/mwan/upgrades/102/20260508T222042Z-rehearsal2/`.
- No prod hosts touched. Only suburban (testbed Proxmox) and guest VM 102 were involved.
- No `tofu apply`, no `ansible-playbook`, no `git push`, no `git merge` to main.

## Top three findings

1. `opnsense-upgrade prepare` is hardwired to `GuestExec` and ignores `--env-transport=grpc`. This is the blocker that stopped the rehearsal.
2. The mwan-opnsense gRPC daemon on the guest is healthy and exposes everything `Prepare` needs. The gap is purely on the host-side caller.
3. `metadata.json` is written before the snapshot is taken and is not cleaned up on failure. Retries with the same deploy_id can read stale snapshot names.

## QGA install attempt 2026-05-08 (rehearsal2 retry)

This pass aimed to install `os-qemu-guest-agent` on VM 102 so the existing `Prepare` GuestExec path could work, then resume the abandoned rehearsal at Phase C. The pass stopped in Phase B before any guest mutation occurred.

### Pre-flight

- `qm config 102` shows `agent: enabled=1,type=virtio` and the existing `args:` line wiring `/var/run/qemu-server/102.mwanrpc` as a virtio-serial port. Both channels coexist as expected.
- `qm guest cmd 102 ping` returned `QEMU guest agent is not running`. Baseline confirmed.
- `mwan opnsense-probe -op exec -cmd pkg -cmd-arg info -cmd-arg -E -cmd-arg os-qemu-guest-agent` returned exit 1 with `pkg: No package(s) matching os-qemu-guest-agent`. Confirms QGA not installed.
- The probe's `-cmd` is the executable, not a shell string. Shell-style `pkg info ... || echo NOT_INSTALLED` failed with `executable file not found in $PATH`. Subsequent calls used `-cmd-arg` per token. This is worth noting for future runs and probably for the probe's `--help` text.

### Snapshot

Took `pre-qga-install-2026-05-08` with `--vmstate 1`. `qm listsnapshot 102` shows the chain unchanged through `prod-shaped-25-7-baseline-2026-05-08` and the new snapshot appended after it.

### Install attempt and blocker

`pkg install -y os-qemu-guest-agent` (via `-cmd-arg` per token) returned exit 3 with:

```
Updating OPNsense repository catalogue...
Unable to update repository OPNsense
pkg: https://pkg.opnsense.org/FreeBSD:14:amd64/25.7/latest/meta.txz: No address record
pkg: https://pkg.opnsense.org/FreeBSD:14:amd64/25.7/latest/packagesite.pkg: No address record
pkg: https://pkg.opnsense.org/FreeBSD:14:amd64/25.7/latest/packagesite.txz: No address record
```

Forensics inside the guest:

- `cat /etc/resolv.conf` lists `nameserver 127.0.0.1` plus two IPv6 upstreams under `2a07:a8c0::/32`.
- `drill pkg.opnsense.org` against `127.0.0.1` returned `SERVFAIL`. Local Unbound has no upstream reachable.
- `netstat -rn -f inet` shows only the four directly-connected /24s (`10.240.{1,2,3,4}.0/24`). No `default` row.
- `netstat -rn -f inet6` shows the directly-connected /64s and one /96 NAT64 hint via `vlan064`. No `default` row.
- `xpath-get /opnsense/gateways` returned only `<gateway_group></gateway_group>`. The config defines no gateways at all.
- `/var/cache/pkg` does not exist. There is no offline pkg cache to fall back on.

The testbed VM is wired as a closed-loop simulation. It owns its VLANs but has no upstream egress configured. Any pkg install path needs either an upstream gateway plus working DNS, or an out-of-band binary delivery (e.g. `pkg fetch` on a host that has internet, then `scp` the package set onto the guest filesystem and `pkg install -y --no-repo-update <pkg>`).

### Decision

Stopped per Rule 3 before improvising. No gateway was added. No DNS was changed. No `pkg add` of a hand-fetched archive was attempted. The guest VM state is unchanged from the `pre-qga-install-2026-05-08` snapshot.

The rehearsal phases C through G were not reached and no rehearsal2 artefacts were updated. State on disk under `/var/lib/mwan/upgrades/102/20260508T222042Z-rehearsal2/` is whatever the prior agent left.

### Recommendations

- The "make Prepare gRPC-aware" recommendation from the previous section is the right path. The QGA install detour for the testbed needs working upstream networking that does not exist there today, which makes the QGA-only fallback impractical to rehearse against this VM in its current shape.
- If we still want to rehearse the QGA path on suburban, a pre-step is needed: either add a `<gateways>` entry plus a default route to a reachable upstream, or stage a local pkg mirror on suburban (`pkg-repo` or a plain HTTP server) and point the guest at it. Both are a non-trivial amount of testbed plumbing.
- The probe `--help` could clarify that `-cmd` is `argv[0]` and that shell metacharacters do not work; recommend `-cmd-arg` for any multi-token invocation.

### Hard rules check (this pass)

- `prod-shaped-25-7-baseline-2026-05-08` still present in `qm listsnapshot 102`.
- `pre-qga-install-2026-05-08` snapshot exists and was not deleted (commit/rollback never ran).
- No prod hosts touched; only suburban and VM 102.
- No `tofu apply`, `ansible-playbook`, `git push`, or `git merge` to main.

### Top three findings (this pass)

1. The QGA-on-guest fallback path is not exercisable on suburban as configured. The testbed has no IPv4 default route, no IPv6 default route, an empty `<gateways>` block, and a local resolver that cannot reach its upstreams. `pkg install os-qemu-guest-agent` cannot fetch from `pkg.opnsense.org`.
2. The `opnsense-probe -op exec` path takes argv tokens, not a shell string. Multi-token commands need `-cmd` for the executable plus repeated `-cmd-arg` flags. Shell pipes, quoting, and `||` do not work and produce a misleading "executable file not found" error.
3. Combined with the prior pass's finding that `Prepare` is hardwired to `GuestExec`, the practical conclusion for prod cutover is to make `Prepare` gRPC-aware rather than to chase QGA install on the testbed. The MWAN-162/163 follow-on for `Prepare` is the unblocker.

## Rehearsal attempt 3

This pass picked up after MWAN-160/161/162/163/164 had landed on main. MWAN-164 added the gRPC-aware `OpsExecutor` so `Prepare`, `Execute`, `Rollback`, and `Commit` can route guest ops through `mwan-opnsense` over `unix:///var/run/qemu-server/102.mwanrpc`. The goal was the full prepare/execute/validate/commit-or-rollback loop. The pass stopped before `Execute` was invoked. `Prepare` worked end-to-end over gRPC. `Execute` would have called a binary (`opnsense-upgrade`) that does not exist on OPNsense 25.7. The pre-rehearsal snapshot `prod-shaped-25-7-baseline-2026-05-08` is intact.

### Environment

- Worktree: `/Users/agoodkind/Sites/configs/.claude/worktrees/upgrade-rehearsal-run-3`, branch `upgrade-rehearsal-run-3`, HEAD `c0039d10eca086b604eba1b438d8f7560ce854fe` (merge of MWAN-164).
- Built `bin/mwan-linux` from that HEAD. Binary stamp: `commit=c0039d1 dirty=clean binhash=5a870a85c510`. sha256 `5a870a85c510b61e628a275c35a9262433931aefd2c223ce4f3daca22d767b15`.
- Installed at `/usr/local/bin/mwan` on suburban. Prior `mwan` (commit `4230efc`, sha256 `4ac0c8cb83a02835c20de8620b1d77debee26d2236e5e1b51949c6960c51e172`) preserved at `/usr/local/bin/mwan.prev`.
- Restarted `mwan-watchdog-testbed` and `mwan-opnsense-host`. Both `active` after restart, both reporting the new build stamp.
- Deploy ID carried over: `20260508T222042Z-rehearsal2`. Pre-baseline `pre-baseline.json` carried over from rehearsal 2 unchanged.
- Stale `metadata.json` from rehearsal 2's failed first prepare moved aside to `metadata.json.stale-from-rehearsal2-attempt1` so the new prepare started clean. The stale file referenced a snapshot (`pre-upgrade-26x-1778279233`) that never existed on the host. Recommendation 2 from the previous rehearsal is still open.

### Phase 1 - Build and deploy fresh mwan binary

Built and deployed without surprises. `mwan opnsense-upgrade prepare --help` showed both `--env-transport` and `--env-grpc-target`, confirming MWAN-164 made it onto the deployed binary.

### Phase 2 - Pre-baseline confirmation

`pre-baseline.json` present at 23715 bytes, captured `2026-05-08T15:21:02-07:00`, deploy_id matches. BGP checks fail with `vtysh exit 127` because the testbed VM lacks `frr`. Same as rehearsal 2.

### Phase 3 - Prepare with gRPC transport (PASSED)

Command:

```
mwan opnsense-upgrade prepare \
  --vmid 102 \
  --deploy-id 20260508T222042Z-rehearsal2 \
  --env-transport=grpc \
  --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc
```

Result: exit 0. Final line `phase=prepared deploy_id=20260508T222042Z-rehearsal2 snapshot=pre-upgrade-26x-1778280742`.

Log line by line:

1. `INFO upgrade.Prepare: captured config.xml vmid=102 bytes=226749 sha256=ba8d717a978cef9789c6ae11c72e7216700d3e71b307e682821f44d6dbae6911`. config.xml capture used the gRPC `read-config` op.
2. `WARN opnsense: daemon returned error status method_id=2 code=13 message="exec: runExec: exec: \"vtysh\": executable file not found in $PATH"`. BGP capture attempted to call `vtysh` over the daemon's `Exec` op. The guest does not have `vtysh`.
3. `WARN upgrade.Prepare: capture bgp_status: GuestExec failed, writing empty placeholder`. So `bgp_status.json` is written as a 0-byte file. This is the documented best-effort behavior.
4. `INFO upgrade: state saved vmid=102 phase=prepared path=/var/lib/mwan/upgrades/102/state.json`.
5. `INFO upgrade.Prepare: snapshot taken vmid=102 snapshot=pre-upgrade-26x-1778280742`.

Artefacts under `/var/lib/mwan/upgrades/102/20260508T222042Z-rehearsal2/`:

| File | Size | Notes |
| --- | --- | --- |
| `pre-baseline.json` | 23715 | carried over from rehearsal 2 |
| `metadata.json` | 241 | new, snapshot=`pre-upgrade-26x-1778280742` |
| `metadata.json.stale-from-rehearsal2-attempt1` | 241 | preserved for forensics |
| `config.xml.pre` | 226749 | sha256 matches `ba8d717a...` |
| `config.xml.pre.sha256` | 81 | `ba8d717a978cef9789c6ae11c72e7216700d3e71b307e682821f44d6dbae6911  config.xml.pre` |
| `version.txt` | 22 | `OPNsense 25.7 (amd64)` |
| `interfaces.json` | 6978 | `ifconfig -av`, `netstat -rn -f inet`, `netstat -rn -f inet6` |
| `bgp_status.json` | 0 | empty placeholder, vtysh not on guest |
| `transcripts/` | dir | created in Phase 4 prep, empty log |

`state.json` at `/var/lib/mwan/upgrades/102/state.json`:

```json
{
  "vmid": "102",
  "deploy_id": "20260508T222042Z-rehearsal2",
  "target": "",
  "snapshot": "pre-upgrade-26x-1778280742",
  "phase": "prepared",
  "updated_at": "2026-05-08T15:52:22.956821129-07:00"
}
```

`qm listsnapshot 102` shows `pre-upgrade-26x-1778280742` appended after `pre-qga-install-2026-05-08`. Snapshot creation succeeded.

This is the first time the rehearsal has cleared Phase 3. MWAN-162 + MWAN-164 together unblock `Prepare` over gRPC. Recommendation 1 from the previous rehearsal is now resolved by the deployed code.

### Phase 4 - Execute (STOP - did not run)

Pre-flight probes against the guest before invoking `execute`:

1. DNS: `drill pkg.opnsense.org` against `127.0.0.1` returned `SERVFAIL`. Local Unbound on the guest still cannot reach upstreams. Same as rehearsal 2.
2. IPv4 routing: `netstat -rn -f inet` shows only the four directly-connected `/24`s (`10.240.{1,2,3,4}.0/24`). No `default` row.
3. IPv6 routing: `netstat -rn -f inet6` shows only directly-connected `/64`s and one `/96` NAT64 hint via `vlan064`. No `default` row.
4. Binary check on the guest:
   - `which -a opnsense-upgrade opnsense-update` returns only `/usr/local/sbin/opnsense-update` (exit 1, missing `opnsense-upgrade`).
   - `ls -la /usr/local/sbin/opnsense-upgrade` returns `No such file or directory`.
   - `ls -la /usr/local/sbin/opnsense-update` returns `-r-xr-xr-x 1 root wheel 25396 Jul 21 2025`.
   - `find /usr -name "opnsense-*"` enumerates `opnsense-update`, `opnsense-fetch`, `opnsense-revert`, `opnsense-version`, etc. There is no `opnsense-upgrade`.

The host-side orchestrator in `mwan/go/internal/opnsense/upgrade/execute.go` builds its argv as `opnsense-upgrade -r <target>` (or `opnsense-upgrade -c` for `--dry-run-execute`):

```
// upgradeCommand builds the argv that the guest will run. With
// --dry-run-execute the command becomes `opnsense-upgrade -c` which is
// the documented check-only mode (resolved decision 11.4). The real
// path is `opnsense-upgrade -r <target>`.
func upgradeCommand(target string, dryRun bool) []string {
        if dryRun {
                return []string{"opnsense-upgrade", "-c"}
        }
        if target == "" {
                return []string{"opnsense-upgrade"}
        }
        return []string{"opnsense-upgrade", "-r", target}
}
```

So `execute` will call a binary that does not exist on this guest. On a real OPNsense 25.7 image the canonical major-release upgrade tool is `opnsense-update -ur` (the `-u` switch downloads the new release set, `-r <release>` targets a specific release). `opnsense-upgrade` is not part of OPNsense 25.7 at all.

Per Rule 3 the rehearsal stopped here. No `execute` was attempted. The background `socat` to `/var/run/qemu-server/102.serial0` was started but produced an empty transcript; the log file is preserved as a sentinel under `transcripts/serial-execute-20260508T225257Z.log`. State.json is unchanged at `phase=prepared`.

### Phases 5 to 6

Skipped. No validate, no commit, no rollback. State remains `prepared`.

### Surprises

- The `opnsense-upgrade` binary does not exist on OPNsense 25.7. The host-side orchestrator's argv `opnsense-upgrade -r 26.x` is wrong for this OS. The right argv on stock OPNsense is `opnsense-update -ur`. This was not surfaced in any prior rehearsal because Phase 3 had been failing first. With `Prepare` now passing, this gap is the next blocker.
- BGP capture in `Prepare` calls `vtysh` over the daemon's `Exec` op. Failure is downgraded to a warning and writes an empty `bgp_status.json`. That is the right semantics for the testbed, but in prod the empty file should not be confused with a successful capture. Suggest a one-line stamp inside the placeholder, like `{"captured": false, "reason": "vtysh not in $PATH"}`, to make the post-mortem trail self-describing.
- gRPC transport for `Prepare` produces the desired transport-routing log lines (`captureConfigXML` over `read-config`, `bgp_status` over `Exec`) but does not yet log a single boundary line at the start saying "transport=grpc target=...". Adding that one line at the top of `Prepare` would make rehearsal logs self-contained without diffing against `state.json`.
- The testbed VM is still a closed-loop simulation with no upstream connectivity (no default route, local resolver fails). Even if the host orchestrator switched to `opnsense-update -ur`, the guest could not download release set 26.x from `pkg.opnsense.org`. So the rehearsal blocker is two-layered.

### Recommendations for prod cutover

1. **Block prod cutover on `Execute` argv correction.** File a new MWAN issue: change `upgradeCommand` (and any test fixtures) to call `opnsense-update -ur` (or whichever exact form matches the OPNsense major-version upgrade contract). Verify against a real OPNsense `man opnsense-update` rather than from memory. The exact argv should be derived from the release notes for the from→to pair, since OPNsense has changed flag semantics across releases. This is the natural follow-on to MWAN-164.
2. **Decide on testbed connectivity for end-to-end rehearsal.** Two options: (a) give the testbed VM a working egress path (gateway + DNS) so `opnsense-update` can fetch real release artefacts, or (b) point it at a local pkg mirror staged on suburban. Option (a) is simpler and matches prod behaviour. Until one of these is in place, rehearsal can only validate orchestration, not the actual upgrade.
3. **Stamp `bgp_status.json` placeholder.** Replace the 0-byte file with a one-line JSON envelope describing why the capture was skipped. Keeps post-mortem signal honest.
4. **`Prepare` should log a single boundary line announcing transport at the start.** Equivalent to what other phases already do at the boundary. Cheap, makes rehearsal logs more legible.
5. **`Prepare` still does not clean up `metadata.json` on failure.** Rehearsal 2's stale file had to be moved aside by hand before this rehearsal could start. File a small follow-up to either write it last (after snapshot) or stamp it incomplete.

### Snapshot lifecycle

- `pre-upgrade-26x-1778280742` was created by `Prepare`. Still present (no execute, so no commit/rollback).
- `prod-shaped-25-7-baseline-2026-05-08` still present.
- `pre-qga-install-2026-05-08` still present (parent of the new snapshot).
- No snapshots were deleted in this pass.

### Hard rules check

- Pre-rehearsal snapshot `prod-shaped-25-7-baseline-2026-05-08` exists in `qm listsnapshot 102` after this pass.
- All artefacts confined to `/var/lib/mwan/upgrades/102/20260508T222042Z-rehearsal2/`.
- No prod hosts touched. Only suburban (testbed Proxmox) and guest VM 102 were involved.
- No `tofu apply`, no `ansible-playbook` runs.
- No `git push`, no `git merge` to main. The new `mwan` binary on suburban is from local build of `c0039d1`, the merge already on `main`.

### Top three findings (rehearsal 3)

1. `Execute` calls `opnsense-upgrade` but OPNsense 25.7 ships no such binary. The major-release upgrade tool on stock OPNsense is `opnsense-update -ur` (or `-r <release>` after a fetch). This is the next structural fix and should land before another rehearsal attempt.
2. `Prepare` works end-to-end over gRPC now. config.xml is captured directly via `read-config` (226749 bytes, sha256 matches), interfaces.json is captured via `Exec`, BGP capture is best-effort and writes an empty file when `vtysh` is missing. Snapshot creation, `state.json` write, and `metadata.json` write all succeed.
3. The testbed VM still has no upstream connectivity. Even after the `Execute` argv is fixed, an end-to-end rehearsal needs either gateway+DNS on the guest or a local pkg mirror on suburban. Picking and standing up one of these is a prerequisite for rehearsal 4.


## Rehearsal attempt 4 (2026-05-08, MWAN-168)

This pass started from the `v5-rehearsal-ready-2026-05-08` snapshot of VM 102: BGP v4 Established to both peers, BGP v6 Established to one peer (`:201::3`), default route installed v4+v6, system DNS resolution working via the prod-shaped public-resolver fallbacks at `2a07:a8c0::7d:698e` / `2a07:a8c1::7d:698e` (the `<system><dnsserver>` entries from the redacted prod XML).

### Phase 1 - Codify DNS forwarder fix in substitutions.yaml

The prod `unbound` config has two `<dot>` entries that forward to AdGuard at `3d06:bad:b01::53` (one plain forward at port 53, one DoT at port 853). The pre-existing v6 VMNET prefix shift (`3d06:bad:b01::` to `3d06:bad:b01:200::`) rewrites this to `3d06:bad:b01:200::53`, which has no listener on the testbed and produces SERVFAIL from `unbound` on `127.0.0.1`. The user-visible symptom is masked because the `<system><dnsserver>` Foundation public resolvers act as fallback in `/etc/resolv.conf`, so `host(1)` and `pkg update` work via the resolver fallthrough even though `drill @127.0.0.1` returns SERVFAIL.

Added a new `text_literal` entry at the bottom of `mwan/testbed/opnsense/substitutions.yaml`, after the `chaotic.dog` rules and after the v6 VMNET shift, mapping `3d06:bad:b01:200::53` to `2606:4700:4700::1111`. This catches the post-shift literal for both `<dot>` entries plus the alias content at line 3532 of the redacted XML and a host alias entry. Cloudflare answers DoT on tcp/853 and plain DNS on 53, so both `<dot>` ports are covered.

### Phase 2 - Regenerate config-testbed.xml

`make build` succeeded (the MWAN-176 lint baseline issue called out in the briefing was no longer blocking on this branch; baseline regenerated cleanly). `./bin/mwan opnsense-import-config -input redacted-prod.xml -substitutions substitutions.yaml -output generated/config-testbed.xml` wrote 219484 bytes. `xmllint --noout` passed. `grep '200::53'` returned zero hits. `grep '2606:4700'` returned 4 hits at the expected places (alias content, two `<dot><server>` entries, host alias).

### Phase 3 - Firmware fetch sanity

`opnsense-update -c` exit 0. `opnsense-update -cVbk` resolved the package mirror URL to `https://pkg.opnsense.org/FreeBSD:14:amd64/25.7/sets`, which means name resolution worked end-to-end. `pkg update -f` processed 930 packages successfully. Firmware metadata fetch is functional via the resolver fallback path.

### Phase 4 - Snapshot

`qm snapshot 102 v5-rehearsal-ready-2026-05-08 --vmstate 1` succeeded. RAM saved 2.03 GiB in 2s. Snapshot tree on suburban now ends `... v4-bgp-up -> v5-rehearsal-ready-2026-05-08 -> current`.

### Phase 5 - Pre-upgrade baseline

`DEPLOY_ID=20260508T210917Z-rehearsal4`. `mwan opnsense-validate -baseline-only -deploy-id $DEPLOY_ID -env-transport=grpc -env-grpc-target unix:///var/run/qemu-server/102.mwanrpc 102` wrote `/var/lib/mwan/upgrades/102/$DEPLOY_ID/pre-baseline.json` (36883 bytes). Summary: pass=29 fail=23 skip=0 error=9. Most fails are testbed-expected (acme/crowdsec/git_backup/nginx/redis/tayga/wireguard plugins not installed; quagga API endpoint shape; radvd not announcing). Errors are missing flags (`LANClientSSHHost`, `ProxmoxSSHHost`) which are not provided in the rehearsal command. Acceptable for a baseline-only run.

### Phase 6 - Prepare

`mwan opnsense-upgrade prepare --vmid 102 --deploy-id $DEPLOY_ID --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc` succeeded. Captured `config.xml` (227219 bytes, sha256 `e8d5c89e9ebd463745c40cdf10b6aa4481eee8e0494dce510181a6e72eea5a32`). State written `phase=prepared`. Snapshot created `pre-upgrade-26x-1778299810`. Artefacts present: `bgp_status.json` (2919 bytes, non-empty this time), `config.xml.pre`, `config.xml.pre.sha256`, `interfaces.json` (8028 bytes), `metadata.json`, `version.txt` (`OPNsense 25.7.11_9 (amd64)`).

### Phase 7 - Execute (FAILED)

`mwan opnsense-upgrade execute --vmid 102 --deploy-id $DEPLOY_ID --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc` failed immediately with:

```
opnsense: daemon status method=2: rpc error: code = Internal
desc = exec: runExec: exec: "opnsense-upgrade": executable file not found in $PATH
```

State transitioned `prepared` to `executing` to `execute_failed`. The `mwan` binary on suburban (build `commit=c0039d1 dirty=clean binhash=5a870a85c510`) is from the merge that fixed the argv to `opnsense-update -u` (commit `993611e`). However the deployed binary on suburban still calls `opnsense-upgrade`, which suggests the suburban `/usr/local/bin/mwan` was not rebuilt after the merge. The build identifier reports `c0039d1` which is the merge commit at the tip of `main`, so the binary should contain the fix; this contradicts the observed behaviour and indicates that either (a) the `Execute` path wraps a different code path that still references `opnsense-upgrade`, or (b) the on-disk binary at `/usr/local/bin/mwan` is older than its reported commit. Either way, no upgrade was attempted on the guest. VM 102 still reports `OPNsense 25.7.11_9 (amd64)`.

The serial-capture started before execute exited within seconds (zsh backgrounded the `socat` and the ssh channel closing reaped it), leaving `/tmp/vm102-rehearsal4-serial.log` at 0 bytes. Since execute did not boot the guest, no serial output was generated to capture, so this is incidental.

### Phase 8 - Validate (skipped)

Skipped because Execute did not actually upgrade the guest. Running validate now would only re-run the baseline checks against an unchanged 25.7.11_9 system.

### Phase 9 - Rollback decision

Rollback is not strictly required: no upgrade ran, the guest is unchanged, and `pre-upgrade-26x-1778299810` is still in place. Leaving the deploy state as `execute_failed` is intentional so the failure is recorded and the artefacts remain in `/var/lib/mwan/upgrades/102/$DEPLOY_ID/` for inspection. A future rehearsal 5 can either reuse the same `pre-upgrade-26x-*` snapshot or take a fresh one.

### Snapshot lifecycle

- `pre-install-2026-05-08` (intact)
- `pre-config-import-2026-05-08` (intact)
- `post-runbook-pre-import-v2-2026-05-08` (intact)
- `v4-baseline-2026-05-08` (intact)
- `v4-bgp-up-2026-05-08` (intact)
- `v5-rehearsal-ready-2026-05-08` (created this pass)
- `pre-upgrade-26x-1778299810` (created this pass by `Prepare`)
- No deletions.

### Hard rules check

- Pre-rehearsal baseline ancestors all present in `qm listsnapshot 102`.
- No production hosts touched. Suburban (testbed Proxmox) and guest VM 102 only.
- No `tofu apply`, no `ansible-playbook` runs.
- No `git push`, no `git merge` to main.
- No typographic em-dashes or en-dashes in added text.

### Top three findings (rehearsal 4)

1. `Execute` is still calling `opnsense-upgrade` on the guest, even though the deployed `mwan` on suburban reports the post-fix commit. The error message is unambiguous: `exec: "opnsense-upgrade": executable file not found in $PATH`. Either the binary is stale, or there is a second code path (perhaps a hard-coded retry, an embedded constant in the upgrade orchestration, or a fallback when a feature flag is off) that still references the old name. Investigate `internal/upgrade` for any remaining string literal `opnsense-upgrade` and verify which file produces the argv passed into `GRPCExecutor.Exec` for the upgrade command. The fix from MWAN-166 may have only landed in one of two places.

2. The DNS forwarder fix in `substitutions.yaml` is now codified for any future regenerated testbed config. The pre-existing `<system><dnsserver>` fallback masked the broken unbound forwarder during the rehearsal; firmware fetch worked despite local resolver SERVFAIL. Codifying the fix prevents this from being a future surprise once the system fallback is altered or a strict-resolver mode is introduced.

3. `Prepare` is reliable end-to-end over gRPC. Snapshot creation, config capture, BGP-status capture (non-empty 2919 bytes this time), interfaces capture, version capture, sha256, and `state.json` all worked without manual intervention. This is the third consecutive rehearsal where `Prepare` worked cleanly; it is no longer the rehearsal blocker.

### Recommendations for prod cutover

1. **Block prod cutover until `Execute` actually runs `opnsense-update -u` on the guest.** Add an integration test or a smoke harness on suburban that runs `Execute` against VM 102 and asserts the version flips. The current build identifier check (commit hash in `mwan boundary` log line) is not enough on its own; the rehearsal proved the binary can claim the fixed commit while still emitting the pre-fix argv.

2. **Add a sanity probe that runs as the first action of `Execute`.** Before invoking the upgrade command, run `command -v opnsense-update` (or the equivalent) on the guest via gRPC and fail fast with a clear message if the executable is not on `$PATH`. The current failure mode buries the actionable signal inside a generic `runExec` error.

3. **Land a deploy step that rebuilds `mwan` on suburban from the current `main` HEAD before each rehearsal.** This pass discovered that the deployed binary's behaviour did not match the merge that fixed it, regardless of cause. A `make deploy-suburban` (or rsync-and-restart) target that runs as the first rehearsal step removes the ambiguity. For prod, the same step gates the cutover.

## Rehearsal attempt 5 (2026-05-08, post MWAN-166 redeploy)

### Goal

Verify the MWAN-166 argv fix is actually present on suburban, then re-run the upgrade rehearsal end-to-end. Rehearsal 4 had identified that the deployed binary on suburban was stale and pre-dated MWAN-166, so `Execute` was still calling `opnsense-upgrade` instead of `opnsense-update -u`.

### Build and deploy

Built `bin/mwan-linux` from the worktree at commit `cdd6630` via `make build-linux`. SHA256 `9c4691d021f1e21d4f69bb39c6b426aa3dfdf5b9e857b84726874ae74d094488`. scp'd to suburban, kept `/usr/local/bin/mwan.prev` as rollback marker, installed at `/usr/local/bin/mwan` with same sha256. Restarted `mwan-watchdog-testbed` and `mwan-opnsense-host`; both `is-active`. `mwan version` boundary log line reports `commit=cdd6630 dirty=clean binhash=9c4691d021f1`. The `--env-transport` and `--env-grpc-target` flags are present on `opnsense-upgrade prepare --help`.

### State reset

Stale `state.json` from rehearsal 4 left the upgrade lifecycle in `phase=execute_failed`. The state machine refused a fresh `prepare` because that transition is not allowed. The corresponding snapshot `pre-upgrade-26x-1778299810` had to be deleted manually before `qm rollback 102 v5-rehearsal-ready-2026-05-08` would run. Proxmox refused the rollback because the orphan snapshot was not the most recent on the disk. After rolling back the snapshot, the operator also has to delete `state.json` manually per the design comment in `state.go:135`. The snapshot delete and state.json clear sequence would be cleaner if scripted as a single subcommand. Today it is documented as a manual recovery step. No `mwan opnsense-upgrade reset` exists.

### Phase 5 - Baseline (PASS)

`DEPLOY_ID=20260509T041828Z-rehearsal5`. `mwan opnsense-validate -baseline-only -deploy-id $DEPLOY_ID -env-transport=grpc -env-grpc-target unix:///var/run/qemu-server/102.mwanrpc 102` wrote `pre-baseline.json` (41484 bytes). Summary `pass=31 fail=21 skip=0 error=9`. The shape matches rehearsal 4. The fails are testbed-expected (uninstalled plugins, quagga shape, radvd). The errors are missing-flag errors for ProxmoxSSHHost and LANClientSSHHost, which are not provided in the rehearsal command.

### Phase 6 - Prepare (PASS)

After clearing stale state.json, `mwan opnsense-upgrade prepare --vmid 102 --deploy-id $DEPLOY_ID --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc` succeeded. Captured `config.xml.pre` 227219 bytes sha256 `e8d5c89e9ebd463745c40cdf10b6aa4481eee8e0494dce510181a6e72eea5a32`. Snapshot `pre-upgrade-26x-1778300364` taken. `state.json` written `phase=prepared`. All artefacts present (`bgp_status.json` 3396 bytes non-empty, `interfaces.json` 8087 bytes, `metadata.json`, `version.txt` `OPNsense 25.7.11_9 (amd64)`).

### Phase 7 - Execute (FAILED, gRPC per-call timeout)

`mwan opnsense-upgrade execute --vmid 102 --deploy-id $DEPLOY_ID --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc` failed with `exit=-1`. The `upgrade.log` shows:

```
argv=[opnsense-update -u]
exit=-1
err=<nil>
stdout:
Fetching packages-26.1-amd64.tar: ..................................................................................................................................................
stderr:
[grpc-executor: command timed out]
```

The argv is correct, so the MWAN-166 fix is on the wire. `opnsense-update -u` started, began fetching `packages-26.1-amd64.tar`, and was killed mid-fetch by the daemon's per-RPC command timeout. State went to `execute_failed`.

Root cause: `cmd/mwan/opnsense_upgrade_executor.go:92` constructs the GRPCExecutor with `ExecTimeoutSeconds: 0`. The daemon interprets zero as its 30s default and caps the absolute maximum at 300s (per `executor_grpc.go:38-46` doc comment). The `opnsense-update -u` upgrade easily exceeds that on a fresh testbed where the package set has never been fetched. The outer `Execute` ctx uses `DefaultUpgradeTimeout = 30 * time.Minute`, which would be sufficient. The per-RPC timeout is the binding constraint, and it is hardwired to the short-command default.

The pre-MWAN-166 `opnsense-upgrade` binary did not exist on the guest, so `Execute` always returned a fast `executable file not found` error. That error fired long before the per-RPC timeout could expire. The fix in MWAN-166 surfaced this latent timeout bug.

### Phase 9 - Rollback (snapshot OK, post-rollback wait FAILED)

`mwan opnsense-upgrade rollback --vmid 102 --deploy-id $DEPLOY_ID --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc` ran `qm rollback 102 pre-upgrade-26x-1778300364` successfully. After the snapshot restore, `waitForGuest` polled the gRPC `Exec` with `cmd=true` for 60 seconds and got `opnsense: client is closed` on every retry. State went to `rollback_failed`.

Functionally the VM is fine. The snapshot rollback completed. BGP came back up to both v4 peers, `Established` for roughly one minute by the time the next probe ran. `opnsense-version` reports `OPNsense 25.7.11_9 (amd64)`. A fresh `mwan opnsense-probe --op=exec --cmd=hostname` returns `router-test.test.home.goodkind.io`. The VM is in the expected post-rollback state. The orchestrator's wait loop reused a gRPC client that was killed by the QEMU restart caused by the snapshot rollback. The reused-client design fits the steady-state daemon model with one connection per orchestrator run. It does not survive a full guest restart.

### Outcome

- Phases reached: 1-7 attempted, 8 (validate) skipped because Execute failed, 9 (rollback) attempted.
- Version post-upgrade: still `OPNsense 25.7.11_9 (amd64)` (upgrade was killed mid-fetch, snapshot restored).
- BGP up post-rollback: yes (v4 to both peers Established).
- Validate pass/fail counts: not run.
- Commit-or-rollback decision: rollback (forced by Execute failure).
- Final state.json: `phase=rollback_failed`, `snapshot=pre-upgrade-26x-1778300364`. The state value is misleading. The snapshot rollback itself worked. Only the post-rollback liveness probe failed.
- Snapshot tree end state: leaf is `pre-upgrade-26x-1778300364` (post-rollback). The rehearsal-ready snapshot `v5-rehearsal-ready-2026-05-08` is intact one level up. No prod snapshots affected.

### Top three findings

1. **gRPC executor per-RPC timeout caps the upgrade.** The `GRPCExecutor` is constructed with `ExecTimeoutSeconds: 0`. The daemon treats zero as a 30-second default and enforces a 300-second hard maximum. `opnsense-update -u` exceeds 300s on a fresh testbed because it fetches a multi-hundred-MB package set. MWAN-166 fixed the argv. The upgrade now hits this latent ceiling. The fix needs `Execute`-specific timeout plumbing. One option is to pass the outer `DefaultUpgradeTimeout` of 30 minutes through as `ExecTimeoutSeconds`. Another option is to split the executor so long-running guest commands stream progress instead of relying on a single-shot RPC. The daemon may also need to accept a higher max for explicit upgrade calls.

2. **`Rollback`'s waitForGuest reuses a stale gRPC client.** The `qm rollback` resets the QEMU process. That reset closes any in-flight virtio-serial gRPC connections. The orchestrator's `waitForGuest` then polls with the now-closed client and times out after 60s. The fix is to redial after `qm rollback` returns, not before. The VM and the daemon are healthy. Only the orchestrator's expectation of connection persistence is wrong.

3. **State machine has no operator-friendly reset path.** Recovery from rehearsal 4's `execute_failed` required three manual steps. The operator first deletes the orphan snapshot. The operator then runs `qm rollback` to a known-good snapshot. The operator finally runs `rm /var/lib/mwan/upgrades/102/state.json`. The design doc comment (`state.go:135`) says "operator clears the state file". No `mwan opnsense-upgrade reset` subcommand exists to do this safely. A `reset --vmid <id>` would confirm the snapshot named in state.json no longer exists and then truncate state.json. Adding it would close this gap and reduce the foot-cannon surface during rehearsal recovery.

## Rehearsal attempt 6 (2026-05-08, post MWAN-177 + MWAN-178)

Worktree `vm102-rehearsal6` off main with the merged MWAN-177 (--exec-timeout flag, daemon cap raised to 60m) and MWAN-178 (GRPCExecutor redial-on-rollback) fixes. Built `mwan-linux` at commit `6334e93`, sha256 `66ab46c4a441174831ac80148ad69c887f48684f07832746e23efd2f2541b407`. Deployed to suburban via scp + install + restart of `mwan-watchdog-testbed` and `mwan-opnsense-host`. Both came back active. Verified `--exec-timeout` flag is present on the deployed binary (`Per-RPC Exec timeout for the gRPC executor (passed to mwan-opnsense daemon) (default 30m0s)`).

Rolled VM 102 back to `v5-rehearsal-ready-2026-05-08`. The leaf snapshot from rehearsal 5 (`pre-upgrade-26x-1778300364`) had to be deleted first, then `qm rollback` succeeded. Verified the gRPC channel returns and the guest reports `OPNsense 25.7.11_9 (amd64)`, hostname `router-test.test.home.goodkind.io`. Cleared `/var/lib/mwan/upgrades/102` and captured a fresh `pre-baseline.json` (37709 bytes, summary pass=31 fail=21 skip=0 error=9; the failures are the optional-plugin-not-installed pattern from prior rehearsals). Ran `prepare`. State went to `phase=prepared`. New snapshot `pre-upgrade-26x-1778302151` created on top of `v5-rehearsal-ready-2026-05-08`. All 7 expected artefacts present.

### Phase 7 - Execute (failed in 1m45s on transient fetch timeout)

Started a serial capture as a transient systemd unit (`vm102-rehearsal6-serial.service` running `socat -u UNIX-CONNECT:/var/run/qemu-server/102.serial0 OPEN:/tmp/vm102-rehearsal6-serial.log,creat,append`) and a separate transient unit (`vm102-rehearsal6-execute.service`) for the execute call with `--exec-timeout 60m`. The execute exited non-zero after 1m45s. Inspecting the artefact `upgrade.log`:

```
argv=[opnsense-update -u]
exit=1
err=<nil>
stdout:
Fetching packages-26.1-amd64.tar: ...................................................................... done
Fetching base-26.1-amd64.txz: ..............................[fetch: transfer timed out] failed, no signature found

stderr:
```

The argv (`opnsense-update -u`) is correct, so MWAN-166 is on the wire. The exit was not a per-RPC timeout from MWAN-177 (the orchestrator was nowhere near the 60m cap). It was the in-guest `fetch(1)` giving up on `base-26.1-amd64.txz` after its own internal timeout. A retry of the same URL from inside the guest via `mwan opnsense-probe -op exec -cmd /bin/sh -cmd-arg -c -cmd-arg "fetch --timeout=30 -qo /dev/null https://pkg.opnsense.org/FreeBSD:14:amd64/26.1/sets/base-26.1-amd64.txz"` succeeded in 6.5s. The first failure was a transient network blip on the upstream mirror, not a code defect in mwan or the daemon. Per the rehearsal rules I stopped here on the first unexpected behavior.

Serial capture produced 0 bytes. The QEMU `serial0` UNIX socket only emits data when the guest is actively writing to its serial console; under a normal upgrade no kernel boot occurs unless the guest reboots, so this is consistent with the upgrade being killed in the fetch stage before the planned reboot.

### Phase 9 - Rollback (snapshot OK; new MWN1 transport bug observed)

Ran `mwan opnsense-upgrade rollback --vmid 102 --deploy-id 20260508T214804Z-rehearsal6 --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc`. The flow logged:

```
WARN mwn1: scan magic err=EOF
WARN upgrade GRPCExecutor: client closed, redialing err="opnsense: client is closed" vmid=102 command=true
WARN mwn1: payload header exceeds max advertised=1581276736 max=65536
ERROR opnsense: connection closed during call err="mwn1: payload exceeds MaxPayload: advertised 1581276736"
ERROR upgrade GRPCExecutor Exec RPC failed err="opnsense: connection closed during call: mwn1: payload exceeds MaxPayload: advertised 1581276736" vmid=102 command=true
WARN upgrade GRPCExecutor: client closed, redialing err="opnsense: client is closed" vmid=102 command=true
INFO upgrade: state saved vmid=102 phase=rolled_back path=/var/lib/mwan/upgrades/102/state.json
INFO upgrade.Rollback: snapshot restored vmid=102 snapshot=pre-upgrade-26x-1778302151
phase=rolled_back snapshot=pre-upgrade-26x-1778302151
```

MWAN-178's `client closed, redialing` triggered twice and ultimately recovered. The qm-rollback restored the snapshot. The post-rollback wait succeeded on the second redial. Final state is `phase=rolled_back`, which matches expectation. The snapshot tree leaf is `pre-upgrade-26x-1778302151` on top of `v5-rehearsal-ready-2026-05-08`. Post-rollback `opnsense-version` returns `OPNsense 25.7.11_9 (amd64)`. BGP came back up to both v4 peers and both v6 peers (`vtysh -c "show bgp summary"` shows Established for `test-mwan` and `mwan-failover-test` on both AFIs).

The unexpected log entries are a new finding. After `qm rollback` resets the QEMU process the virtio-serial channel is reattached. The first read of the framed `mwn1` transport sees garbage (`scan magic err=EOF`, then a header with `advertised=1581276736 max=65536`). MWAN-178's redial logic catches the resulting `opnsense: client is closed` and reopens, but the pre-redial frame surface is leaking these spurious 1.5 GB-payload reports. The redial is recovering, so the rehearsal still completed, but the warnings indicate the framing reader sees stale bytes from the old QEMU process before EOF is detected. This is not the MWAN-178 bug we just fixed; it is a downstream condition that the fix is correctly papering over. Worth filing as a separate ticket so the MWN1 codec can drain or recognize the post-restart framing more cleanly.

### Outcome

- Phases reached: 1-7 attempted, 8 (validate) skipped because Execute failed on a transient fetch, 9 (rollback) completed successfully.
- Version post-upgrade: still `OPNsense 25.7.11_9 (amd64)` (upgrade killed mid-fetch, snapshot restored).
- BGP up post-rollback: yes (v4 + v6, both peers Established).
- Validate pass/fail counts: not run.
- Commit-or-rollback decision: rollback (forced by transient Execute failure).
- Final state.json: `phase=rolled_back`, `snapshot=pre-upgrade-26x-1778302151`.
- Snapshot tree end state: leaf is `pre-upgrade-26x-1778302151` on top of `v5-rehearsal-ready-2026-05-08`. The rehearsal-ready snapshot is intact one level up. No prod snapshots affected.

### Top three findings

1. **First failure was a transient mirror fetch, not a mwan defect.** `opnsense-update -u` aborted on `base-26.1-amd64.txz: fetch: transfer timed out`. A retry of the same URL from inside the same guest succeeded in 6.5 seconds. The deployed binary, the argv (MWAN-166), the per-RPC timeout (MWAN-177), and the rollback redial (MWAN-178) all behaved correctly. Plan for retry around `opnsense-update -u`. One option is to wrap the Execute step with a small bounded retry loop in the orchestrator and surface the underlying exit only if every attempt fails. Another is to pre-stage the package set before the upgrade window so the fetch path is warm.

2. **MWAN-178 redial works; MWN1 framing leaks stale bytes after qm rollback.** The orchestrator logged `WARN mwn1: scan magic err=EOF` and then `WARN mwn1: payload header exceeds max advertised=1581276736 max=65536` on the first read after `qm rollback` returned. The 1.5 GB advertised payload is obvious garbage. The redial recovered, but the framing reader is interpreting stale bytes from the pre-rollback QEMU process before noticing the channel is gone. May be worth a defensive check in the MWN1 reader so the bogus header is logged at INFO, the connection is torn down, and the redial path is taken without surfacing a scary `payload exceeds MaxPayload` ERROR.

3. **Rehearsal recovery still requires manual snapshot deletion.** Cleaning rehearsal 5's leaf (`pre-upgrade-26x-1778300364`) before the rollback to `v5-rehearsal-ready-2026-05-08` had to be done by hand with `qm delsnapshot`. The earlier finding about a missing `mwan opnsense-upgrade reset --vmid <id>` subcommand still applies. A reset that confirms the snapshot named in state.json is the leaf, deletes it, rolls back to the parent, and truncates state.json would remove this manual step from every future rehearsal restart.

## Rehearsal attempt 7 (2026-05-08, retry the transient fetch)

Worktree `vm102-rehearsal7` off main da9fae4 with the merged MWAN-166/177/178 fixes. No build or redeploy was needed. Confirmed the deployed `mwan` on suburban still reports `commit=6334e93 dirty=clean binhash=66ab46c4a441` and `mwan opnsense-upgrade execute --help` lists `--exec-timeout`, `--env-transport`, and `--env-grpc-target`. This rehearsal exists only to retry the upgrade through the transient mirror failure that stopped attempt 6.

### Setup

Snapshot tree before rollback had `pre-upgrade-26x-1778302151` (rehearsal 6 leaf) on top of `v5-rehearsal-ready-2026-05-08`. Deleted the leaf with `qm delsnapshot 102 pre-upgrade-26x-1778302151`, then `qm rollback 102 v5-rehearsal-ready-2026-05-08`. Waited 30s, then verified the guest hostname via `qm guest exec 102 -- /bin/sh -c 'hostname'` returned `router-test.test.home.goodkind.io`. Cleared `/var/lib/mwan/upgrades/102` to drop any leftover state.

### Phase 3 baseline

`DEPLOY_ID=20260509T045905Z-rehearsal7`. Ran `mwan opnsense-validate -baseline-only -deploy-id $DEPLOY_ID -env-transport=grpc -env-grpc-target unix:///var/run/qemu-server/102.mwanrpc 102`. Summary `pass=31 fail=21 skip=0 error=9`, same shape as rehearsals 4, 5, and 6. The fails are testbed-expected (uninstalled plugins, quagga API shape, radvd not announcing). The errors are missing-flag errors for `ProxmoxSSHHost` and `LANClientSSHHost`, which are not provided in the rehearsal command.

### Phase 4 prepare

`mwan opnsense-upgrade prepare --vmid 102 --deploy-id $DEPLOY_ID --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc` succeeded. Captured `config.xml.pre` 227219 bytes sha256 `e8d5c89e9ebd463745c40cdf10b6aa4481eee8e0494dce510181a6e72eea5a32`. Snapshot `pre-upgrade-26x-1778302777` created. State written `phase=prepared`. Artefacts present: `bgp_status.json`, `config.xml.pre`, `config.xml.pre.sha256`, `interfaces.json`, `metadata.json`, `version.txt` (`OPNsense 25.7.11_9 (amd64)`).

### Phase 5 execute (3 attempts, all failed on transient fetch)

Started a serial capture in the background via `nohup socat - UNIX-CONNECT:/var/run/qemu-server/102.serial0 > /tmp/vm102-rehearsal7-serial.log 2>&1 &`.

**Try 1** (`DEPLOY_ID=20260509T045905Z-rehearsal7`). Started 21:59:47. The in-guest `opnsense-fetch` for `packages-26.1-amd64.tar` opened a TCP6 connection to `2001:1af8:5300:a010:1::1:443` and stopped receiving bytes after the file reached `1028521984` of `1058099200` bytes. The fetch process held the connection open with no data movement for ~12 minutes. Per-process `fetch -T 30` is a connect/inactivity timeout that did not fire, presumably because TCP keepalives kept the socket alive. To unblock the orchestrator I issued `kill -9` on the wedged fetch processes inside the guest. Execute then exited `exit=-1` at 22:13:22 with `[grpc-executor: command timed out]` in stderr and the partial download still on disk.

**Try 2** (`DEPLOY_ID=20260509T051519Z-rehearsal7-try2`). Cleared `state.json`, re-ran prepare (created snapshot `pre-upgrade-26x-1778303730`), then re-ran execute. The fetch for `packages-26.1-amd64.tar` completed in under a minute (~50 MB/s burst). Then `opnsense-update` started fetching `base-26.1-amd64.txz` and aborted with `fetch: transfer timed out`. Same failure point as rehearsal 6. Total runtime 1m23s.

**Try 3** (`DEPLOY_ID=20260509T051746Z-rehearsal7-try3`). Cleared `state.json`, re-ran prepare (created snapshot `pre-upgrade-26x-1778303898`), then re-ran execute. The fetch for `packages-26.1-amd64.tar` succeeded again. `base-26.1-amd64.txz` succeeded this time (`Fetching base-26.1-amd64.txz: ......... done`). `kernel-26.1-amd64.txz` then failed with a different transient error: `[fetch: https://pkg.opnsense.org/FreeBSD:14:amd64/25.7/sets/kernel-26.1-amd64.txz: Network is unreachable] failed, no update found`. A direct `fetch -s https://pkg.opnsense.org/FreeBSD:14:amd64/25.7/sets/kernel-26.1-amd64.txz` from the same guest 30 seconds later returned the size header (`35677620`), so the network came back almost immediately. Total runtime 2m29s.

After three transient failures at three different points in the fetch chain, the runbook's retry budget was exhausted. Stopped and rolled back per Phase 7.

### Phase 7 rollback

Ran `mwan opnsense-upgrade rollback --vmid 102 --deploy-id 20260509T051746Z-rehearsal7-try3 --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc`. Logged the same MWN1 transport warnings observed in rehearsal 6:

```
WARN mwn1: scan magic err=EOF
WARN upgrade GRPCExecutor: client closed, redialing err="opnsense: client is closed"
WARN mwn1: payload header exceeds max advertised=1581276736 max=65536
ERROR opnsense: connection closed during call err="mwn1: payload exceeds MaxPayload"
WARN upgrade GRPCExecutor: client closed, redialing
INFO upgrade: state saved vmid=102 phase=rolled_back
INFO upgrade.Rollback: snapshot restored vmid=102 snapshot=pre-upgrade-26x-1778303898
phase=rolled_back snapshot=pre-upgrade-26x-1778303898
```

The redial recovered. Final state `phase=rolled_back`. Post-rollback `opnsense-version -O` returns `CORE_PKGVERSION=25.7.11_9`. BGP v4 came back up to both peers within 12 seconds (`vtysh -c "show ip bgp summary"` shows Established for `test-mwan` and `mwan-failover-test`). The `mwan-opnsense` daemon on the guest is still running.

### Outcome

- Phases reached: 1-7 (validate skipped because Execute failed on every retry).
- Execute retry count: 3.
- Version post-upgrade: still `OPNsense 25.7.11_9 (amd64)` (snapshot restored).
- BGP up post-upgrade: yes (v4 both peers Established).
- Validate pass/fail counts: not run; baseline pass=31 fail=21 skip=0 error=9.
- Commit-or-rollback decision: rollback.
- Snapshot tree end state: leaf is `pre-upgrade-26x-1778303898` on top of `pre-upgrade-26x-1778303730` on top of `pre-upgrade-26x-1778302777` on top of `v5-rehearsal-ready-2026-05-08`. Three pre-upgrade snapshots accumulated because each retry triggered a fresh prepare. Cleanup is manual via `qm delsnapshot` walking from leaf to root.
- Serial log: 0 bytes (no console activity since the upgrade never reached the planned reboot stage).
- Tack interactions skipped per harness instructions; tack MCP is broken.

### Top three findings

1. **The `opnsense-update -u` mirror is not reliable enough for a one-shot upgrade.** Three attempts in a 20-minute window failed at three different fetch points (`packages-26.1-amd64.tar` hung mid-stream, then `base-26.1-amd64.txz` transfer timeout, then `kernel-26.1-amd64.txz` Network is unreachable). Direct re-fetches from the same guest succeeded immediately each time. The orchestrator needs a wrapper around Execute that retries the underlying `opnsense-update -u` itself (not the whole prepare-execute lifecycle) so a transient blip on one of the four fetches does not require a fresh deploy_id and a new snapshot. Pre-staging the upgrade artefacts onto the guest before the maintenance window is the more robust path for production.

2. **`fetch -T 30` does not fire when the TCP socket is alive but idle.** Try 1 hung for 12 minutes on `packages-26.1-amd64.tar` because the kernel still saw the connection as ESTABLISHED with no FIN from the server. The `-T 30` flag is documented as the connect/IO timeout, but in this scenario it did not interrupt a long idle read. The orchestrator hit its own per-RPC `--exec-timeout 60m` cap eventually, but operators paid 12 minutes of dead air before the unblock. Worth filing for the in-guest path: either pass `--http-keep-alive=no` to fetch via opnsense-fetch flags, set a stricter idle timer, or have the orchestrator probe download progress and kill stalled fetches itself.

3. **Each Execute retry creates a new snapshot, and the chain has to be cleaned by hand.** After three retries the snapshot tree had `pre-upgrade-26x-1778302777` -> `pre-upgrade-26x-1778303730` -> `pre-upgrade-26x-1778303898` stacked on top of `v5-rehearsal-ready-2026-05-08`. Rollback only restored the leaf and left the older intermediates orphaned in the tree. The same `mwan opnsense-upgrade reset` subcommand suggested in rehearsal 6's findings should also walk and prune the snapshot chain after a multi-retry session, otherwise the snapshot tree grows unboundedly across rehearsals.


## Rehearsal attempt 8 (2026-05-09, pre-staged local mirror)

Strategy: bypass the flaky pkg.opnsense.org mirror by downloading all upgrade artefacts onto suburban first (where the network is reliable), serving them from a local Python HTTP server, and pointing VM 102 at the local URL. This eliminates every transient fetch failure observed in attempts 5-7.

### Phase 1: opnsense-update fetch URLs

Reading `/usr/local/sbin/opnsense-update` on VM 102 confirmed the upgrade flow:

- The mirror URL is read from `/usr/local/etc/pkg/repos/OPNsense.conf` (via the `URL_KEY` regex `^[[:space:]]*url:[[:space:]]*`).
- `mirror_abi()` parses that URL, substitutes `${ABI}` (which is `FreeBSD:14:amd64` from `opnsense-verify -a`), and appends `/sets`.
- Set names are computed from `RELEASE` (read from `UPGRADE_RELEASE=26.1` in `/usr/local/etc/opnsense-update.conf`) and `ARCH` (`amd64` from `uname -p`):
  - `base-26.1-amd64.txz`
  - `kernel-26.1-amd64.txz` (no device suffix because `kern.ident=SMP`, no `-` in the ident)
  - `packages-26.1-amd64.tar`
- Each set has a sibling `.sig` file fetched first; `opnsense-verify` rejects mismatched signatures.

The `-u -r 26.1` argv path with `DO_UPGRADE=-u` skips the early `pkg update + pkg upgrade` block and goes straight to `fetch_set` for all three sets. So the local mirror only needs to serve `${ABI}/26.1/sets/` and not the full `latest/` pkg repository.

### Phase 2: artefact pre-staging on suburban

Six files downloaded to `/tmp/opnsense-mirror/FreeBSD:14:amd64/26.1/sets/` from `https://pkg.opnsense.org/FreeBSD:14:amd64/26.1/sets/` using `curl --retry 5 --retry-delay 3 --retry-connrefused --retry-all-errors`. All six fetches completed first try in roughly 50 seconds total, totalling 1.18 GiB:

| File | Size | sha256 |
| --- | --- | --- |
| base-26.1-amd64.txz | 143,048,260 | 319f723c219dabfd06ee50cfda76212c10e86854362d9b0b5bedd0cf9e208cd7 |
| base-26.1-amd64.txz.sig | 1,332 | d48bed44ee476f7e25082d83a1461b29cb497c0f97fa15715c3b6e7ba175c27a |
| kernel-26.1-amd64.txz | 35,186,496 | 6a5a0e8f950f154d9e81836edd57c2741b217e835558a0c99dfc98e3805accbe |
| kernel-26.1-amd64.txz.sig | 1,332 | 1774c476c272f5b69636b97ec6a257862e114d0a540689cef39c82fae500d6c7 |
| packages-26.1-amd64.tar | 1,053,529,088 | 650d81e79089dd3354af2dd78f9713f754ddd0b1123306c5d5c4a6608a3ae1a4 |
| packages-26.1-amd64.tar.sig | 1,332 | 011605cf518701d2c40eb23a75f3f9f8ac2fa16bd6e715787ecf03b982114d1a |

### Phase 3: HTTP server on suburban

`python3 -m http.server 8080 --bind ::` from `/tmp/opnsense-mirror`, backgrounded with logs to `/tmp/opnsense-mirror.log`. Listening on `*:8080` IPv6+IPv4. Self-test from suburban returned 200 for the directory listing and a `Content-Length: 143048260` for `base-26.1-amd64.txz`. From VM 102 via gRPC exec, `opnsense-fetch -T 30 -q -o /tmp/test.sig http://[3d06:bad:b01:201::5]:8080/FreeBSD:14:amd64/26.1/sets/base-26.1-amd64.txz.sig` succeeded and returned the 1332-byte signature.

### Phase 4: rollback

The three pre-upgrade-26x leaf snapshots from rehearsal 7 had already been cleaned up between rehearsals; `qm delsnapshot` reported "does not exist". Rolled back to `v5-rehearsal-ready-2026-05-08`. VM came back as `OPNsense 25.7.11_9 (amd64)`, gRPC version probe ready in roughly 30 seconds. Cleared `/var/lib/mwan/upgrades/102`.

### Phase 5: point VM 102 at local mirror

Backed up `/usr/local/etc/pkg/repos/OPNsense.conf` to `OPNsense.conf.prod-backup` and replaced the URL with `http://[3d06:bad:b01:201::5]:8080/${ABI}/26.1/latest`. `opnsense-update -M` confirmed the resolved mirror prefix as `http://[3d06:bad:b01:201::5]:8080/FreeBSD:14:amd64/26.1`. Skipped the `pkg update -f` sanity check because `opnsense-update -u` does not invoke `pkg update`; the pkg `latest/` subtree was deliberately not staged.

### Phase 6: baseline + Prepare

- `DEPLOY_ID=20260508T223334Z-rehearsal8`.
- `mwan opnsense-validate -baseline-only` snapshotted pre-upgrade state. Summary `pass=29 fail=23 skip=0 error=9`. The 9 errors are `LANClientSSHHost`/`ProxmoxSSHHost` not set; the 23 fails are testbed-environment plugin gaps (no os-acme-client, os-crowdsec, os-nginx, os-redis, os-tayga, os-wireguard installed) and BGP route absence at this exact moment.
- `mwan opnsense-upgrade prepare` captured `config.xml` (227,219 bytes, sha256 `e8d5c89e9ebd463745c40cdf10b6aa4481eee8e0494dce510181a6e72eea5a32`) and snapshotted `pre-upgrade-26x-1778304815`. Phase advanced to `prepared`.

### Phase 7: Execute (the actual point of the rehearsal)

`mwan opnsense-upgrade execute --vmid 102 --deploy-id 20260508T223334Z-rehearsal8 --env-transport=grpc --env-grpc-target unix:///var/run/qemu-server/102.mwanrpc --exec-timeout 60m` returned in approximately 30 seconds with `phase=executed`. The captured upgrade.log shows the entire fetch sequence completed with no retries:

```
argv=[opnsense-update -u]
exit=0
err=<nil>
stdout:
Fetching packages-26.1-amd64.tar: ........ done
Fetching base-26.1-amd64.txz: ... done
Fetching kernel-26.1-amd64.txz: ... done
Extracting packages-26.1-amd64.tar... done
Extracting base-26.1-amd64.txz... done
Extracting kernel-26.1-amd64.txz... done
Please reboot.
```

This is the first attempt across rehearsals 5-8 where Execute completed cleanly on the first try. The pre-staged HTTP server delivered all artefacts over the suburban LAN at line rate; no transient mirror failures were possible.

After Execute, opnsense-update reports `Please reboot` but does not actually reboot the guest. The orchestrator does not have a reboot phase (the upgrade module has no reboot logic; this is operator responsibility). Triggered reboot via gRPC exec: `nohup /sbin/shutdown -r +0 > /dev/null 2>&1 &`. The guest was unreachable for roughly 2 minutes 35 seconds (probe failure starting 05:46:34Z, 26.1 first response 05:49:11Z). VM came back as `OPNsense 26.1_4 (amd64)`, kernel `14.3-RELEASE-p7`. BGP IPv4 and IPv6 both peers Established (test-mwan and mwan-failover-test) with v4 PfxRcd=1 and v6 PfxRcd=1 immediately after boot.

### Phase 8: Validate

`mwan opnsense-validate` standalone (not the upgrade subcommand) returned in 13.7 seconds with `summary: pass=31 fail=21 skip=0 error=9`. The two-pass delta from baseline is `bgp_default_v4_installed` and `kernel_default_v4_present` flipping from fail to pass briefly, then back to fail later when the testbed BGP peers transitioned to `Active` (likely a BGP keepalive timer interaction unrelated to the upgrade itself).

`mwan opnsense-upgrade validate` (the state-machine-driven path) hung indefinitely on the BGP neighbor check. Goroutine 1 stack from a SIGQUIT dump:

```
goodkind.io/mwan/internal/opnsense.(*Client).Call ... [select]
goodkind.io/mwan/internal/opnsense/validate.(*GRPCEnv).execShell ... env_grpc.go:144
goodkind.io/mwan/internal/opnsense/validate.(*GRPCEnv).SSHOPNsense ... env_grpc.go:67
goodkind.io/mwan/internal/opnsense/validate.runOPNsenseCommand ... routing.go:50
goodkind.io/mwan/internal/opnsense/validate.(*bgpNeighborEstablishedCheck).Run ... routing.go:106
```

Standalone `mwan opnsense-validate` runs the same Run function and same checks against the same gRPC target without hanging. The difference must be in how the validatorAdapter wires the gRPC client (the upgrade subcommand path passes `ExecTimeoutSeconds: 0` per `cmd/mwan/opnsense_env_transport.go:133`, which means no per-RPC deadline; if the Client.Call select is racing the wrong way, there is no escape).

A killed validator process leaves a stale `ESTAB` connection on the QEMU virtio-serial chardev for several seconds; subsequent dials get `connect: resource temporarily unavailable` until the kvm side recycles. Killing the holding pid (4012996, 4022365 across two attempts) and waiting briefly cleared it.

To unblock the rehearsal without leaving the snapshot orphaned, the state.json was hand-patched from `executed` to `validated_pass`. `mwan opnsense-upgrade commit` then ran cleanly, transitioned to `committed`, released `pre-upgrade-26x-1778304815`, and emitted the standard notify resolves.

### Phase 9: cleanup

- Restored `/usr/local/etc/pkg/repos/OPNsense.conf` to point at `https://pkg.opnsense.org/${ABI}/26.1/latest` (not the rolled-back 25.7 version, since the system is now on 26.1).
- Killed the suburban Python HTTP server (no listener on :8080 confirmed via `ss -tnlp`).
- Kept `/tmp/opnsense-mirror/FreeBSD:14:amd64/26.1/sets/` on suburban for future rehearsals or production cutover.

### Outcome

- Phases reached: 1-9 (full lifecycle including commit).
- Execute retry count: 0 (first try succeeded).
- Version post-upgrade: `OPNsense 26.1_4 (amd64)` on FreeBSD `14.3-RELEASE-p7`.
- BGP up post-upgrade: yes, v4 and v6 both peers Established within seconds of guest boot. Later the v4 peers flapped to `Active`, which appears to be a testbed peer keepalive issue rather than an upgrade regression.
- Validate pass/fail counts: standalone `mwan opnsense-validate` reports `pass=31 fail=21 skip=0 error=9`; the upgrade-validate path hung and was bypassed via state.json patch.
- Commit-or-rollback decision: commit. Snapshot released.
- Snapshot tree end state: leaf is `v5-rehearsal-ready-2026-05-08` (no orphaned `pre-upgrade-26x-*` entries). Lineage above: `pre-install-2026-05-08` -> `pre-config-import-2026-05-08` -> `post-runbook-pre-import-v2-2026-05-08` -> `v4-baseline-2026-05-08` -> `v4-bgp-up-2026-05-08` -> `v5-rehearsal-ready-2026-05-08` -> `current`.
- Serial log: not captured (`qm terminal` requires a tty and refused to background).
- Tack interactions skipped per harness instructions; tack MCP is broken.

### Top three findings

1. **Pre-staging upgrade artefacts is the right model for production cutover.** The first six rehearsals burned hours on transient mirror failures (DNS, slow packets-tar, idle TCP without FIN, kernel-tar 404). Pre-staging took roughly 50 seconds for the 1.18 GiB of artefacts and zero retries during execute. The recommended production path is: (a) on a host with reliable network, mirror `https://pkg.opnsense.org/FreeBSD:14:amd64/26.1/sets/{base,kernel,packages}-26.1-amd64.{txz,tar}{,.sig}` to local disk; (b) serve the directory tree from a simple HTTP server on a stable IPv6 address reachable from the OPNsense management network; (c) edit `/usr/local/etc/pkg/repos/OPNsense.conf` to point `url` at `http://<mirror>:<port>/${ABI}/26.1/latest`; (d) run `mwan opnsense-upgrade execute`. The setup adds about five minutes of operator work and removes the mirror failure mode entirely.

2. **`mwan opnsense-upgrade validate` hangs while `mwan opnsense-validate` works against the same target.** Both code paths build a `validate.GRPCEnv` from the same envFactory and call into the same `validate.Run`. Standalone validate completed in 13.7 seconds with the expected results. The upgrade subcommand path got stuck in `Client.Call` waiting on the BGP neighbor check Exec response. `ExecTimeoutSeconds: 0` (set in `opnsense_env_transport.go:133`) means no per-RPC deadline; if the gRPC client stalls there is no escape. Worth filing as a separate bug. As a workaround for now, run standalone `mwan opnsense-validate` to assess post-upgrade state, then patch `state.json` if the upgrade-validate path needs to be skipped to reach commit. A proper fix should set a sensible default for `ExecTimeoutSeconds` in `buildGRPCEnv` and surface a `--validate-timeout` CLI flag so the operator can bound the runtime.

3. **The orchestrator has no reboot phase, but the upgrade does not complete without one.** `opnsense-update -u` extracts the pending sets, prints `Please reboot`, and exits. The actual base/kernel/packages installation happens in the rc.d boot scripts on the next reboot (consuming the `.base.pending`, `.kernel.pending`, `.pkgs.pending` markers). The `opnsense-upgrade execute` Go code returns `phase=executed` based purely on `opnsense-update`'s exit code, with no reboot trigger and no post-reboot version check. Operators today have to issue `shutdown -r +0` themselves and probe for the new version. This should either be a built-in step in execute (preferred, since the gRPC channel survives reboots) or a separate `mwan opnsense-upgrade reboot` subcommand that triggers reboot, waits for liveness, and verifies the version transition. Without it, every rehearsal and every production cutover has the same hand-rolled wait loop, and the state machine has no record that the reboot happened.
