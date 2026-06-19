# Operate the OPNsense OOB daemon

This guide covers the day-to-day operations you run against the `mwan-opnsense`
out-of-band (OOB) daemon: update its binary, upgrade the OPNsense firmware through it,
and recover it when a channel wedges. For why the daemon is built the way it is, read
[OPNsense OOB daemon](daemon.md). To install it on a fresh VM, read [install](install.md).

Exact command flags drift, so this guide describes behavior and names the verbs. Run
`mwan opnsense <verb> --help` for the current flag set; that help text is the source of
truth.

## Pick the right channel

The daemon is one of three OOB channels into OPNsense, and each fits a different job.

- The gRPC-over-serial daemon is reliable for short calls: `version`, `state`, xpath
  reads, and the upgrade validation matrix. Prefer it for control-plane operations,
  because it survives a packet-filter or routing break that would silently fail the
  guest agent.
- The QEMU Guest Agent (QGA) is fine for read-only probes such as `qm guest exec`.
- SSH over the privileged admin path carries large file pushes and long exec runs better
  than the serial daemon does.
- The serial console is the kernel-level last resort. It is the only signal that survives
  a kernel panic, a botched bootloader, or lost network state.

## Update the daemon binary

You update the daemon over the same serial channel it serves, so the update works even
when the network is down. The flow is push, stage, restart, verify.

1. Push the new binary to the staging slot: `mwan opnsense daemon push <binary>`.
2. Promote it: `mwan opnsense daemon stage <sha256>`. This swaps the new binary into the
   active slot, keeps the previous binary as the rollback slot, and drops a
   pending-verify marker.
3. Restart onto it: `mwan opnsense daemon restart`. The daemon exits and the supervisor
   respawns it on the new binary.
4. Verify: `mwan opnsense daemon version` shows the new commit, and `mwan opnsense daemon
   state` shows the active and previous hashes and the health.

A new binary that never reports healthy auto-reverts to the previous binary on the next
respawn, so a bad self-deploy heals itself. To revert by hand, run `mwan opnsense daemon
revert`. The daemon stamps itself healthy once it serves cleanly, which clears the
pending-verify marker so later restarts keep the new binary.

## Use case: upgrade the OPNsense firmware

The `mwan opnsense upgrade` verb drives a firmware upgrade as a state machine, so each
phase records an artifact and refuses to run out of order. Run the phases by hand the
first time on a target, then use one-shot mode for repeat cycles.

The phases run in order.

1. `prepare`: take a Proxmox snapshot, capture the pre-upgrade config and routing state,
   and move the state to prepared.
2. `execute`: run the in-guest upgrade, stream its log, and reboot onto the new firmware.
3. `validate`: run the post-upgrade check matrix and compare it against the pre-upgrade
   baseline.
4. `commit` or `rollback`: commit deletes the snapshot and locks the cycle; rollback
   restores the snapshot and re-validates.

Use `mwan opnsense upgrade run` to chain prepare, execute, and validate under one set of
flags, with auto-rollback on a hard failure. Use it only after a manual cycle has
succeeded on that target, so you have seen each artifact land.

Prefer the gRPC transport for the upgrade so the control path survives a routing break
during the upgrade itself. Proxmox-host and LAN-client checks still run over SSH, because
the daemon does not proxy those surfaces.

Watch three signals during the execute window, which runs 10 to 30 minutes:

- The upgrade log the daemon streams to its state directory.
- The OPNsense system log over the admin SSH path, which drops during the reboot and
  returns when the guest is back.
- The serial console, which is the only signal that survives a panic or a failed reboot.

## Take snapshots without saved RAM

Always take testbed snapshots with `--vmstate 0`. A snapshot that includes RAM resumes on
rollback with a stale wall clock, dead TCP sockets, and a stale resolver cache, which
produced hours of confusing failures in past sessions. Production OPNsense never uses RAM
snapshots. The web GUI defaults RAM snapshots on for a running VM, so do not take
snapshots from the GUI for this work.

## Recover a wedged daemon

The daemon can wedge under a heavy exec or a large stdin payload. Recover in order of
least disruption.

1. Retry over SSH or the serial console for the operation that wedged it, since those
   channels do not share the daemon's state.
2. Reset the guest with `qm reset <vmid>`. This recovers the guest agent and usually
   recovers the daemon.
3. If the daemon stays down, reach the Proxmox host over the pinned OOB tunnel at
   `root@3d06:bad:b01:ff::1` and restart the service from there.
4. For a guest that will not come back over any network path, attach the serial console
   and recover at the kernel level.

After a `qm rollback`, the guest reboots and the daemon starts with it. The daemon is
reachable as soon as the virtio-serial socket is up, which is the same liveness signal the
upgrade validator waits on.
