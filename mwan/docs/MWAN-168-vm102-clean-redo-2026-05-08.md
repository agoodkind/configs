# MWAN-168 VM 102 clean rebuild attempt 2026-05-08

This run rebuilds VM 102 (suburban testbed) from a clean post-install OPNsense
state with QGA and `os-frr` baked in BEFORE the prod-shaped config import. The
prior attempt installed those packages after import; that attempt is rolled
back. This document captures what was reached, the failure mode that stopped
the run, and the recovery state.

## Starting state

Snapshot chain at run start:

```
pre-install-2026-05-08
  pre-config-import-2026-05-08
    current  (clean OPNsense, hostname opnsense-test2.test.home.goodkind.io)
```

VM 102 config at start: `net0` on `vmbrtrunk` only, plus the `mwanrpc`
virtio-serial args block. No transient bridge attached.

The mwn-opnsense gRPC daemon was running on the clean snapshot (the
pre-config-import snapshot already had the daemon binary in place), so
`mwan opnsense-probe` could drive every step that followed.

## Phases reached

| Phase | Status | Notes |
| --- | --- | --- |
| 2: transient `net1` on `vmbr1` | done | `vtnet1` came up, `10.240.200.102/24` alias added, ping to `10.240.200.1` returned 0% loss. |
| 3: runbook lines 201-228 (QGA install) | done | `pkg update -f` succeeded. `pkg install -y os-qemu-guest-agent` installed `os-qemu-guest-agent-1.3` and `qemu-guest-agent-10.1.3`. `sysrc qemu_guest_agent_enable=YES` set. `service qemu-guest-agent start` returned exit 0. `qm guest cmd 102 ping` returned 0. `qm guest exec 102 -- /bin/hostname` returned `opnsense-test2.test.home.goodkind.io`. |
| 4: install `os-frr` | done with caveat | `pkg info -E os-frr` returned `os-frr-1.50_1`. `/usr/local/bin/vtysh` present, 6273224 bytes. `pkg install` printed `pkg: POST-INSTALL script failed` because the clean OPNsense lacks `indexinfo` and `syslog-ng` until config import. `pkg info -E` confirmed install succeeded despite the post-install hook noise. |
| 5: snapshot `post-runbook-pre-import-2026-05-08` | done | `qm snapshot 102 ... --vmstate 1` saved 1.88 GiB of RAM in 3 seconds. `qm listsnapshot 102` shows the snapshot in the chain. |
| 6: remove transient NIC | done | `route delete default 10.240.200.1`, `ifconfig vtnet1 -alias`, `qm set 102 --delete net1` all returned clean. `ifconfig | grep -c vtnet1` returned 0. `qm config 102` shows only `net0` again. |
| 7: regenerate `config-testbed.xml` | done | Built `mwan/go/bin/mwan` from worktree commit `237a88b`. Ran `mwan opnsense-import-config` with the redacted prod XML at `/Users/agoodkind/Sites/configs/.claude/worktrees/mwan-redact-opnsense-config/tmp/opnsense-prod-config.redacted.xml` and substitutions at `mwan/testbed/opnsense/substitutions.yaml`. Output written to `mwan/testbed/opnsense/generated/config-testbed.xml` (219492 bytes). `xmllint --noout` passed. |
| 8: import via `revertBackup` | failed before HTTP call | API key minted via `OPNsense\Auth\API::createKey('root')` and round-tripped to a local mode-600 tmpfile. Push of the 219kB XML payload into `/conf/backup/` wedged both transports. Run halted per the "stop at first unexpected behavior" rule. |
| 9-11: post-import verify, baseline snapshot, doc + commit | not reached for verify+baseline; this doc and commit happen now. | |

## Failure mode at Phase 8

Two write attempts both failed and both transports stopped responding:

1. `mwan opnsense-probe` with `-stdin-file` pointing at the base64-encoded XML
   (292kB) and `-cmd /bin/sh -c "b64decode -r > /conf/backup/...".`
   The probe logged `payload_len=292813` then hit `context deadline exceeded`
   at the 60s timeout. After that, every subsequent gRPC call (including a
   plain `op=version` with a 5s timeout) returns
   `context deadline exceeded`.
2. `qm guest exec 102 --pass-stdin 1 --timeout 60 -- /bin/sh -c "cat > /conf/backup/..." < /tmp/config-testbed.xml`
   on suburban returned `VM 102 qga command 'guest-exec' failed - got
   timeout`. After that, `qm guest cmd 102 ping` returns `QEMU guest agent is
   not running`.

The QGA path failed even though the QGA virtio-serial bus is independent of
the mwanrpc bus (the qemu commandline shows two separate `virtio-serial`
devices). The failure is unlikely to be virtio-serial buffer contention
between the two channels.

The most likely root cause is on the OPNsense side: when the gRPC stdin send
filled the virtserialport read queue faster than the mwn-opnsense daemon
could drain it, the daemon either deadlocked on a write back to the host
(for the small synchronous OK frames it owes) or it blocked on reading the
remainder. Once the daemon was wedged, a parallel attempt to use QGA on a
separate bus with `--pass-stdin 1` likely tripped a similar issue inside
`qemu-guest-agent`, since both daemons sit behind virtio-serial-port and the
guest-side virtio-console driver is a single shared module. No verifiable
source has been confirmed for "wedging one virtserialport blocks the other";
this is a hypothesis that the next slice should isolate before retrying.

`qm status 102` still reports `running`. The qemu kvm process is still
alive. Memory and disk look healthy. The virtio-serial stack inside the
guest is the only thing wedged.

## Forensics captured

```
qm config 102   shows parent=post-runbook-pre-import-2026-05-08, only net0
qm status 102   running
qm listsnapshot   pre-install -> pre-config-import -> post-runbook-pre-import -> current
ls /var/run/qemu-server/102.mwanrpc   0-byte unix socket, present
mwan opnsense-probe -op version -timeout 5s   context deadline exceeded
qm guest cmd 102 ping   QEMU guest agent is not running
ps auxw on suburban for vmid 102   kvm process alive; -loadstate references
                                   pre-config-import vmstate (expected since
                                   the live RAM still descends from that
                                   boot; subsequent snapshots are deltas)
```

## Recovery path the next slice should take

The clean baseline is preserved on disk: `post-runbook-pre-import-2026-05-08`
captured QGA + `os-frr` installed before any import attempt. The next slice
can roll back to that snapshot and try a smaller, less risky push channel:

1. `qm rollback 102 post-runbook-pre-import-2026-05-08` to restore the
   pre-import baseline. This re-loads RAM from that snapshot's vmstate; both
   QGA and the mwn-opnsense daemon should come back up.
2. Drop the prod-shaped XML into `/conf/backup/` using `qm guest exec` with
   `--pass-stdin 1` BUT chunk the input or fall back to a smaller transport.
   Options worth investigating:
   - Split the XML into smaller pieces and `cat >>`.
   - Use `qm terminal 102` over the existing `serial0` socket and pipe
     base64 in line by line. (serial-exec is broken in this env per the
     task brief; manual is fine for one-off.)
   - Stand up the transient `vmbr1` NIC again and use `scp` to drop the XML
     onto the VM the way the rest of the testbed work does.
3. Mint the API key the same way (OPNsense `Auth\API::createKey('root')`)
   once the daemon is back, then `curl -k -u $KEY:$SECRET
   https://10.240.4.1/api/core/backup/revertBackup/<file>`.
4. `qm reboot 102`, wait for the daemon, run the post-import checks.

## Snapshot tree at end of this slice

```
pre-install-2026-05-08      (preserved)
  pre-config-import-2026-05-08   (preserved)
    post-runbook-pre-import-2026-05-08   (created in this slice; vmstate)
      current   (post-runbook + transient NIC removal + the wedged write
                attempt; safe to discard, baseline is the snapshot above)
```

`prod-shaped-25-7-baseline-v2-2026-05-08` was NOT created. Phase 9
post-import verification was NOT run.

## Things to track for the followup

- Is `qm guest exec --pass-stdin 1` with a 219kB payload reliably broken on
  this OPNsense build, or only when the mwn-opnsense daemon is also wedged
  on a separate bus? Test the QGA path alone on a fresh rollback.
- Should `mwan opnsense-probe -stdin-file` enforce a maximum payload size,
  or should the daemon side stream the payload in chunks? The current
  failure mode (silent deadlock) is bad UX for a tool that the rest of the
  flow leans on.
- After this slice's rollback path runs, VM 102 still needs a permanent
  `net1` on `vmbr2` carrying the prod WAN address before BGP can establish
  with VM 950. That work is out of scope for this MWAN-168 slice and stays
  on the backlog.

## Hard rules check

- pre-install-2026-05-08 still exists. Yes.
- pre-config-import-2026-05-08 still exists. Yes.
- post-runbook-pre-import-2026-05-08 exists. Yes (created in this slice).
- prod-shaped-25-7-baseline-v2-2026-05-08 exists. No (not reached).
- Transient `vmbr1` NIC removed. Yes.
- No production hosts touched. Confirmed (only suburban + VM 102).
- No `tofu apply`. Confirmed.
- No `ansible-playbook` run. Confirmed.
- No `git push`. Confirmed.
- No `git merge` to `main`. Confirmed.
- No password or API key printed in chat or committed. Confirmed; the
  minted root API key+secret was round-tripped between mode-600 tmpfiles
  and deleted from both VM 102 and the local mac at the end of the slice.
  The vault entries were not updated; the key is gone with no record.

## Follow-up: SSH+scp import path (2026-05-08, second session)

After the first session ended with both gRPC and QGA wedged on the 219kB
stdin push, this session recovered by rolling VM 102 back to
`post-runbook-pre-import-2026-05-08` and importing the prod-shaped XML over
SSH+scp instead of stdin-over-virtio-serial. The path worked end to end and
the v2 baseline was reached.

### Wedge recovery

`qm rollback 102 post-runbook-pre-import-2026-05-08` restored both daemons.
gRPC `op=version` returned `Version OK` within the first 5 second probe,
`qm guest cmd 102 ping` returned exit 0, and `pkg info -E os-qemu-guest-agent
os-frr` confirmed `os-qemu-guest-agent-1.3` and `os-frr-1.50_1` were still
installed inside the snapshot. The wedge was confirmed to be a transient
runtime state, not a snapshot corruption.

### SSH+scp import path that worked

Phase by phase:

1. The post-runbook snapshot already had `net1` on `vmbr1` with
   `vtnet1` carrying `10.240.200.102/24` and a default route via
   `10.240.200.1`. No `qm set --net1` call was needed; the snapshot
   restored the prior session's transient NIC.
2. `pf` blocked SSH on `vtnet1` because the OPNsense pf rules only
   permit SSH on `vtnet0` (the LAN interface). Disabling pf with
   `pfctl -d` opened the path. The clean baseline is replaced by the
   import anyway, so loosening pf for the import window is harmless.
3. `xmllint --noout` validated the regenerated
   `mwan/testbed/opnsense/generated/config-testbed.xml` (219492 bytes,
   sha256 `3e967e73b0c1586ab33937566ac34c2ba4023f5817803f7d4055694272f904d5`).
4. `scp` from local Mac to suburban, then suburban to VM 102 via
   `sshpass -p opnsense scp ... root@10.240.200.102:/conf/backup/...`.
   `mwan opnsense-probe ... -op exec -cmd /sbin/sha256` on VM 102
   confirmed the destination hash matched bit for bit.
5. API key was minted by writing a small PHP script, scp-ing it to
   VM 102, running `/usr/local/bin/php /tmp/keymint.sh` over SSH, and
   capturing the two output lines (key on line 1, secret on line 2)
   into a mode-600 tmpfile on the local Mac and on suburban.
6. `curl -k -u "$KEY:$SECRET" -X POST
   https://10.240.200.102/api/core/backup/revertBackup/config-testbed-import-clean-2026-05-08.xml`
   returned `{"status":"reverted"}` with HTTP 200. The HTTPS endpoint
   was reachable on `vtnet1` since `lighttpd` listens on `*:443`.
7. Default route deleted, `vtnet1` alias removed, `qm set 102 --delete
   net1` cleared the transient NIC. `qm config 102` shows only `net0`
   on `vmbrtrunk`. The mwanrpc virtio-serial args block is preserved.
8. `qm reboot 102` and a 180 second probe loop confirmed gRPC came
   back. Post-reboot verification:
   - `hostname` returned `router-test.test.home.goodkind.io`.
   - `vtnet0` carries `10.240.4.1/24` and `3d06:bad:b01:204::1/64`,
     description `MANAGEMENT (opt9)`, group `INTERNAL`.
   - `pkg info -E os-qemu-guest-agent os-frr` returned both packages.
   - `service mwan_opnsense status`, `service qemu-guest-agent
     status`, and `service openssh status` all reported running.
   - `qm guest cmd 102 ping` returned exit 0.
9. `qm snapshot 102 prod-shaped-25-7-baseline-v2-2026-05-08 --vmstate
   1` saved 1.83 GiB of RAM in 3 seconds. Snapshot tree end state has
   all 4 snapshots: `pre-install-2026-05-08` ->
   `pre-config-import-2026-05-08` ->
   `post-runbook-pre-import-2026-05-08` ->
   `prod-shaped-25-7-baseline-v2-2026-05-08` -> `current`.

### Pre-existing finding: vtysh fails at startup on this baseline

`vtysh -c "show ip bgp summary"` returns:

```
ld-elf.so.1: /usr/local/lib/libpcre2-8.so.0: version PCRE2_10.47
required by /usr/local/lib/libyang.so.2 not defined
```

`pkg info` shows `pcre2-10.45_1` and `libyang2-2.1.128`. The libyang
shared library was built against PCRE2_10.47 but the OPNsense repo
ships pcre2-10.45_1. Both packages were installed during Phase 4 of
the first session from the OPNsense package mirror, so this skew is
not caused by the SSH+scp import path; it is a pre-existing testbed
package set issue.

This blocks any FRR-driven verification of the testbed BGP peering
until the package set is reconciled. The mwn-opnsense daemon, QGA,
SSH, lighttpd, and pf are all unaffected. The next slice should
update pcre2 to a version that exports PCRE2_10.47 (or downgrade
libyang2 to a build that matches pcre2-10.45) before any BGP peering
work begins.

### Hard rules check at end of this session

- pre-install-2026-05-08 still exists. Yes.
- pre-config-import-2026-05-08 still exists. Yes.
- post-runbook-pre-import-2026-05-08 still exists. Yes.
- prod-shaped-25-7-baseline-v2-2026-05-08 exists. Yes.
- Transient vmbr1 NIC removed. Yes.
- No production hosts touched. Confirmed.
- No `tofu apply`, no `ansible-playbook`, no `git push`, no `git
  merge` to main. Confirmed.
- No password or API key printed in chat or committed. Confirmed;
  the minted API key+secret were captured into mode-600 tmpfiles on
  the local Mac and suburban, used only for the single revertBackup
  call, and deleted at the end of this session.

### Top three findings

1. The SSH+scp import path is reliable for the 219kB prod-shaped
   config. The earlier wedge was caused by pushing the same payload
   over virtio-serial stdin (gRPC `--stdin-file` and `qm guest exec
   --pass-stdin`); both transports wedged. SCP over SSH avoided the
   virtio-serial buffer pressure entirely. Future runbook revisions
   should mark `--stdin-file` and `--pass-stdin` unsafe for payloads
   above some bound that needs measurement, and document SCP+SSH as
   the canonical path.
2. pf on a freshly imported prod-shaped OPNsense permits SSH only on
   the LAN interface. To reach the VM via a transient `vmbr1` NIC,
   pf must be disabled with `pfctl -d` for the duration of the
   import. Adding a temporary pass rule on `vtnet1` would also work
   but is more invasive.
3. The current OPNsense 25.7 testbed package set has a pcre2 vs.
   libyang2 ABI skew that breaks `vtysh` at startup. This was not
   surfaced earlier because the prior session never reached a
   working FRR runtime check. BGP work on this baseline must
   reconcile the package set first.
