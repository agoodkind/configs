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
