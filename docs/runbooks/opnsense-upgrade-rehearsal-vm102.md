# OPNsense Upgrade Rehearsal Runbook (VM 102)

Status: operator runbook, drafted 2026-05-08.
Tracking issue: MWAN-13 (parent OPNsense 26.x upgrade ticket).
Primary subcommands exercised: `mwan opnsense-upgrade` (designed in MWAN-152) and `mwan opnsense-validate` (designed in MWAN-153).

This runbook walks an operator through running an actual 25.7-to-26.x upgrade rehearsal against suburban VM 102 (`opnsense-test2`) using the new monolith subcommands. The intent is to capture findings against a prod-shaped 25.7 baseline so the prod cutover plan against vault VM 101 can reuse the same recipe with measured, not assumed, behavior.

Prerequisite reading (read first; this runbook references them by section):

- `mwan/docs/MWAN-152-opnsense-upgrade-rollback-design.md` (subcommand surface, state machine, snapshot conventions).
- `mwan/docs/MWAN-153-26x-upgrade-test-matrix.md` (validation surface, baseline JSON shape, severity rules).
- `mwan/docs/MWAN-151-26x-changelog-deep-dive.md` (risk register, FreeBSD 14.3 -> 14.3 carry, vtnet LRO default flip).
- `mwan/docs/opnsense-25.7-config-import-flow.md` (how the prod-shaped baseline was loaded onto VM 102).

## 1. Why this runbook

VM 102 is the testbed baseline established under MWAN-149 (`mwan/docs/runbooks/opnsense-testbed-baseline-vm102.md` lines 1-114). After today's MWAN-127 import via the swap-and-reconfigure recipe in `mwan/docs/opnsense-25.7-config-import-flow.md` lines 215-240, VM 102 carries a prod-shaped 25.7 `/conf/config.xml`. That makes it the right rehearsal target, since the upgrade behavior we care about is against config shapes that resemble prod, not against installer defaults.

The wedged VM 101 stays preserved as a forensic artifact per `mwan/docs/runbooks/opnsense-testbed-baseline-vm102.md` line 12 and `mwan/docs/MWAN-152-opnsense-upgrade-rollback-design.md` line 439. Do not rehearse on it.

The rehearsal produces three deliverables. First, the actual go/no-go signal for the prod cutover. Second, captured forensic artifacts (pre-baseline JSON, post-baseline JSON, diff report, upgrade log) usable for post-mortem review. Third, a reality check against the MWAN-151 risk register: which flagged risks fired, which did not, which need the register updated.

## 2. Pre-flight checklist

Run each item from the suburban operator workstation. Each item is a binary go or no-go signal; failure of any item halts the rehearsal until resolved.

### 2.0 Operational context introduced 2026-05-10/11

Several findings from the 2026-05-10 and 2026-05-11 sessions changed how this runbook is run. Read this subsection before the rest of the checklist so the commands later make sense.

The MANAGEMENT VLAN does not accept SSH from anyone. The VLAN is the UniFi control plane and OPNsense's pf rule set on `vlan0100` carries an explicit deny on inbound SSH that fires regardless of source. The `agoodkind` user is also configured with no password authentication and `PermitRootLogin no` in `sshd_config`, so `root@10.240.4.1` would be rejected even if pf admitted the connection.

The PRIVILEGED VLAN is the sanctioned admin SSH path. Suburban now carries a VLAN 100 sub-interface `vmbrtrunk.100` at `10.240.1.5/24` (codified in `/etc/network/interfaces` on 2026-05-11). Suburban's `root@hypervisor` ed25519 public key is durably stored in OPNsense's `/conf/config.xml` under `<system><user[name=agoodkind]><authorizedkeys>` (base64-encoded). Plain `ssh agoodkind@10.240.1.1` from suburban succeeds without any `-i` override. SSH commands in this runbook use this form.

Every snapshot taken during a rehearsal uses `--vmstate 0` explicitly. The Web GUI's Take Snapshot dialog defaults Include RAM to checked for running VMs and is forbidden for testbed work. Production OPNsense never uses vmstate. The earlier vmstate-bearing baseline lineage on VM 102 (snapshots dated 2026-05-08) produced hours of stale-RAM-rollback symptoms and is the reason for the no-vmstate rule.

Channel reliability is not uniform across the three OOB paths. The mwan-opnsense gRPC daemon at `unix:///var/run/qemu-server/102.mwanrpc` and the QEMU guest agent both wedge under exec-heavy load and stdin payloads as small as a few kilobytes; a `qm reset 102` recovers QGA and usually recovers the gRPC daemon, though sometimes the daemon stays wedged past a reset. Ticket MWAN-184 tracks this at high priority. For short-lived RPCs (version, xpath, the validate matrix) gRPC is reliable. For file pushes and long-running exec the SSH path via PRIVILEGED is the preferred channel; for true kernel-level fallback the serial console at `unix:///var/run/qemu-server/102.serial0` is the last resort.

OPNsense regenerates `/home/<user>/.ssh/authorized_keys` from the base64-encoded blob in `<user><authorizedkeys>` whenever it re-syncs the user, which happens on boot, on a GUI user save, on `revertBackup`, and on plugin events. The propagation script is `/usr/local/opnsense/scripts/auth/sync_user.php -u <username>`. Direct edits to the on-disk file work until the next sync, then disappear. Operator edits to authorized keys belong in the XML blob; the GUI is the documented path, but a console operator can edit `/conf/config.xml` with a proper XML tool and then invoke `sync_user.php` to propagate.

The user `agoodkind` on OPNsense has its login shell set to `/usr/local/bin/bash`. The bash package is not installed by default on a fresh OPNsense system, and sshd refuses to admit a user whose shell binary is missing with the message `User agoodkind not allowed because shell /usr/local/bin/bash does not exist`. The fix is `pkg install -y bash` on the guest. The 2026-05-11 baseline has bash installed already; a fresh rebuild from `pre-config-import-2026-05-08` would need this step.

The current testbed snapshot lineage carries `pre-install-2026-05-08` (disk-only) at the root, `pre-config-import-2026-05-08` (vmstate-bearing) one step below, then four intermediate vmstate-bearing snapshots from the 2026-05-08 work, then `ssh-ready-2026-05-11` (disk-only, the post-session state) as the current leaf. The four intermediate vmstate-bearing snapshots are still present pending operator deletion. Anyone rebuilding the baseline from scratch should roll back to `pre-config-import-2026-05-08` and produce a new disk-only chain forward.

### 2.0.1 vmstate-0 declaration

This rehearsal makes one explicit commitment up front: every `qm snapshot` invocation in any step below uses `--vmstate 0`. If a step omits the flag, the omission is a bug in the runbook; the operator should add `--vmstate 0` and surface the discrepancy. The Web GUI's Take Snapshot dialog is forbidden for any snapshot in this work.

### 2.0.2 bash presence on the guest

Before running anything that depends on SSH login as `agoodkind`, confirm bash is installed on the guest:

```sh
ssh suburban 'qm guest exec 102 --timeout 10 -- /bin/sh -c "test -x /usr/local/bin/bash && echo BASH_OK || echo BASH_MISSING"'
```

Expected output: `BASH_OK`. If `BASH_MISSING`, run `ssh suburban 'qm guest exec 102 --timeout 120 -- /usr/sbin/pkg install -y bash'` once before proceeding.

### 2.0.3 SSH from suburban via PRIVILEGED

Confirm the PRIVILEGED SSH path works end-to-end:

```sh
ssh suburban 'ssh -b 10.240.1.5 -o BatchMode=yes agoodkind@10.240.1.1 hostname'
```

Expected: `router-test.test.home.goodkind.io`. If this fails with `Permission denied (publickey)`, suburban's pubkey is not in OPNsense's `<authorizedkeys>` for `agoodkind`; re-add it via the `inject_authkey.py + sync_user.php` flow documented in the late-2026-05-10 handoff. If it fails with `Connection refused` from a non-`10.240.1.5` source, suburban's route to `10.240.1.1` is not using `vmbrtrunk.100`; check `ip route get 10.240.1.1` on suburban and re-create the sub-interface per the durable stanza in `/etc/network/interfaces`.

### 2.0.4 Channel reliability constraint

This runbook prefers SSH via PRIVILEGED for file pushes and any operation that streams more than a kilobyte of stdin. The mwan-opnsense gRPC daemon serves the validate matrix's RPCs and short xpath calls reliably; it wedges on larger exec or stdin payloads and requires a `qm reset 102` to recover. Reaching for QGA is fine for read-only probes via `qm guest exec`, but multi-step exec sequences in quick succession have wedged QGA in past sessions and should be replaced with one consolidated SSH call where possible.

### 2.1 Confirm VM 102 is at the prod-shaped 25.7 baseline

The MWAN-127 import recipe in `mwan/docs/opnsense-25.7-config-import-flow.md` line 215 leaves VM 102 with the prod hostname, prod LAN address shape, and the prod plugin set. Verify by reading the live `/conf/config.xml` over the PRIVILEGED SSH path (see §2.0):

```sh
ssh suburban 'ssh -b 10.240.1.5 agoodkind@10.240.1.1 "xmllint --xpath \"/opnsense/system/hostname/text()\" /conf/config.xml; echo; xmllint --xpath \"/opnsense/interfaces/lan/ipaddr/text()\" /conf/config.xml; echo"'
```

The hostname must match the prod-shaped value the MWAN-127 import wrote (the operator running the import should have recorded this in the slice notes; cross-check before moving on). The LAN ipaddr likely shows the testbed override `10.240.4.1` per `mwan/docs/runbooks/opnsense-testbed-baseline-vm102.md` line 130, since that block is rewritten for the testbed plane; that is expected.

If either field is unset or differs from the recorded MWAN-127 import expectation, stop. The baseline is not what we think it is.

### 2.2 Confirm a disk-only prod-shaped baseline snapshot exists

The operator running MWAN-127 should have taken a baseline snapshot immediately after the import succeeded, so the rehearsal can reset to it between cycles. As of 2026-05-11 the current leaf of the lineage is `ssh-ready-2026-05-11`, which is disk-only and carries the durable PRIVILEGED SSH access. The earlier dated snapshots from 2026-05-08 are vmstate-bearing and forbidden as rollback targets without a forced cold boot afterward; do not pick them for fresh rehearsal cycles. Verify on suburban:

```sh
ssh suburban 'qm listsnapshot 102 | grep -E "ssh-ready-2026-05-11|prod-shaped-25-7-baseline"'
```

If no disk-only prod-shaped baseline exists, take one now before the rehearsal starts. The name must be under 40 characters because Proxmox truncates silently past that, and the date goes in `--description` so it does not consume name budget:

```sh
ssh suburban 'qm snapshot 102 prod-shaped-25-7-baseline-v6 --vmstate 0 --description "Disk-only baseline: rebuilt from pre-config-import-2026-05-08, prod-shaped config imported, captured for MWAN-13 rehearsal cycles"'
```

The `--vmstate 0` argument is mandatory per §2.0.1 above. Production OPNsense never uses vmstate snapshots, and rollback to a vmstate snapshot resumes from saved RAM with stale wall clock, dead TCP sockets, and stale Unbound cache. The older recommendation in `mwan/docs/MWAN-152-opnsense-upgrade-rollback-design.md` section 11.1 to use vmstate is superseded by the 2026-05-10 finding tracked under MWAN-182.

### 2.3 Confirm the mwan-opnsense daemon is running and the gRPC probe succeeds

The MWAN-152 design routes `prepare` and `validate` over the typed RPC where available (`mwan/docs/MWAN-152-opnsense-upgrade-rollback-design.md` section 11.3, line 408). If the daemon is not up, those phases fall back to QGA, which works but loses the structured response shape.

```sh
ssh root@10.240.0.148 'mwan opnsense-probe -target unix:///var/run/qemu-server/102.mwanrpc -op version'
```

The expected output is a build banner of the form `commit=<sha> dirty=clean binhash=<hash>` per `mwan/docs/runbooks/opnsense-testbed-baseline-vm102.md` line 179. A non-zero exit means the daemon is not registered against the chardev. Check `service mwan_opnsense status` inside the guest and the symlink at `/dev/vtcon/io.goodkind.mwan-opnsense.0` per the same runbook lines 161-167.

### 2.4 Confirm the subcommands exist on the suburban host's installed mwan binary

`mwan opnsense-upgrade` and `mwan opnsense-validate` are both new subcommands. Confirm both before doing anything else.

```sh
ssh root@10.240.0.148 'mwan opnsense-upgrade --help'
ssh root@10.240.0.148 'mwan opnsense-validate --help'
```

If either subcommand returns "unknown command", deploy a fresh mwan binary using the AGENTS.MD recipe under "Manual rollout of a new mwan binary". Do not proceed with the rehearsal against an older binary. The flag shapes used below are the ones documented in MWAN-152 and MWAN-153 design docs; the operator should read `--help` before running each phase to confirm the deployed binary matches the design.

Note: the flag shapes shown in this runbook are the design-doc shapes from MWAN-152 sections 4.1 through 4.6 and MWAN-153 section 6. The implementation slice may have minor differences. When in doubt, `--help` is the source of truth, and this runbook is the cross-check.

## 3. Step 1: Capture pre-upgrade baseline

The pre-upgrade baseline run drives the diff in step 4, per `mwan/docs/MWAN-153-26x-upgrade-test-matrix.md` section 4 (line 178) and section 5 (line 213). The baseline produces a JSON artefact with one record per check.

Pick a deploy ID for this rehearsal cycle. Convention: `rehearsal-<unix-timestamp>` so the directory name is unambiguous. Example: `rehearsal-1746657600`. Set it once and reuse for every command in this cycle:

```sh
DEPLOY_ID="rehearsal-$(date +%s)"
echo "DEPLOY_ID=$DEPLOY_ID"
```

Pick a transport for the validator. The default is `--env-transport=ssh`, which reaches OPNsense via the operator-supplied `--opnsense-ssh` host. The MANAGEMENT address `10.240.4.1` rejects SSH by design, but as of 2026-05-11 the PRIVILEGED path `agoodkind@10.240.1.1` from suburban is durable (see §2.0) and the SSH transport works against it. The recommendation is still `--env-transport=grpc` for the validate matrix: gRPC routes OPNsense ops through the mwan-opnsense daemon over the persistent virtio-serial gRPC channel (MWAN-163) and survives a pf or routing break during the upgrade itself. The SSH path is a fine fallback when gRPC wedges (see §2.0.4) and is the preferred channel for file pushes. Production cutover should also prefer gRPC for the matrix and reach for SSH for any config push.

The gRPC transport requires `--env-grpc-target` pointing at the per-VM mwanrpc unix socket:

```sh
GRPC_TARGET="unix:///var/run/qemu-server/102.mwanrpc"
```

Proxmox-host and LAN-client checks still use SSH because the daemon does not proxy those surfaces. Pass `--proxmox-ssh` and `--lan-client-ssh` alongside the gRPC flag so those probes have a transport.

Capture the baseline:

```sh
ssh root@10.240.0.148 "mwan opnsense-validate --capture-baseline --vmid 102 --env-transport=grpc --env-grpc-target=${GRPC_TARGET} --output /var/lib/mwan/upgrades/102/${DEPLOY_ID}/pre-baseline.json"
```

The flag names follow MWAN-153 section 6 (line 250). The output path matches the storage layout in MWAN-153 section 9.8 (line 533), with the caveat noted in section 5 of this runbook below regarding the `upgrades/` versus `upgrade/` spelling gap.

Expected output: a JSON document with one record per check from the matrix in MWAN-153 sections 2.a through 2.i. The record shape is fixed in MWAN-153 section 4 (line 184): `check_id`, `raw_stdout`, `raw_exit_code`, `parsed_value`. The runner exits zero on success; non-zero means at least one blocker check failed at baseline, and the upgrade should not proceed (MWAN-153 section 4, line 207).

Operator action after the run:

- Open `pre-baseline.json` and skim the `parsed_value` for the routing-surface checks. Confirm BGP peer count matches expected: `bgp_v4_neighbor_established` and `bgp_v6_neighbor_established` should each list two peers (`10.250.250.3`, `10.250.250.4` for v4; `3d06:bad:b01:fe::3`, `3d06:bad:b01:fe::4` for v6) per MWAN-153 lines 51-52.
- Confirm the plugin list matches expected. The prod plugin set documented in MWAN-153 section 9.3 (line 392) is `os-acme-client, os-crowdsec, os-frr, os-git-backup, os-nginx, os-qemu-guest-agent, os-redis, os-tayga`. The testbed VM 102 may differ depending on what MWAN-127 imported; record the actual list as part of the rehearsal notes.
- Confirm captive portal coverage. Per MWAN-153 section 9.2 (line 344), prod runs the core captive portal feature with one zone (`c561c16d-1165-4df3-8f9f-23626c79fa12`). The matrix `core_captiveportal_zones_active` check should record the zone in the baseline.
- Confirm nothing is flagged as a `blocker` failure at baseline. If any blocker fails, the host is unhealthy before the upgrade; rolling back the rehearsal to `prod-shaped-25.7-baseline-2026-05-08` and re-running step 1 may clear it. If it persists, file a bug against MWAN-127 import quality before proceeding.

## 4. Step 2: Prepare phase

The prepare phase takes the Proxmox snapshot, captures pre-upgrade artefacts beyond the matrix baseline, and transitions the state machine to `prepared`. See MWAN-152 section 4.1 (line 118).

On the suburban testbed, prepare cannot reach the guest over QGA: VM 102 has no `os-qemu-guest-agent` package installed and no internet egress to install it (see `MWAN-13-rehearsal-findings-2026-05-08.md`). Use `--env-transport=grpc` so the in-guest capture commands (`cat /conf/config.xml`, `opnsense-version`, `ifconfig -av`, `netstat -rn`, `vtysh -c 'show bgp summary json'`) route through the mwan-opnsense daemon's Exec RPC instead of QGA. The `--env-grpc-target` value matches the validator flag set above.

```sh
ssh root@10.240.0.148 "mwan opnsense-upgrade prepare \
  --vmid 102 --target 26.1.7 --deploy-id ${DEPLOY_ID} \
  --env-transport=grpc --env-grpc-target=${GRPC_TARGET}"
```

Production cutover should also prefer `--env-transport=grpc` because the gRPC path survives a pf or routing break that would silently fail the QGA channel.

`--target 26.1.7` is the latest 26.1 release per MWAN-151 section 1 (line 47). The target should always be a specific point release rather than the bare ABI string, so the rehearsal exercises a real upgrade path.

What `prepare` does, mapped to the design:

- Reads `Version` over the typed RPC and writes `version.txt` to the state dir (MWAN-152 section 4.1 step 3).
- Reads `BackupConfigXML` and writes `config.xml.pre` plus a sha256 (MWAN-152 section 4.1 step 3).
- Captures `bgp_status.json`, `interfaces.json`, and `metadata.json` (same).
- Takes the Proxmox snapshot with the prefix `pre-upgrade-26x-<unix-timestamp>` per MWAN-152 section 3 (line 99). The vmstate pause should be near the 6.67s measurement on suburban (MWAN-152 section 11.1, line 391).
- Writes the rollback state file with `phase=prepared` to `<state_dir>/102/state.json` per MWAN-152 section 4.7 (line 224).
- Emits an `opnsense-upgrade-prepared` Info event over `notify` per MWAN-152 section 11.7 (line 463).

Operator action after the run:

- Confirm the state file landed:

  ```sh
  ssh root@10.240.0.148 "cat /var/lib/mwan/upgrades/102/state.json"
  ```

  The file is JSON. Confirm `"phase": "prepared"` and note the `"snapshot"` field value (`pre-upgrade-26x-<ts>`). Record the snapshot name next to the deploy ID for use in step 5.

- Confirm the per-deploy directory has all the artefacts:

  ```sh
  ssh root@10.240.0.148 "ls -la /var/lib/mwan/upgrades/102/${DEPLOY_ID}/"
  ```

  Expected contents: `version.txt`, `config.xml.pre`, `config.xml.pre.sha256`, `bgp_status.json`, `interfaces.json`, `metadata.json`, plus `pre-baseline.json` from step 1.

- If `prepare` failed at the snapshot step, the state file is not written and nothing else happens (MWAN-152 section 4.1 failure path, line 137). Inspect Proxmox storage health (`zpool status` on suburban) and re-run `prepare` after clearing.

## 5. Step 3: Execute phase

The execute phase runs the in-guest upgrade. See MWAN-152 section 4.2 (line 140).

```sh
ssh root@10.240.0.148 "mwan opnsense-upgrade execute \
  --vmid 102 --deploy-id ${DEPLOY_ID} \
  --env-transport=grpc --env-grpc-target=${GRPC_TARGET}"
```

The same `--env-transport=grpc` flag applies here as on prepare. QGA is still not available on the testbed VM. The execute argv (`opnsense-upgrade -r <target>`) is dispatched through the daemon's Exec RPC. The per-call timeout falls back to the daemon default. The upgrade-watchdog timeout from `--upgrade-timeout` still gates the surrounding context.

What `execute` does:

- Loads the state file. Refuses to execute unless `phase=prepared` (MWAN-152 section 4.2 step 1, line 145).
- Runs the OPNsense upgrade in the guest via the configured Executor (QGA on the SSH transport, mwan-opnsense daemon on the gRPC transport). The argv shape is `["opnsense-upgrade", "-r", "26.1.7"]` or whatever the design slice settled on (MWAN-152 section 4.2 step 2, line 146; channel decision in section 11.3, line 408).
- Streams stdout and stderr to `<state_dir>/102/<deploy-id>/upgrade.log`.
- Applies a watchdog timeout (default 30 minutes per MWAN-152 section 4.2 step 4, line 148). Operator can override via `--timeout` if available.
- Reboots the VM as part of the upgrade itself; the QGA channel comes back on the new kernel.
- On clean exit, writes `phase=executed` and emits `opnsense-upgrade-executed`.

Operator action during the run:

The upgrade is a 10 to 30 minute window. Watch three signals in parallel.

First, the upgrade log itself:

```sh
ssh root@10.240.0.148 "tail -f /var/lib/mwan/upgrades/102/${DEPLOY_ID}/upgrade.log"
```

Second, the OPNsense system log over the PRIVILEGED SSH path (see §2.0). SSH drops during the reboot window and comes back once the guest is up:

```sh
ssh -J suburban -o ProxyCommand='ssh -W %h:%p suburban' -o BindAddress=10.240.1.5 agoodkind@10.240.1.1 'tail -f /var/log/system/latest.log'
```

If the ProxyCommand-plus-BindAddress combination is fiddly in your client config, the simpler two-hop form works:

```sh
ssh suburban 'ssh -b 10.240.1.5 agoodkind@10.240.1.1 "tail -f /var/log/system/latest.log"'
```

Third, the serial console as a kernel-level fallback for the reboot window:

```sh
ssh root@10.240.0.148 'socat - UNIX-CONNECT:/var/run/qemu-server/102.serial0'
```

The serial console is the only signal that survives a kernel panic or a botched bootloader rewrite. If the upgrade hangs or the VM does not reboot cleanly, the serial console output is the forensic artefact.

Failure modes during execute, per MWAN-152 section 6 (line 282):

- Upgrade hangs past the watchdog timeout: state transitions to `execute_hung`. In single-step mode (this runbook) no auto-rollback fires; the operator decides.
- Upgrade returns non-zero: state transitions to `execute_failed`. No auto-rollback in single-step mode (MWAN-152 section 4.2 failure path, line 151).
- Kernel panic: state stays at `executed` because the QGA call may have returned before the panic. The validate phase will catch the unreachability.
- Network goes dead post-reboot: same shape as kernel panic; validate catches it.

## 6. Step 4: Validate phase

Two ways to run the post-upgrade validation, and the design supports both.

The `mwan opnsense-upgrade validate` form runs the full matrix against the upgraded VM and stamps the state file:

```sh
ssh root@10.240.0.148 "mwan opnsense-upgrade validate --vmid 102 --deploy-id ${DEPLOY_ID}"
```

The MWAN-153 check matrix needs operator-supplied transport flags to reach the OPNsense guest, the Proxmox host, and a LAN client; these mirror `mwan opnsense-validate` and are read by the upgrade subcommand's validator adapter (MWAN-160). Any subcommand that drives the matrix (`validate` and `run`) accepts the same flag set:

```sh
ssh root@10.240.0.148 "mwan opnsense-upgrade validate \
  --vmid 102 --deploy-id ${DEPLOY_ID} \
  --opnsense-ssh router --opnsense-jump vault \
  --proxmox-ssh vault --lan-client-ssh client \
  --opnsense-addr 10.240.0.1 \
  --api-key ${OPN_API_KEY} --api-secret ${OPN_API_SECRET} \
  --bgp-v4-neighbors 10.0.0.1 --bgp-v6-neighbors 'fc00::1' \
  --mwan-opnsense-socket /run/mwan-opnsense.sock"
```

When the operator omits these flags the matrix runs with empty defaults; checks that need a transport return `error`, which the upgrade state machine treats as `validate_failed`.

The `mwan opnsense-validate --compare` form runs the matrix and produces an explicit diff against the baseline JSON, per MWAN-153 section 6 (line 252):

```sh
ssh root@10.240.0.148 "mwan opnsense-validate --compare /var/lib/mwan/upgrades/102/${DEPLOY_ID}/pre-baseline.json --vmid 102 --output /var/lib/mwan/upgrades/102/${DEPLOY_ID}/post-result.json"
```

For the rehearsal, run both. The first stamps the state machine so commit and rollback work; the second produces the diff artefact for the post-mortem.

Pass criteria, per MWAN-153 section 5 (line 213) and MWAN-152 section 4.3 (line 154):

- All `blocker` checks pass: phase becomes `validated` and the rehearsal can commit.
- A `regression` check fails but no `blocker`: phase becomes `validate_failed` by default, but `--accept-partial` overrides to `validated_partial` for human decision per MWAN-152 section 4.3 step 5 (line 176).
- Any `blocker` fails: phase becomes `validate_failed`. Single-step rehearsal does not auto-rollback (MWAN-152 section 6, line 282).

The MWAN-151 risk register intersects with three checks the operator should single out:

- `kernel_default_v4_persists_post_finalize` and `kernel_default_v6_persists_post_finalize` (MWAN-153 section 9.7 row 2 and 3, lines 506-507): these sample the kernel default route three times at 30 second intervals starting 60 seconds after `firmware-finalize` exits. They catch the MWAN-151 R1 hazard (`system_routing_configure` flushes the BGP-installed default).
- `vtnet_hwlro_disabled` (MWAN-153 section 9.7 row 4, line 508): expected to flip from 1 to 0 across the upgrade per MWAN-151 commit `c7cd4884`. Recorded as advisory because the new default is correct for a forwarding box.
- `quagga_api_post_only` (MWAN-153 section 9.7 row 6, line 510): expected to return HTTP 405 or 400 on GET to `/api/quagga/bgp/set` post-upgrade, confirming the 26.1 MVC POST-only hardening took effect (MWAN-151 section 6).

Operator action after the run:

```sh
ssh root@10.240.0.148 "cat /var/lib/mwan/upgrades/102/${DEPLOY_ID}/post-result.json | jq '.results[] | select(.severity == \"blocker\" and .outcome != \"pass\")'"
```

If that `jq` filter returns any records, those are the blockers to inspect. The diff-report.json (also written under the same dir per MWAN-153 section 9.8 line 540) provides the per-check pass-pass / pass-fail / fail-pass classification from MWAN-153 section 5.

## 7. Step 5: Commit or rollback

The rehearsal cycle is not complete until the operator picks one. Both end states stamp the state machine into a terminal phase per MWAN-152 section 5 (line 234).

### 7.1 Commit (the upgrade is good)

Use commit when the validate phase landed at `validated` and the operator is ready to release the safety net.

```sh
ssh root@10.240.0.148 "mwan opnsense-upgrade commit \
  --vmid 102 --deploy-id ${DEPLOY_ID} \
  --env-transport=grpc --env-grpc-target=${GRPC_TARGET}"
```

The transport flags do not affect commit semantics on their own. Commit is a snapshot delete plus a state-file write. The same flag set is required for any subsequent `validate` or `run` invocation in the same shell. Passing the flags here keeps the operator's history-recall consistent across phases.

What commit does, per MWAN-152 section 4.5 (line 195):

- Refuses unless `phase` is `validated` or `rolled_back`.
- Deletes the prepare-phase Proxmox snapshot.
- Writes `phase=committed` and emits `opnsense-upgrade-committed`.
- Idempotent: running it twice is a no-op.

After commit, the rehearsal cycle is locked. The next cycle starts with a fresh deploy ID.

### 7.2 Rollback (the upgrade is bad)

Use rollback when validate landed at `validate_failed`, `execute_hung`, or `execute_failed`.

```sh
ssh root@10.240.0.148 "mwan opnsense-upgrade rollback \
  --vmid 102 --deploy-id ${DEPLOY_ID} \
  --env-transport=grpc --env-grpc-target=${GRPC_TARGET}"
```

Pass the gRPC transport flags so the post-rollback liveness probe (a `true` exec via the Executor) lands on the daemon path rather than QGA. After a Proxmox `qm rollback`, the guest reboots. The daemon starts with the guest. The daemon is reachable as soon as the virtio-serial socket is up. This is the same liveness signal the validator uses.

What rollback does, per MWAN-152 section 4.4 (line 178):

- Refuses unless phase is one of `executed`, `execute_failed`, `execute_hung`, `validate_failed`, `validated_partial`.
- Deletes child snapshots in newest-first order via `rollback.SnapshotsAfter` plus `ops.VMDelSnapshot`.
- Calls `ops.VMRollback` against the prepare-phase snapshot, restores both disk and vmstate.
- Polls QGA with a 60 second deadline to confirm the guest came back.
- Re-runs the validate matrix as a post-rollback sanity check.
- On success: `phase=rolled_back` and `opnsense-upgrade-rolled-back` Warn event.
- On failure: `phase=rollback_failed` and `opnsense-upgrade-rollback-failed` Error event with the `loud-alert` payload (MWAN-152 section 6, line 290).

After a successful rollback, the operator can either run `commit` to release the safety net (the prepare-phase snapshot is no longer the rollback target since the rollback already consumed its purpose), or leave the snapshot in place for forensic inspection per MWAN-152 section 4.4 step 8 (line 191).

If `phase=rollback_failed`, drop to OOB access at `root@3d06:bad:b01:ff::1` per `feedback_cutover2_corrections` and `project_oob_access` and recover manually.

## 8. One-shot mode

After the first manual rehearsal cycle has succeeded, repeat cycles can use one-shot mode. See MWAN-152 section 4.6 (line 207).

```sh
ssh root@10.240.0.148 "mwan opnsense-upgrade run \
  --vmid 102 --target 26.1.7 --deploy-id ${DEPLOY_ID} \
  --env-transport=grpc --env-grpc-target=${GRPC_TARGET} \
  --auto-rollback-on-fail"
```

The `run` subcommand fans out to prepare, execute, validate, and either rollback or commit under one set of flags. The gRPC transport flags propagate to every phase from a single invocation. This is the recommended shape for testbed cycles.

What run does:

- Runs `prepare` then `execute` then `validate` sequentially.
- Auto-rolls back on triggers per MWAN-152 section 4.6 (lines 211-216): `execute_hung`, `execute_failed` with all-fail validate, full-fail validate.
- Does not auto-roll back on partial validate pass (the operator must decide).
- Emits `opnsense-upgrade-run-complete` at the highest severity reached during the run.

When to use one-shot:

- After the first successful manual rehearsal, when the matrix and state machine are known-working on this VM.
- For batch tests that exercise the same upgrade target multiple times to bound variance.
- Not for the first cycle. The first cycle should run each phase manually so the operator sees each artefact land.

Pair `--dry-run-execute` (MWAN-152 section 11.4, line 425) with one-shot to exercise the full state machine without committing the upgrade. The dry-run swap is `opnsense-upgrade -c` (check only) instead of the real upgrade; phase transitions still happen, so the matrix exercises against an unchanged guest and the snapshot lifecycle still runs.

## 9. Observability during the upgrade

Three vantage points cover the upgrade window.

### 9.1 OPNsense's own view (in-guest log)

```sh
ssh -J root@10.240.0.148 root@10.240.4.1 'tail -f /var/log/system/latest.log'
```

The `-J` jump-host syntax routes through suburban. The `10.240.4.1` is VM 102's testbed LAN address per `mwan/docs/runbooks/opnsense-testbed-baseline-vm102.md` line 130. SSH drops during the reboot window; reconnect once the guest is back.

### 9.2 The gRPC bridge (host view)

The `mwan-opnsense-host` systemd unit on suburban is the host-side counterpart to the in-guest daemon. Its log is the operator's view of every gRPC call during prepare and validate.

```sh
ssh root@10.240.0.148 'journalctl -u mwan-opnsense-host -f'
```

A flurry of activity during prepare (`Version`, `BackupConfigXML`, `Exec` for ifconfig and netstat) and during validate (the matrix RPC calls) is expected. Silence during execute is expected, since the upgrade itself runs over QGA per MWAN-152 section 11.3 (line 408).

### 9.3 Serial console (kernel-level fallback)

```sh
ssh root@10.240.0.148 'socat - UNIX-CONNECT:/var/run/qemu-server/102.serial0'
```

The serial socket is the only signal that survives kernel panics, bootloader rewrites, and lost network state. The MWAN-149 OpenTofu shell exposes it (per `mwan/docs/runbooks/opnsense-testbed-baseline-vm102.md` line 23). Always have this connected during the execute phase.

## 10. Capturing findings for the prod cutover

After each rehearsal cycle (commit or rollback), the artefacts under `/var/lib/mwan/upgrades/102/${DEPLOY_ID}/` are the operator's record of what happened. Per MWAN-153 section 9.8 (line 533) the artefacts are:

- `pre-baseline.json` (step 1).
- `post-result.json` (step 4).
- `diff-report.json` (step 4).
- `snapshot-meta.json` (step 2 metadata.json equivalent).
- `upgrade.log` (step 3).
- `version.txt`, `config.xml.pre`, `config.xml.pre.sha256`, `bgp_status.json`, `interfaces.json`, `metadata.json` (step 2).

Copy these into the repo for the post-mortem so they survive the eventual `gc` pass (MWAN-152 section 11.8, line 478):

```sh
mkdir -p /Users/agoodkind/Sites/configs/mwan/docs/upgrades/${DEPLOY_ID}
scp -r root@10.240.0.148:/var/lib/mwan/upgrades/102/${DEPLOY_ID}/ /Users/agoodkind/Sites/configs/mwan/docs/upgrades/${DEPLOY_ID}/
```

Cross-reference with the MWAN-151 risk register. For each entry in `mwan/docs/MWAN-151-26x-changelog-deep-dive.md` section 7 (the risk register), record one of:

- The risk fired and was caught by a matrix check (note which check).
- The risk fired and was not caught (file a follow-up to add the check).
- The risk did not fire (note the supporting evidence from the diff report).

The expected interesting risks based on the changelog deep dive:

- R1 (kernel default route flush during `firmware-finalize`): caught by `kernel_default_v4_persists_post_finalize` and `kernel_default_v6_persists_post_finalize` per MWAN-153 section 9.7.
- R5 (vtnet LRO off-by-default flip): caught as advisory by `vtnet_hwlro_disabled`.
- R6 (interfaces.inc refactor): caught by `interfaces_set_unchanged`.
- The MVC POST-only hardening: caught by `quagga_api_post_only`.

If any of these fired in a way the matrix did not catch, the rehearsal has surfaced a real gap. Update both this runbook and MWAN-153 in a follow-up before the prod cutover.

## 11. What to do if the rehearsal succeeds

Success means: `phase=committed` on the state file, `post-result.json` shows no blocker failures, and the diff report shows only advisory or expected differences against the pre-baseline.

Operator action:

- Mark the rehearsal cycle in MWAN-13 (the parent upgrade ticket) as a successful 26.x rehearsal. Include the deploy ID, the target version, the snapshot retention decision, and a one-line summary of the diff highlights.
- File a follow-up ticket scoped to the actual prod cutover. Title suggestion: `MWAN-XXX: schedule OPNsense 26.1.7 cutover on vault VM 101`. The cutover plan reuses this runbook with three substitutions: VMID `101` (not `102`), Proxmox host `vault` (not `suburban`), and SSH bastion routing `ssh vault` (not `ssh root@10.240.0.148`).
- Before scheduling the prod cutover, pre-flight items from MWAN-152 section 8.3 (line 343): confirm BGP graceful-restart timing absorbs the snapshot pause measured on prod hardware, and probe the prod disk backend per MWAN-152 section 11.9 (line 499) since the suburban vs prod backend is still partially resolved.

The prod plan should also re-run step 1 of this runbook against vault VM 101 to capture the prod baseline. The testbed baseline on VM 102 is similar but not identical to prod (LAN addressing differs, possible plugin set drift); the prod baseline is the source of truth for the prod diff.

## 12. What to do if the rehearsal fails partway

Failure means: any non-`committed` terminal state where the diff report or the upgrade log shows the upgrade did not land cleanly.

Operator action:

- Capture forensics first. Copy the artefacts per section 10 before any further state changes.
- Roll back via `mwan opnsense-upgrade rollback` if not already done. Confirm `phase=rolled_back` and that the post-rollback validate passes (MWAN-152 section 4.4 step 7, line 190).
- File a follow-up ticket per failure mode. Title suggestion shape: `MWAN-XXX: 26.x upgrade fails at <phase> on VM 102 due to <observation>`. Examples:
  - `26.x upgrade fails at firmware-finalize step due to interface name shift` (would map to MWAN-151 R6).
  - `26.x upgrade fails post-reboot because BGP default route does not re-install within 30s` (would map to MWAN-151 R1).
  - `26.x upgrade fails because os-frr 1.51 plugin install errors out on prod-shaped config` (would file a new risk).
- Re-run the rehearsal from `prod-shaped-25.7-baseline-2026-05-08` once the issue is understood. The reset path:

  ```sh
  ssh root@10.240.0.148 'qm rollback 102 prod-shaped-25.7-baseline-2026-05-08'
  ```

  After the rollback, re-run section 2 (pre-flight) before the next cycle so any drift between cycles is caught.

The rehearsal can absorb several failure cycles. Each cycle adds a row to the post-mortem table and either confirms or refines the risk register.

## 13. Known gaps in the prerequisite docs

One open gap remains; a previously flagged state-directory spelling disagreement has been resolved (see 13.1).

### 13.1 State directory spelling (resolved: plural)

MWAN-152 section 11.6 and MWAN-153 section 9.8 previously disagreed about whether the state directory should be `/var/lib/mwan/upgrades/` (plural) or `/var/lib/mwan/upgrade/` (singular). Both documents now agree on plural, matching the implementation (`upgrade.DefaultStateDir = "/var/lib/mwan/upgrades"`). The commands in this runbook all use the plural form.

### 13.2 Vault prod VM 101 disk backend (MWAN-152 9.9)

Section 11.9 of MWAN-152 (line 499) is only partially resolved. The suburban testbed sits on `local-zfs`, but prod may be `local-lvm` or directory storage. The snapshot pause budget and the ZFS-host snapshot fallback both depend on the answer. The prod cutover plan must include a `qm config 101` probe on vault as the first step, which is out of scope for the rehearsal on VM 102 but is a hard prerequisite for the actual cutover.

The disk backend gap is tracked in MWAN-152 design section 11.11 follow-up list (line 532). It does not block this rehearsal because the rehearsal runs on suburban, where the backend is known.
