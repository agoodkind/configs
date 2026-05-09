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
