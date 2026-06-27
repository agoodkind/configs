package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/activation"
	"github.com/coreos/go-systemd/v22/daemon"
)

const (
	// drainBufSize is the per-read buffer for both relay directions.
	drainBufSize = 64 * 1024
	// drainReconnectBackoff paces chardev re-dials while the VM is down.
	drainReconnectBackoff = 2 * time.Second
)

// drainHub holds the current client and chardev connections. Both sides have
// independent lifecycles: the bridge (client) reconnects on its own schedule,
// and the chardev is re-dialed only when the VM restarts. The pumps read the
// current peer from the hub on each iteration, so a swap on either side is
// picked up without tearing the other side down.
type drainHub struct {
	mu      sync.Mutex
	client  net.Conn
	chardev net.Conn
}

func (h *drainHub) setClient(c net.Conn) {
	h.mu.Lock()
	old := h.client
	h.client = c
	h.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func (h *drainHub) getClient() net.Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.client
}

func (h *drainHub) clearClient(c net.Conn) {
	h.mu.Lock()
	if h.client == c {
		h.client = nil
	}
	h.mu.Unlock()
}

func (h *drainHub) setChardev(c net.Conn) {
	h.mu.Lock()
	h.chardev = c
	h.mu.Unlock()
}

func (h *drainHub) getChardev() net.Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.chardev
}

// runOPNsenseHostDrain runs the host-side chardev drainer. It holds the qemu
// virtio-serial chardev open and always reads it, so a bridge restart never
// disconnects the host side and strands a guest write in the kernel (see
// docs/opnsense/wedge.md). The bridge dials the relay socket this serves in
// place of the chardev. The chardev connection survives a drainer restart via
// the systemd file descriptor store, so a deploy opens no wedge window.
func runOPNsenseHostDrain(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			fmt.Fprintln(os.Stdout, "usage: mwan opnsense host drain")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Reads chardev/listen from [opnsense.drain] in TOML. Holds the qemu")
			fmt.Fprintln(os.Stdout, "chardev open and relays it to the bridge over the listen socket.")
			return 0
		}
	}
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "mwan opnsense host drain: unexpected arguments: %v\n", args)
		return 2
	}

	cfg, err := loadOpnsenseConfig()
	if err != nil {
		return printAndExit("host drain", err)
	}
	chardevTarget, err := requireDrainChardev(cfg)
	if err != nil {
		return printAndExit("host drain", err)
	}
	listenPath, err := requireDrainListen(cfg)
	if err != nil {
		return printAndExit("host drain", err)
	}
	chardevPath, ok := unixPath(chardevTarget)
	if !ok {
		return printAndExit("host drain", fmt.Errorf("[opnsense.drain].chardev must be unix:///abs/path"))
	}

	log := slog.Default()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Fds passed by systemd: the socket-activated relay listener (name
	// "relay") and, after a restart, the live chardev connection from the fd
	// store (name "chardev"). The map is empty when not run under systemd.
	byName := activation.FilesWithNames()

	ln, err := acquireListener(ctx, log, byName, listenPath)
	if err != nil {
		return printAndExit("host drain", fmt.Errorf("acquire listener: %w", err))
	}
	// Closing ln on return (after runDrainRelay sees ctx cancelled) unblocks
	// acceptLoop's Accept, so no separate ctx-watcher goroutine is needed.
	defer func() { _ = ln.Close() }()

	// Signal readiness once the relay socket is up, independent of the chardev.
	// The drainer can accept the bridge while it re-dials a chardev whose VM is
	// down, so a Type=notify start must not hang waiting for the VM to boot.
	_, _ = daemon.SdNotify(false, daemon.SdNotifyReady)

	// openChardev adopts the systemd-reclaimed chardev on the first call, then
	// dials fresh and re-stores on later re-opens (a VM restart).
	first := true
	openChardev := func(c context.Context) (net.Conn, error) {
		var reclaimable map[string][]*os.File
		if first {
			reclaimable = byName
		}
		conn, adopted, err := acquireChardev(c, log, reclaimable, chardevPath)
		first = false
		if err != nil {
			return nil, err
		}
		if !adopted {
			// A failed fdstore upload is non-fatal: the drainer still serves,
			// it just loses the zero-window hand-off on its next restart.
			// storeChardevFD logs the failure.
			_ = storeChardevFD(c, log, conn)
		}
		return conn, nil
	}

	log.InfoContext(ctx, "opnsense drain: serving", "chardev", chardevPath, "listen", listenPath)
	runDrainRelay(ctx, log, ln, openChardev)
	return 0
}

// runDrainRelay owns the relay. It accepts bridge clients on ln and keeps a
// chardev connection (from openChardev) continuously drained, re-opening the
// chardev when a session ends. It returns when ctx is cancelled. The chardev
// transport detail lives in openChardev, so this loop is the same under systemd
// and in tests.
func runDrainRelay(ctx context.Context, log *slog.Logger, ln net.Listener, openChardev func(context.Context) (net.Conn, error)) {
	hub := &drainHub{}
	spawn(ctx, log, "drain accept", func() { acceptLoop(ctx, log, hub, ln) })
	for {
		if ctx.Err() != nil {
			return
		}
		chardev, err := openChardev(ctx)
		if err != nil {
			log.WarnContext(ctx, "opnsense drain: chardev unavailable; retrying", "err", err)
			if !sleepCtxOK(ctx, drainReconnectBackoff) {
				return
			}
			continue
		}
		hub.setChardev(chardev)
		serveErr := drainChardev(ctx, log, hub, chardev)
		hub.setChardev(nil)
		_ = chardev.Close()
		if ctx.Err() != nil {
			return
		}
		log.WarnContext(ctx, "opnsense drain: chardev session ended; re-opening", "err", serveErr)
		if !sleepCtxOK(ctx, drainReconnectBackoff) {
			return
		}
	}
}

// acquireListener prefers the socket-activated relay listener passed by systemd
// (fd name "relay") and falls back to binding the path directly when not run
// under systemd (manual runs and tests). A reclaimed unix listener must not
// unlink its path on Close, since systemd owns it.
func acquireListener(ctx context.Context, log *slog.Logger, byName map[string][]*os.File, listenPath string) (net.Listener, error) {
	if fs := byName["relay"]; len(fs) > 0 {
		ln, err := net.FileListener(fs[0])
		_ = fs[0].Close()
		if err != nil {
			log.ErrorContext(ctx, "opnsense drain: socket-activated listener unusable", "err", err)
			return nil, fmt.Errorf("file listener: %w", err)
		}
		if ul, ok := ln.(*net.UnixListener); ok {
			ul.SetUnlinkOnClose(false)
		}
		log.InfoContext(ctx, "opnsense drain: adopted socket-activated relay listener")
		return ln, nil
	}
	if err := os.Remove(listenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.ErrorContext(ctx, "opnsense drain: clear stale relay socket", "path", listenPath, "err", err)
		return nil, fmt.Errorf("clear stale relay socket %s: %w", listenPath, err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", listenPath)
	if err != nil {
		log.ErrorContext(ctx, "opnsense drain: bind relay listener", "path", listenPath, "err", err)
		return nil, fmt.Errorf("listen %s: %w", listenPath, err)
	}
	if err := os.Chmod(listenPath, 0o600); err != nil {
		_ = ln.Close()
		log.ErrorContext(ctx, "opnsense drain: chmod relay listener", "path", listenPath, "err", err)
		return nil, fmt.Errorf("chmod %s: %w", listenPath, err)
	}
	log.InfoContext(ctx, "opnsense drain: bound relay listener", "path", listenPath)
	return ln, nil
}

// acquireChardev adopts the live chardev connection reclaimed from the fd store
// (name "chardev") when present, else dials the chardev fresh. The bool reports
// whether the connection was adopted, so the caller stores a freshly dialed one
// but does not re-store an adopted one.
func acquireChardev(ctx context.Context, log *slog.Logger, byName map[string][]*os.File, path string) (net.Conn, bool, error) {
	if fs := byName["chardev"]; len(fs) > 0 {
		c, err := net.FileConn(fs[0])
		_ = fs[0].Close()
		if err == nil {
			log.InfoContext(ctx, "opnsense drain: adopted reclaimed chardev fd")
			return c, true, nil
		}
		log.WarnContext(ctx, "opnsense drain: reclaimed chardev fd unusable; dialing fresh", "err", err)
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, false, fmt.Errorf("dial chardev %s: %w", path, err)
	}
	log.InfoContext(ctx, "opnsense drain: dialed chardev")
	return c, false, nil
}

// storeChardevFD uploads the chardev connection fd to the systemd file
// descriptor store so it survives a drainer restart. It reads the fd without
// net.Conn.File, which would force the connection into blocking mode. FDPOLL is
// left at the default so systemd auto-prunes the entry when the chardev closes
// on POLLHUP (the VM went down); a live fd is never pruned and is passed back
// to the next instance.
func storeChardevFD(ctx context.Context, log *slog.Logger, c net.Conn) error {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		err := fmt.Errorf("chardev conn is %T, not *net.UnixConn", c)
		log.ErrorContext(ctx, "opnsense drain: fdstore upload", "err", err)
		return err
	}
	rc, err := uc.SyscallConn()
	if err != nil {
		log.ErrorContext(ctx, "opnsense drain: fdstore upload", "err", err)
		return fmt.Errorf("chardev syscall conn: %w", err)
	}
	var sendErr error
	ctlErr := rc.Control(func(fd uintptr) {
		sendErr = notifyWithFD(ctx, log, "FDSTORE=1\nFDNAME=chardev", int(fd))
	})
	if ctlErr != nil {
		log.ErrorContext(ctx, "opnsense drain: fdstore upload", "err", ctlErr)
		return fmt.Errorf("chardev rawconn control: %w", ctlErr)
	}
	if sendErr == nil {
		log.InfoContext(ctx, "opnsense drain: stored chardev fd in systemd fdstore")
	}
	return sendErr
}

// notifyWithFD sends a single fd to the systemd notify socket as SCM_RIGHTS
// ancillary data, alongside the state payload (for example "FDSTORE=1"). The
// go-systemd daemon helper cannot pass fds, so this hand-builds the message. It
// is a no-op when not run under systemd (NOTIFY_SOCKET unset).
func notifyWithFD(ctx context.Context, log *slog.Logger, state string, fd int) error {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return nil
	}
	name := sock
	if strings.HasPrefix(name, "@") {
		name = "\x00" + name[1:] // abstract namespace socket
	}
	// Send from an unbound datagram socket with sendmsg addressed to the notify
	// socket, carrying the fd as SCM_RIGHTS ancillary data. Go's net package
	// cannot autobind a unixgram socket, so this uses raw syscalls. This is the
	// canonical sd_notify-with-fds path.
	s, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		log.ErrorContext(ctx, "opnsense drain: notify socket", "err", err)
		return fmt.Errorf("notify socket: %w", err)
	}
	defer func() { _ = syscall.Close(s) }()
	syscall.CloseOnExec(s)
	dst := &syscall.SockaddrUnix{Name: name}
	if err := syscall.Sendmsg(s, []byte(state), syscall.UnixRights(fd), dst, 0); err != nil {
		log.ErrorContext(ctx, "opnsense drain: notify sendmsg", "err", err)
		return fmt.Errorf("notify sendmsg: %w", err)
	}
	return nil
}

// acceptLoop accepts bridge connections one at a time and pumps each one toward
// the current chardev. It runs for the life of the process and exits when the
// listener closes on context cancel.
// Panics in acceptLoop are recovered by its launch wrapper in runDrainRelay.
func acceptLoop(ctx context.Context, log *slog.Logger, hub *drainHub, ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			// A normal stop closes the listener on ctx cancel, so only an
			// Accept failure with the context still live is unexpected and
			// worth a log signal (the relay stops accepting after it).
			if ctx.Err() == nil {
				log.WarnContext(ctx, "opnsense drain: relay accept failed; stopping accept loop", "err", err)
			}
			return
		}
		hub.setClient(c)
		spawn(ctx, log, "drain client", func() { clientPump(hub, c) })
	}
}

// clientPump copies one client (the bridge) to the current chardev. It stops
// when the client errors or is superseded by a newer client. A failed chardev
// write drops the client, which reconnects; that never wedges, because only the
// chardev read side matters for completing guest writes.
func clientPump(hub *drainHub, c net.Conn) {
	defer func() {
		hub.clearClient(c)
		_ = c.Close()
	}()
	buf := make([]byte, drainBufSize)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			if dev := hub.getChardev(); dev != nil {
				// Lossless blocking write: a slow guest applies backpressure,
				// which is safe because the chardev stays connected. No write
				// deadline, since a partial write would corrupt the byte stream.
				if _, werr := dev.Write(buf[:n]); werr != nil {
					return
				}
			}
		}
		if err != nil || hub.getClient() != c {
			return
		}
	}
}

// drainChardev reads one chardev continuously and forwards to the attached
// client. The copy is lossless while a bridge is attached, so a transfer is not
// corrupted; with no bridge attached the data is discarded so a guest write
// still completes and never wedges. It returns when the chardev errors (VM
// down) or ctx is done.
func drainChardev(ctx context.Context, log *slog.Logger, hub *drainHub, chardev net.Conn) error {
	readErr := make(chan error, 1)
	spawn(ctx, log, "drain reader", func() {
		err := drainReader(ctx, log, hub, chardev)
		select {
		case readErr <- err:
		default:
		}
	})
	select {
	case <-ctx.Done():
		return nil
	case err := <-readErr:
		return err
	}
}

// drainReader reads the chardev and forwards each chunk to the attached client.
// A write to the client is lossless within drainWriteTimeout, which preserves
// transfer integrity. If the client write fails or stalls past the timeout, the
// client is dropped and the chunk is discarded, so a slow or dead bridge cannot
// stall the read and busy-spin the guest. With no client attached the chunk is
// discarded outright (the chardev is still drained, so the guest write
// completes). It returns the chardev's terminating error.
func drainReader(ctx context.Context, log *slog.Logger, hub *drainHub, chardev net.Conn) error {
	buf := make([]byte, drainBufSize)
	for {
		n, err := chardev.Read(buf)
		if n > 0 {
			if c := hub.getClient(); c != nil {
				// Lossless blocking write to the attached bridge: a slow bridge
				// applies backpressure (safe, the chardev stays connected); a
				// disconnected bridge errors and is dropped, then later chunks
				// drain to void so a guest write still completes. No write
				// deadline, since a partial write would corrupt the byte stream.
				if _, werr := c.Write(buf[:n]); werr != nil {
					hub.clearClient(c)
					_ = c.Close()
				}
			}
		}
		if err != nil {
			log.WarnContext(ctx, "opnsense drain: chardev read ended", "err", err)
			return fmt.Errorf("chardev read: %w", err)
		}
	}
}

// spawn runs fn in a goroutine with a recover that logs any panic, so one
// broken relay connection never crashes the drainer.
func spawn(ctx context.Context, log *slog.Logger, where string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.ErrorContext(ctx, "opnsense drain: goroutine panic",
					"where", where, "panic", r, "err", fmt.Errorf("panic: %v", r))
			}
		}()
		fn()
	}()
}
