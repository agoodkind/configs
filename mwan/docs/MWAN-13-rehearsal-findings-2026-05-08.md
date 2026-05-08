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

