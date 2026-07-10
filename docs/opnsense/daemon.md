# OPNsense out-of-band daemon

`mwan-opnsense` is the break-glass control channel into the production OPNsense router, and it reaches the router over a serial line with no dependency on the network stack. It exists for the one moment when nothing else works, so it must work every time. This page covers why it is built the way it is, the invariants that must not change, and how to operate it. The code is the source of truth for what it does; this page records the durable reasons so a change is never made blind.

## Why it exists

The daemon gives you gRPC access to OPNsense over a serial channel, so you can reach the router when its network stack is down. SSH cannot fill that role, because it breaks across firmware upgrades and depends on the very network that may be down. A package manager cannot either, so the daemon ships as one self-contained binary dropped onto the guest from the Proxmox host. Judge its blast radius by the failure it exists for: in an outage every other path is gone and this serial daemon is the only way in.

## Topology

Three processes connect you to the OPNsense config. A probe, the `mwan` monolith, dials a local Unix socket on the Proxmox host. The host bridge, `mwan-opnsense-host`, a systemd service that always restarts, forwards that connection over a multiplexed session across the qemu virtio-serial channel. The daemon, `mwan-opnsense` on FreeBSD, serves gRPC over that session, reading and writing `/conf/config.xml` and running guest commands.

## The serial transport and its invariants

The transport is qemu virtio-serial, because FreeBSD has no host support for virtual sockets. The device is a FreeBSD tty, which fights binary traffic in several ways, so the code holds a set of invariants that each prevent a wedge the bring-up fought for days. Do not change them without reading this section.

- The persistent file descriptor never closes while the daemon lives. Its close call is a deliberate no-op so the descriptor outlives every serial session, and the serve loop rebuilds the multiplexing and gRPC layers over the same channel when a session ends. The descriptor closes only when the daemon exits.
- The device opens in raw mode with software flow control off. Arbitrary binary bytes contain the flow-control control codes, so leaving flow control on lets the tty eat payload and hang a transfer.
- Each write is capped at 8 KiB. The FreeBSD tty input queue is sized from the baud rate, and a single write larger than the queue silently loses its tail.
- The transfer paces with stop-and-wait acknowledgements, one chunk outstanding at a time, so the virtio receive queue cannot overflow.
- The daemon flushes the tty queue on open, because the driver does not flush on close and leftover bytes from a prior session would corrupt the next handshake.
- The daemon binds the named virtio port, never the raw device. The guest agent shares the raw device numbering, so binding the raw path lets the two collide and steal each other's port, and the named port also survives slot renumbering across reboots.

### What not to touch

These are the load-bearing invariants, and changing any of them reopens a documented wedge: the no-op close and the never-close-while-alive rule, the raw-mode termios with flow control cleared, the 8 KiB write cap and the ack pacing, the multiplexer tuning with keepalive off, the named virtio port, and the flush-on-open with the session-rebuild loop.

## Lifecycle and self-deploy

The daemon starts from rc.d, serves until its context is cancelled, then exits. A stop or self-deploy succeeds only on the round-trip: the lever comes back up and serves the intended binary, and a bad binary auto-reverts. Exiting cleanly and staying down is a failure for a break-glass lever.

On self-deploy you push a new binary over the transfer service, stage it as an atomic swap of the active and previous slots plus a pending-verify marker, then ask the daemon to restart. The daemon exits and is respawned onto the new binary, and the rc.d preflight reverts to the previous slot unless the daemon stamped itself healthy, so a bad deploy heals itself. Config writes are atomic, so a hard exit never leaves a half-written config. On a clean stop the daemon closes the serial descriptor to unblock its read loop, bounds the graceful stop, and forces exit as a last resort, and it kills its process group so a wedged child can never orphan.

## Session recovery after a host-side disruption

When the host bridge or the chardev drainer restarts or dies mid-transfer, the guest keeps its serial descriptor and its old session open, because the byte stream never breaks. The host re-dials and opens a fresh session over the same stream, the guest's old session reads the new framing as a collision and ends, and the serve loop rebuilds. Recovery is driven by that collision, not by a timer, and completes in under five seconds. This is why the serial stream carries no liveness frames and the multiplexer keepalive stays off: recovery does not depend on a heartbeat, which is only a backstop.

To inspect the guest's serial reads and writes at the syscall level, run dtrace on the OPNsense guest with the no-libraries flag, because the bundled dtrace libraries fail to compile on this kernel and the default invocation aborts.

## Operate it

The exact command flags drift, so this section names the verbs and describes behavior. Run `mwan opnsense <verb> --help` for the current flags, which are the source of truth.

### Pick the right channel

The daemon is one of three out-of-band channels into OPNsense, and each fits a different job. Prefer the gRPC-over-serial daemon for short control-plane calls such as `version`, `state`, config reads, and the upgrade validation matrix, because it survives a packet-filter or routing break that would silently fail the guest agent. Use the QEMU guest agent for read-only probes. Use SSH over the privileged admin path for large file pushes and long command runs, which it carries better than the serial daemon. Fall back to the serial console as the kernel-level last resort, since it is the only signal that survives a kernel panic, a botched bootloader, or lost network state.

### Update the binary

You update the daemon over the same serial channel it serves, so the update works even when the network is down. Push the new binary to the staging slot, stage it by its hash to swap it into the active slot while keeping the previous binary as the rollback slot, restart onto it, then verify with the version and state calls. A new binary that never reports healthy auto-reverts on the next respawn, so a bad self-deploy heals itself, and `mwan opnsense daemon revert` reverts by hand.

### Upgrade the firmware

The `mwan opnsense upgrade` verb drives a firmware upgrade as a state machine, so each phase records an artifact and refuses to run out of order. The phases run in order: prepare takes a snapshot and captures the pre-upgrade state, execute runs the in-guest upgrade and reboots onto the new firmware, validate runs the post-upgrade check matrix against the pre-upgrade baseline, and commit or rollback either deletes the snapshot and locks the cycle or restores it and re-validates. Run the phases by hand the first time on a target so you see each artifact land, then use `mwan opnsense upgrade run` to chain them with auto-rollback on a hard failure.

Prefer the gRPC transport for the upgrade so the control path survives a routing break during the upgrade, while the Proxmox-host and LAN-client checks still run over SSH because the daemon does not proxy those surfaces. During the execute window, which runs ten to thirty minutes, watch the upgrade log the daemon streams, the OPNsense system log over SSH which drops during the reboot, and the serial console which is the only signal that survives a panic.

### Recover a wedged channel

The daemon can wedge under a heavy command or a large input payload. Recover in order of least disruption. First retry the wedging operation over SSH or the serial console, which do not share the daemon's state. Then reset the guest, which recovers the guest agent and usually the daemon. If the daemon stays down, reach the Proxmox host over its out-of-band tunnel and restart the service there. For a guest that will not return over any network path, attach the serial console and recover at the kernel level. After a rollback the guest reboots and the daemon starts with it, and it is reachable as soon as the serial socket is up.

To install OPNsense and the daemon on a fresh VM, read [install](install.md). For the wedge mechanism in depth, its trigger, evidenced cause, and how to inspect a wedged guest with a kernel-debug build, read [wedge](wedge.md).
