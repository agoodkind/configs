# MWAN-95 investigation: FreeBSD virtio_console semantics

Date started: 2026-05-04
Goal: definitive picture of `/dev/ttyV?.??` lifecycle behavior on FreeBSD so we can rewrite `serial_listener.go` from open-per-Accept to persistent-device-with-session-boundaries.

Source corpus locally checked out at `~/Sites/mwan95-investigation/`:
- `freebsd-src/sys/dev/virtio/console/` (driver source, sparse-checkout)
- `freebsd-ports/emulators/qemu/` (qemu-guest-agent rc.d + plist + message)

## Five questions to answer

1. How does FreeBSD `virtio_console(4)` surface host-side disconnect (HUP / POLLHUP / EVFILT_READ-with-EOF / read-returns-0)?
2. Does `tcflush(TCIOFLUSH)` actually drain the kernel buffer for virtio_console?
3. Can `kqueue` on the device fd detect host-side connect (data available) without polling?
4. How does qemu-guest-agent FreeBSD port handle session lifecycle?
5. Does opening with `O_NONBLOCK` change any of the above?

---

## Notes (in order encountered)

### Finding 1: FreeBSD virtio_console does NOT surface host-disconnect to user space

Read `freebsd-src/sys/dev/virtio/console/virtio_console.c` (1514 lines) in full.

The driver allocates each port as a tty via `tty_alloc_mutex` (line 1141) and registers it as `/dev/ttyV<unit>.<id>` via `tty_makedev` (line 1159). The tty ops are:

| Op | What it does |
|---|---|
| `vtcon_tty_open` (line 1395) | When GUEST userspace opens the tty: sends `VIRTIO_CONSOLE_PORT_OPEN value=1` to HOST. |
| `vtcon_tty_close` (line 1410) | When GUEST userspace closes the tty: sends `VIRTIO_CONSOLE_PORT_OPEN value=0` to HOST. |
| `vtcon_tty_outwakeup` (line 1423) | Drains tty output buffer to virtqueue. |

The control-channel handler `vtcon_ctrl_process_event` (line 854) handles host-to-guest events. When it receives `VIRTIO_CONSOLE_PORT_OPEN` (host signaling port state), it calls `vtcon_ctrl_port_open_event` (line 791-813). That function does exactly one thing: `vtcon_port_enable_intr(port)`. **It does NOT call any tty hangup primitive. It does NOT set any port-level "host disconnected" flag. It does NOT signal user space.**

Compare with Linux: `drivers/char/virtio_console.c` on Linux maintains `port->host_connected`, returns POLLHUP from `poll()` and 0 from `read()` when host disconnects. **This Linux behavior is not implemented on FreeBSD.**

Confirms by inspection: there is no `host_connected` field, no `tty_hangup` call, no SIGIO emission, no kqueue notification anywhere in the file related to host-side disconnect events.

### Implication for the redesign

The original plan said: "Host client disconnects: virtio_console signals HUP-equivalent (need to verify how FreeBSD's driver surfaces this)." Verified now: it does not surface this at all.

The "persistent-device with HUP-delineated sessions" model as designed is infeasible against the stock FreeBSD driver. We need a different session-boundary mechanism.

### tcflush analysis

`vtcon_port_in` (line 1300) reads from the in-virtqueue, pushes bytes into the tty discipline via `ttydisc_rint` (line 1322). Once bytes are in the tty discipline's input buffer they are owned by the tty layer (`sys/kern/tty*.c`).

`tcflush(fd, TCIFLUSH)` operates on the tty layer's input queue. It SHOULD clear stale bytes that have already been pushed through `ttydisc_rint`. But bytes still in the virtqueue (intr handler hasn't run yet) won't be cleared.

So tcflush alone is partial. To fully drain we would need:
1. `tcflush` to clear the tty input queue.
2. Some mechanism to wait until the virtqueue intr has been handled. Or repeated short non-blocking reads + flush in a loop until two consecutive flushes return zero bytes.

### Implication for "stale bytes from previous session" bug

When the daemon closes the device after session 1:
- Tty teardown calls `vtcon_tty_close` (line 1410), which sends `PORT_OPEN value=0` to host.
- Host's qemu chardev backend marks the guest port as closed. Qemu's behavior with bytes already buffered is qemu-specific (need to check).

When daemon reopens for session 2:
- `vtcon_tty_open` (line 1395) sends `PORT_OPEN value=1` to host.
- Host's qemu chardev backend resumes forwarding from the unix socket.
- If a client is already connected (or connects during the open), bytes start flowing.
- The first read may pull either the new client's HTTP2 preface OR stale bytes that were in the virtqueue from before the close. The relative timing decides which.

Without a way to tell the host "I am ready, do not send me anything stale", this race is structural to the current driver.

### What this rules in for the redesign

Given FreeBSD virtio_console has no host-disconnect signal, we are looking at three viable design directions:

1. **Patch the driver.** Add a tty hangup or POLLHUP path on `vtcon_ctrl_port_open_event` when value=0. Upstream patch, longer term. Needs custom kernel module shipped with our daemon.

2. **Application-layer session boundaries.** gRPC keepalive or our own length-prefixed framing on top of the byte stream. The daemon never closes the device. When gRPC sees an inactivity gap longer than a threshold, it treats the next bytes as a new session and resets state. Avoids the open-close-reopen race entirely.

3. **Open-close-reopen with explicit pre-handshake byte drain.** Stay with current architecture but add: after open, do non-blocking reads in a loop until empty, then tcflush, then hand to gRPC. Catches stale bytes deterministically. Cheapest fix; ugliest semantics.

Option 2 is most elegant if it can be made to work. Option 3 is a path of least resistance. Option 1 is correct but most expensive.

Need to investigate qemu-side behavior (does qemu drop bytes during host-disconnect-to-reconnect? does it buffer them?) before picking.

### Finding 2: qemu-side semantics from qemu/hw/char/virtio-console.c and virtio-serial-bus.c

Read `qemu/hw/char/virtio-console.c` (308 lines), `qemu/hw/char/virtio-serial-bus.c` (relevant sections), `qemu/chardev/char-socket.c` (relevant sections).

**Port type matters**: our chardev is attached as `virtserialport` (not `virtconsole`). For `virtserialport`, `qemu_chr_fe_set_handlers` registers `chr_event` callback. For `virtconsole`, `chr_event` is NOT registered (consoles ignore disconnect).

**Host-side disconnect path** (`virtio-console.c:151-174`): when the unix socket client disconnects, qemu fires `CHR_EVENT_CLOSED`. `chr_event` calls `virtio_serial_close(port)`. That function (`virtio-serial-bus.c:277-291`):
- Sets `port->host_connected = false`.
- **Discards any pending guest-to-host data**: `discard_throttle_data(port); discard_vq_data(port->ovq, ...)`. Bytes the daemon wrote that hadn't been delivered yet are LOST.
- Sends `PORT_OPEN value=0` to guest via control queue.

**Host-side connect path**: when a new unix socket client connects, qemu fires `CHR_EVENT_OPENED`. `chr_event` calls `virtio_serial_open(port)`. That function:
- Sets `port->host_connected = true`.
- Sends `PORT_OPEN value=1` to guest.

**Write gate** (`virtio_serial_write` at `virtio-serial-bus.c:294-301`):
```c
if (!port || !port->host_connected || !port->guest_connected) {
    return 0;
}
return write_to_port(port, buf, size);
```
If either side is disconnected, qemu returns 0 (drops the bytes). They are NOT buffered.

**Read gate** (`virtio_serial_guest_ready` at lines 307-323): returns 0 if `use_multiport && !guest_connected`. Used by chardev backend's `chr_can_read` to gate reading from the host socket. So when guest device is closed, qemu does NOT read from the unix socket (the OS unix-socket buffer holds the bytes until guest reopens).

**Single-client serialization** (`chardev/char-socket.c:460-477`): when a client disconnects, `tcp_chr_disconnect_locked` calls `qio_net_listener_set_client_func_full` to RE-INSTALL the accept callback. This implies the accept callback was NOT installed while a client was connected. Effect: qemu's chardev unix-socket server only handles ONE client at a time. New connect attempts block at the kernel listen queue until the current one disconnects. We do NOT have multi-client concurrency.

### Finding 3: putting it together. Why every-other-session fails

Sequence (fully understood now):

| t | Event | State |
|---|---|---|
| 0 | Probe N closes its end of unix socket | qemu socket disconnects |
| t1 | qemu fires CHR_EVENT_CLOSED | virtio_serial_close: host_connected=false, discards out-vq data, sends PORT_OPEN=0 to guest |
| t2 | qemu re-installs listener accept callback | Accept callback can fire when next client connects |
| t3 | Probe N+1's pending connect(2) gets accepted | qemu fires CHR_EVENT_OPENED |
| t4 | virtio_serial_open: host_connected=true, sends PORT_OPEN=1 to guest | Guest driver receives PORT_OPEN=1 in vtcon_ctrl_port_open_event. Only enables interrupts. Does NOT signal user space. |
| t5 | Probe N+1 starts sending HTTP2 preface to socket | qemu's `chr_can_read` returns 0 because guest_connected=false (daemon's Close already sent PORT_OPEN=0 from guest). Bytes accumulate in OS unix socket buffer (NOT lost) |
| meanwhile in guest: |||
| g1 | Daemon's stale-read-timeout fires after 1s of no input | gRPC handler exits with EOF |
| g2 | Daemon serialConn.Close runs: os.File.Close calls vtcon_tty_close | sends PORT_OPEN=0 from guest. qemu's port->guest_connected=false |
| g3 | FreeBSD tty close handler flushes tty input/output queues | **Any bytes sitting in tty input queue are discarded** |
| g4 | 1200ms post-close grace timer | Device closed during this window |
| g5 | Daemon Accept calls Open(/dev/ttyV0.1) | vtcon_tty_open sends PORT_OPEN=1 from guest. qemu's guest_connected=true |
| g6 | qemu's `chr_can_read` now returns >0 | qemu reads from unix socket OS buffer, forwards probe N+1's preface bytes via in-vq to guest |
| g7 | Guest in-vq interrupt: ttydisc_rint pushes bytes into tty input queue | First Read on the new daemon-side conn returns probe N+1's preface |

**This SHOULD work in steady state.** Reading the code, the bytes from probe N+1 are correctly buffered in the OS unix-socket buffer during the close window, and forwarded to the guest after the daemon reopens.

So WHY does it actually fail?

Two strong candidate root causes:

**Candidate A. Race window in step t1-g2 ordering.** If qemu processes CHR_EVENT_OPENED for probe N+1 BEFORE the daemon's Close has propagated PORT_OPEN=0 to qemu, then:
- t4 fires: host_connected=true, guest_connected=true (still from session N), PORT_OPEN=1 sent to guest.
- Guest's vtcon_ctrl_port_open_event runs: enables interrupts. Daemon is still mid-Close though.
- Probe N+1's bytes get forwarded immediately by qemu (both connected) into in-vq.
- Driver intr fires, ttydisc_rint pushes into tty input queue. Daemon may or may not have closed yet.
- If daemon hasn't closed: bytes get delivered to gRPC handler that's already exiting. They are READ but CONSUMED-AND-DROPPED by gRPC since the handler is on its way out.
- If daemon has closed (g2 ran first): tty close flush in g3 PURGES those bytes.

Either way, **probe N+1's preface bytes can be eaten before the new gRPC handler sees them**. The next reads get only what arrives AFTER the new daemon Open. But probe N+1's HTTP2 preface was a one-shot; gRPC's client doesn't know to retransmit it. The client just waits for the SETTINGS frame from server, never gets it, times out.

**Candidate B. Tty input queue lost during close.** Even if there is no race, FreeBSD's tty close handler in `sys/kern/tty.c` flushes input/output queues. This is a standard tty(4) behavior: close means session ends, drop pending input. Any bytes that arrived during the close window are gone.

Both candidates produce the same observed symptom. Both are caused by the same underlying issue: **the daemon's open-close-reopen cycle creates windows where in-flight bytes are destroyed**.

### Implications for the redesign

The "persistent device" model is now necessary AND sufficient if we can make gRPC tolerate it. The constraints from qemu's design help:

- qemu serializes clients on the unix socket. **There is never more than one logical session at a time on the wire.**
- qemu drops bytes when one side is disconnected. So bytes can't leak across sessions in the qemu layer.
- qemu's chardev backend correctly buffers OS-socket bytes until the guest is ready (no data loss on the qemu side).

The bug is entirely on the guest daemon side: the open-close-reopen cycle destroys bytes that were correctly delivered into the tty input queue.

### Three viable design patterns (revised)

**Pattern P1: persistent device, single long-lived gRPC server, length-prefixed message protocol replacing gRPC**

Drops gRPC. User said gRPC API is non-negotiable. NOT this.

**Pattern P2: persistent device, yamux-multiplexed gRPC**

- Daemon opens device once at startup, never closes.
- Daemon runs yamux server on the device fd.
- Each new gRPC client connection opens a yamux stream.
- gRPC server runs grpc.Serve on the yamux stream-as-listener.
- Each yamux stream is a fresh gRPC connection from the server's POV.

Pros: clean session semantics. Each client gets a fresh HTTP2 connection. No state corruption.
Cons: extra protocol layer. yamux is well-tested but adds dependency. Both daemon and probe must speak yamux.

**Pattern P3: persistent device, single long-lived gRPC server, single client treats reconnects as the same connection**

Daemon: open once, never close. Run grpc.Serve over the device once. The "connection" lasts forever.

Client: each `mwan opnsense-probe` opens a unix-socket conn. But the gRPC server treats that as continuous bytes appended to the existing HTTP2 stream. **This will not work**: HTTP2 is connection-oriented. Each "new" client would send the connection preface again, which the existing gRPC server would interpret as a protocol error.

Variant P3a: have the probe NOT do gRPC handshake on each invocation. Instead, the daemon's existing connection knows about RPCs without HTTP2 framing. This effectively means writing our own RPC framing. Same as P1, NOT this.

### Decision

**Pattern P2 (yamux-multiplexed gRPC)** is the right answer.

Why:
- Satisfies all four hard constraints (OOB, no SSH, no pkg, gRPC).
- Robust against the qemu-side serialization and the FreeBSD-driver-no-HUP issue.
- Multiple-client support comes for free (even if qemu-serialized, we get correct behavior).
- yamux is mature, pure-Go on both sides (server: github.com/hashicorp/yamux is the canonical impl), low overhead.
- Bytes from one yamux stream cannot leak into another stream's gRPC framing.
- Daemon never closes the device, eliminating the open-close-reopen race entirely.

Implementation shape for serial_listener.go:

1. Daemon startup: open `/dev/ttyV0.1` once with the FreeBSD termios setup (raw, CLOCAL, etc.).
2. Wrap the os.File as an `io.ReadWriteCloser`.
3. Create yamux server session over that ReadWriteCloser.
4. yamux session's `Accept()` returns a stream which is itself an `io.ReadWriteCloser` and implements `net.Conn`.
5. SerialListener.Accept now wraps yamux.Session: each Accept returns the next yamux stream.
6. Pass to gRPC's Serve. gRPC sees a sequence of clean, independent net.Conns.
7. Daemon shutdown: close yamux session, close device.

Probe (client) side:

1. Dial unix socket as today.
2. Wrap the unix conn as an `io.ReadWriteCloser`.
3. Create yamux client session over that.
4. Open one yamux stream.
5. Build grpc.ClientConn that uses a custom dialer returning that stream.
6. Run RPCs.
7. Close stream then close yamux session then close unix conn.

Cost: ~150 LOC in serial_listener.go (server side) plus ~50 LOC in opnsenseclient/client.go (client side). One new dependency: github.com/hashicorp/yamux.

Risk: yamux's bidirectional flow control and keepalives interacting with virtio-serial in unexpected ways. Mitigation: yamux has been deployed on weirder transports (gRPC, websockets). Should be fine.

### Finding 4: qemu-guest-agent FreeBSD design validates Pattern P2

Read `qemu/qga/channel-posix.c` `ga_channel_open` (lines 128-216) for the GA_CHANNEL_VIRTIO_SERIAL case.

What qga does on FreeBSD virtio-serial:

1. **Opens the device ONCE at startup** with `O_RDWR | O_NONBLOCK | O_ASYNC`.
2. **On FreeBSD: only disables ECHO** in termios. Does NOT do full cfmakeraw setup. Comment at line 158-162: "In the default state channel sends echo of every command to a client. The client program doesn't expect this and raises an error. Suppress echo by resetting ECHO terminal flag."
3. **Never closes the device during normal operation.** `ga_channel_client_close` (line 80-89) only runs on event-callback returning false. For virtio-serial mode `c->listen_channel` is NULL, so it doesn't re-listen after close. Effectively close means "stop entirely."
4. Uses glib's `g_io_add_watch(client_channel, G_IO_IN | G_IO_HUP, ...)` for I/O.
5. Application protocol (QMP, JSON-line-delimited) provides message boundaries.

**Pattern observation**: qga treats virtio-serial as a single perpetual connection. The application protocol (QMP) provides the framing. There is no notion of "session boundaries" at the transport layer.

This is exactly Pattern P2 (or its degenerate case Pattern P1 if you drop the gRPC requirement).

### Bonus: qga doesn't even bother with raw-mode setup on FreeBSD

Just disabling ECHO. This is interesting because our daemon does the full cfmakeraw equivalent + CLOCAL + clears HUPCL. None of that is necessary for correctness if we follow qga's model. qga's bytes are JSON; ours would be HTTP2 frames over yamux. Both are arbitrary binary. The driver's tty layer is in raw mode for virtio-console because the FreeBSD virtio_console driver doesn't apply line discipline (it uses `ttydisc_rint` which goes through the discipline, but the default `ttydisc` for virtio_console is a passthrough since there's no input editing for a non-interactive serial port).

We can keep our termios setup (defense in depth) but it's largely cosmetic.

---

## Final answers to the five investigation questions

| # | Question | Answer |
|---|---|---|
| 1 | Does FreeBSD virtio_console(4) surface host-disconnect? | **NO.** The driver's PORT_OPEN handler only enables interrupts. There is no tty hangup, no POLLHUP, no kqueue notification. Confirmed by reading `vtcon_ctrl_port_open_event` at virtio_console.c:791-813. |
| 2 | Does tcflush(TCIOFLUSH) drain the kernel buffer? | **Partially.** It drains the tty layer's input/output queues. Bytes still in the virtio in-virtqueue (not yet processed by `vtcon_port_in`) won't be cleared. To fully drain we would need tcflush PLUS repeated non-blocking reads until empty. Practically not worth doing if we follow Pattern P2 (where we never close the device). |
| 3 | Can kqueue detect host-side connect? | **Indirectly.** kqueue's EVFILT_READ on the device fd would fire when bytes arrive (i.e., when the host writes). It cannot signal "the host JUST connected with no data yet." Effectively the daemon detects host activity by reading bytes, not by a separate connect signal. |
| 4 | How does qga handle this? | **Persistent device, never closes during normal operation.** Application-level framing (JSON line-delimited) provides message boundaries. Confirmed by `qga/channel-posix.c:128-183`. |
| 5 | Does O_NONBLOCK change behavior? | qga opens with O_NONBLOCK + O_ASYNC. This makes reads return immediately if no data is available, and enables SIGIO-style notification (though qga uses glib watchers instead). Doesn't change the no-HUP-surfacing behavior. Useful for the persistent-device design pattern because it lets us drain without blocking. |

## Pattern selected: P2e (custom BEGIN/END framing over persistent device)

### Why not P2 (yamux)

I initially picked yamux but found a structural problem. yamux is a 1:1 client/server pairing per session. Once the daemon's yamux server is set up over the persistent device, all subsequent bytes from any client must be valid yamux frames within that single session. But each `mwan opnsense-probe` is a separate process. Each would need to set up its own yamux client session from scratch. That session-handshake from probe N+1 would arrive at the daemon's yamux server as garbage in the middle of the existing session. yamux protocol error.

Workaround would be to teach all probes to share a yamux client (impossible across processes without a sidecar daemon on the host side) or to restart the yamux server per host disconnect (impossible to detect without HUP).

So yamux is wrong for this exact use case.

### Pattern P2e: simple in-band framing instead of yamux

Daemon:
- Open device once at startup with `O_RDWR | O_NONBLOCK`. Apply minimal termios (ECHO off). Never close until daemon shutdown.
- Run a session-loop:
  - Read bytes until BEGIN_MARKER (8-byte magic value).
  - Wrap the fd as a session that delivers bytes until END_MARKER, then returns io.EOF to the caller.
  - Pass the session as `net.Conn` to gRPC server's serveConn (one-shot, not via Listener.Accept loop).
  - Wait for serveConn to return.
  - Loop.

Probe:
- Dial unix socket.
- Write BEGIN_MARKER.
- Run gRPC client handshake plus RPCs over the unix conn.
- After last RPC: write END_MARKER.
- Close unix conn.

Cost: about 80 LOC daemon-side, about 30 LOC probe-side. No new dependencies. Each session is fully isolated. The "first read after open" stale-bytes problem is sidestepped because the daemon scans for BEGIN_MARKER which is uniquely identifying (an 8-byte magic value will not appear naturally in stale HTTP2 bytes).

Edge cases:
- BEGIN/END markers in legitimate gRPC payload bytes: pick markers that cannot appear in HTTP2. HTTP2 frames have a defined structure. Certain byte patterns are not valid frames. We can pick a magic that is intentionally invalid as HTTP2.
- Probe crashes mid-session: daemon waits for END_MARKER that never arrives. Need a timeout. After N seconds without bytes, daemon assumes session abandoned, scans for next BEGIN.
- Daemon restart while probe is connected: probe's writes go into qemu's buffer (or are dropped per our finding). Probe should detect via gRPC error.
- Multiple probes try to connect: qemu serializes at unix socket level. Only one BEGIN/END pair per time on the device.

**This is the cleanest design.** But before committing, consider one more option that may dominate it.

### Pattern P2h: host-side bridge daemon

The structural problem with every previous pattern is that we have many short-lived gRPC client processes (probes) and one long-lived gRPC server (daemon on OPNsense), connected by a byte stream that lacks natural session boundaries. Multiplexing solves this by adding a layer.

A different approach: convert the many-short-lived-clients into one-long-lived-client by adding a small intermediary on the Proxmox host.

Architecture:
- `mwan-opnsense` (FreeBSD, OPNsense): opens `/dev/ttyV0.1` once. Runs vanilla `grpc.Server` on it. Never closes. Single connection over the persistent device.
- `mwan-opnsense-host` (Linux, Proxmox host): NEW small daemon. At startup, opens unix socket to `/var/run/qemu-server/101.mwanrpc` once. Runs grpc.NewClient on it. Holds that one ClientConn forever. Also listens on a NEW local unix socket (e.g., `/var/run/mwan-opnsense.sock`). On each incoming connection, forwards RPCs through the long-lived ClientConn to the OPNsense daemon.
- `mwan opnsense-probe` (anywhere): connects to `/var/run/mwan-opnsense.sock` instead of `/var/run/qemu-server/101.mwanrpc`. No changes to probe gRPC code. Just a different target.

What this eliminates:
- No HUP detection needed (host-side connection is persistent).
- No session-boundary framing needed (host-side is the only gRPC client, ever).
- No race conditions on disconnect/reconnect (no disconnect/reconnect at the virtio-serial layer).
- No custom multiplexer protocol.
- No new dependencies on either side.
- Both daemons are symmetric: long-lived on both ends of the virtio-serial.

What this adds:
- One small Go binary on the Proxmox host (~150 LOC).
- A new long-running systemd unit on each Proxmox host that hosts an OPNsense VM.
- Slight latency overhead (extra hop) but unmeasurably small for our use case.

Build/deploy: same shape as the existing OPNsense daemon. Cross-compile from macOS, scp to Proxmox host, install via systemd unit. Configurable via `/etc/mwan-opnsense-host.conf`.

**Decision: P2h is the right answer.** It satisfies all constraints, eliminates the failure mode entirely, and adds less code than any other pattern. The "extra daemon" is a feature, not a cost: it puts complexity on the Linux side where we have all the tooling.

### Summary of design decision

Design: P2h (host-side bridge daemon)

Components:
1. `mwan-opnsense` daemon (FreeBSD, OPNsense): persistent device, plain `grpc.Server`. Rewrite of current daemon to use one long-lived listener that returns one Conn for the lifetime of the daemon.
2. `mwan-opnsense-host` daemon (Linux, Proxmox host): NEW. Persistent gRPC client to virtio-serial chardev. Local unix socket listener. Per-incoming-connection ProxyServer that forwards RPCs.
3. `mwan opnsense-probe` (no changes to gRPC code, just target path config).

Cross-platform compile:
- mwan-opnsense: existing `make build-mwan-opnsense` (FreeBSD/amd64).
- host bridge: existing `make build-linux` monolith (Linux/amd64), invoked
  as `mwan opnsense-host serve`.
- Both fold through the existing `build_platform` macro.

Deployment:
- mwan-opnsense: scp to OPNsense as today. rc.d unit.
- mwan-opnsense-host: scp to Proxmox host. systemd unit. Ansible playbook.

Estimated effort:
- Step 3 (daemon rewrite): now means rewriting both daemons. About 5-7 hours.
- Step 4 (test rewrite): about 2 hours.
- Step 5 (testbed validation): about 30 minutes.

This investigation took about 90 minutes. The daemon-side complexity moved from "tricky byte-level race condition handling" to "trivially correct gRPC server over a single conn" with the cost being one extra small daemon.
