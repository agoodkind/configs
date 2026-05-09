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
