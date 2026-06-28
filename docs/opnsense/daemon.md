# OPNsense OOB daemon

`mwan-opnsense` is the out-of-band (OOB) control channel into the production OPNsense
router. It exists so you can reach OPNsense when its network stack is down. Treat it as a
break-glass lever that must work 100% of the time, because the one moment you need it is
the moment nothing else works.

This document explains why the daemon is built the way it is. The code is the source of
truth for what it does; this doc records the durable reasons so a change is never made
blind. For the live production role and topology, see [production OPNsense state](../infra/opnsense.md)
and [emergency out-of-band access](../infra/oob.md).

## Why it exists

The daemon gives you gRPC-style access to OPNsense over a serial channel, with no
dependency on the network stack.

- No SSH. SSH breaks across upgrades and depends on the very netstack that may be down.
- No package manager. The daemon ships as a single self-contained ELF binary, dropped onto
  the guest from the Proxmox host.
- Rate its blast radius by the failure it exists for, not by normal operation. In an
  outage every other path is gone and this serial daemon is the only way into OPNsense.

A multi-year roadmap rides on this channel, so its reliability during a failure is the
whole design goal.

## Topology

Three processes connect the operator to the OPNsense config.

1. A probe (the `mwan` monolith) dials a local unix socket on the Proxmox host.
2. The host bridge (`mwan-opnsense-host`, a systemd service with `Restart=always`) forwards
   that connection over a yamux session to the guest, across the qemu virtio-serial chardev.
3. The daemon (`mwan-opnsense` on FreeBSD) serves gRPC multiplexed over that yamux session,
   reading and writing `/conf/config.xml` and running guest commands.

The host-side bridge lives in `cmd/mwan/opnsense_host.go`. The guest-side serve loop lives
in `internal/opnsensesvc/serve.go`. The wire contract is `proto/mwan/v1/mwan_opnsense.proto`.

## The serial transport and its invariants

The transport is qemu virtio-serial, because FreeBSD has no vsock host support yet. The
device is a FreeBSD tty, which fights binary traffic in several ways, so the code holds a
set of hard-won invariants. Each one prevents a wedge that the May 2026 bring-up fought for
days. Do not change them without reading this section.

- The persistent file descriptor (fd) is never closed during daemon life. `serialStream.Close`
  in `serial_listener.go` is a deliberate no-op so the fd outlives every yamux session; the
  serve loop rebuilds the yamux and gRPC layers over the same chardev when a session ends.
  The fd is closed only when the daemon exits.
- The device opens in raw mode with software flow control off. Arbitrary ELF bytes contain
  the XON and XOFF control codes, so leaving `IXON` or `IXOFF` set lets the tty eat binary
  payload and hang a transfer. See `serial_open_freebsd.go`.
- Each write is capped at 8 KiB. The FreeBSD tty input queue is sized from the baud rate; a
  single write larger than the queue silently loses its tail. The cap and the chunking loop
  live in `serial_listener.go`.
- The transfer protocol paces with stop-and-wait acknowledgements, one chunk outstanding at
  a time, so the qemu virtio receive queue cannot overflow. See `internal/opnsensesvc/transfer.go`.
- The daemon runs `TIOCFLUSH` on open to drop bytes left in the tty queue by a prior
  session. The driver does not flush on close, so without this the leftover frames corrupt
  the next session's handshake. See `serial_open_freebsd.go`.
- The daemon binds the named virtio port `io.goodkind.mwan-opnsense.0`, never the raw device
  `/dev/ttyV0.1`. The QEMU Guest Agent (QGA) shares the raw device numbering, so binding the
  raw path lets the two collide and steal each other's port. The named port also survives
  slot renumbering across reboots.

The deeper driver rationale (no host-disconnect signal, the tty queue draining only at fd
refcount zero) comes from the MWAN-95 design investigation. The repo code embodies the
resulting behavior; the strongest local proof is the stop test, where the serial read
provably never returns on its own.

## Transport history

The transport has been reversed twice and is best understood as hard-won, not arbitrary.

1. A TCP-listener-style model opened and closed the device per session. It broke on the
   second session because of the tty close-flush and a qemu open-event race.
2. A custom MWN1 framing layer (length-prefixed, correlation-id multiplexed) was proven in a
   proof of concept, then reverted.
3. The live transport is yamux over one persistent fd, with gRPC multiplexed on top. It
   landed in commit `91fcbe2` on 2026-05-12 and has been stable since.

The long-term direction is AF_VSOCK once FreeBSD supports it, which would delete this whole
serial apparatus.

Note for anyone reading the May transcripts: a real self-`exec` re-exec existed in the MWN1
era and was removed in the yamux pivot. The current daemon does not re-exec, and the proto
has no re-exec field. Do not reintroduce it from old material.

## Lifecycle and the self-deploy round-trip

The daemon starts from rc.d, serves until its context is cancelled, then exits. The success
criterion for any stop or self-deploy is the round-trip: the lever must come back up and
serve the intended binary, and a bad binary must auto-revert. "Exits cleanly and stays down"
is a failure for a break-glass lever.

- Startup. The rc.d script templates the daemon config, runs the preflight check, then
  starts the daemon under FreeBSD `daemon(8)`. The serve loop opens the serial device once.
- Self-deploy. The operator pushes a new binary over the transfer service, stages it (an
  atomic swap of the `.current` and `.previous` slots plus a pending-verify marker), then
  asks the daemon to restart. The daemon exits and is respawned onto the new binary. The
  rc.d preflight reverts to `.previous` unless the daemon stamped `health=ok`, so a bad
  deploy heals itself. Config writes are atomic, so a hard exit never leaves a half-written
  config. See `internal/opnsensesvc/deploy.go` and `server.go`.
- Restart. `RestartDaemon` cancels the serve context; the daemon exits and the supervisor
  respawns it. There is no in-process re-exec.

## The stop-orphan defect and its fix

The daemon could hang on stop and orphan its child process, which is the defect this work
fixes. The chain: on stop the serve context is cancelled, the shutdown path calls the yamux
`Session.Close`, and `Session.Close` blocks waiting for the read loop to exit. The read loop
is parked in a blocking serial read that never returns, because the driver gives no
disconnect signal. The process hangs, rc.d times out and sends `SIGKILL` to the `daemon(8)`
supervisor, and the wedged child survives holding the serial device.

The fix has two coupled halves.

1. Clean bounded exit. On terminal exit the daemon closes the real serial fd to unblock the
   read loop, bounds the graceful stop, and forces the process to exit as a last resort. The
   normal path still returns cleanly so no cleanup is skipped.
2. Respawn with preflight. The rc.d script supervises with `daemon -r` over a small shim that
   runs the preflight then execs the daemon, so a restart round-trips and a bad self-deploy
   auto-reverts. The forced stop kills the process group so a wedged child can never orphan.

## Session recovery after a host-side disruption

When the host bridge or the chardev drainer restarts or dies mid-transfer, the guest keeps
its serial fd open and its old yamux session live, because the byte stream never breaks. The
host re-dials and opens a fresh yamux session over the same stream. The guest's old session
reads the new session's first framing as a collision and ends with the error
`grpc serve: duplicate stream initiated`, and the serve loop rebuilds. Recovery is driven by
that collision event, not by a timer, and is observed to complete in under five seconds
across testbed accumulation runs (162 genuine kill-mid-transfer trials, zero wedges, zero
stalls).

This is why no liveness frames are added to the serial stream and yamux keepalive stays off
(see [What not to touch](#what-not-to-touch)). Recovery does not depend on the host bridge
heartbeat firing; the heartbeat is a backstop, and the common path is the immediate
duplicate-stream rebuild.

To inspect the guest's serial reads and writes at the syscall level, run dtrace on the
OPNsense guest with `-xnolibs`. The bundled dtrace libraries (`ip.d`, `socket.d`) fail to
compile on this kernel for lack of CTF, so the default invocation aborts; `-xnolibs` skips
the libraries and the `syscall` provider still works.

## What not to touch

These are the load-bearing invariants. Changing any of them re-opens a documented wedge.

- The no-op `serialStream.Close` and the never-close-during-life rule.
- The termios setup (`VMIN=1`, `VTIME=0`, `IXON` and `IXOFF` cleared).
- The 8 KiB write cap and the transfer ack pacing.
- The yamux tuning and the keepalive-off setting.
- The named virtio port; never bind the raw `/dev/ttyV0.1`.
- The `TIOCFLUSH`-on-open and the session-rebuild loop.

## Operating it

Two guides cover the daemon end to end:

- [install](install.md) brings up OPNsense and the daemon on a fresh VM.
- [operations](operations.md) covers updating the binary, the firmware-upgrade use case,
  and recovering a wedged channel.
- [wedge](wedge.md) explains the mid-transfer write wedge: its trigger, the evidenced
  mechanism, recovery, and how to inspect a wedged guest with a kernel-debug build.

## Further reading

The build-up and the prior wedges are tracked under the MWAN project: the v0 epic (MWAN-90),
the serial listener (MWAN-95), the wedge tickets (MWAN-184, MWAN-116, MWAN-113, MWAN-114),
the post-rollback redial (MWAN-178), and the round-trip fix this doc accompanies (MWAN-3).
