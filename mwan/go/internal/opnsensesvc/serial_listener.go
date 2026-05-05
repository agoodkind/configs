package opnsensesvc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

// debugSerialIO when true logs first bytes of every serial Read/Write
// to help diagnose framing/protocol issues over virtio-serial.
var debugSerialIO = os.Getenv("MWAN_OPNSENSE_SERIAL_TRACE") == "1"

// SerialListener wraps a single character device (typically
// /dev/ttyV0.1) as a [net.Listener] for use by gRPC.
//
// Lifecycle model: persistent device. The device is opened ONCE in
// [NewSerialListener] and stays open for the lifetime of the listener.
// It is never closed-and-reopened, eliminating the close-reopen race
// that destroys in-flight bytes on virtio-serial.
//
// FreeBSD's virtio_console(4) driver does not surface host-side
// disconnect to user space (no HUP, no POLLHUP, no read-returns-zero
// on host disconnect). So we cannot delineate sessions by waiting for
// a kernel signal. Instead we use idle-read-timeout: when the bytes
// stop flowing for IdleTimeout, the current Conn returns [io.EOF],
// gRPC tears down its server-side state, and the next Accept returns
// a fresh Conn over the still-open device.
//
// The single-peer design assumes ONE host-side gRPC client at a time
// (the bridge daemon on Proxmox host that fans out probe RPCs over
// HTTP/2 stream multiplex). qemu's chardev backend serializes clients
// at the unix-socket layer, so this assumption holds even if multiple
// host processes try to dial concurrently.
type SerialListener struct {
	devPath     string
	rwc         io.ReadWriteCloser
	addr        serialAddr
	idleTimeout time.Duration
	nowFn       func() time.Time
	log         *slog.Logger

	mu            sync.Mutex
	cond          *sync.Cond
	closed        bool
	cur           *serialConn
	nextSessionID uint64
}

// NewSerialListener opens devPath via openFn and returns a Listener
// over the persistent device. The device is opened immediately and
// stays open for the listener's lifetime. Returns an error if openFn
// fails.
func NewSerialListener(devPath string, openFn func(path string) (io.ReadWriteCloser, error)) (*SerialListener, error) {
	if openFn == nil {
		return nil, errors.New("serial listener: openFn required")
	}
	rwc, err := openFn(devPath)
	if err != nil {
		return nil, fmt.Errorf("serial listener: open %s: %w", devPath, err)
	}
	l := &SerialListener{
		devPath:       devPath,
		rwc:           rwc,
		addr:          serialAddr(devPath),
		idleTimeout:   15 * time.Second,
		nowFn:         time.Now,
		log:           slog.Default(),
		mu:            sync.Mutex{},
		cond:          nil,
		closed:        false,
		cur:           nil,
		nextSessionID: 0,
	}
	l.cond = sync.NewCond(&l.mu)
	l.log.Info("serial listener: device opened", "path", devPath)
	return l, nil
}

// Accept returns a Conn wrapping the persistent device. Blocks while
// a previous Conn is still active; returns an error only when the
// listener itself is closed.
//
// The returned Conn shares the underlying [os.File] with the listener.
// Closing the Conn does NOT close the device; only Listener.Close
// does that.
func (l *SerialListener) Accept() (net.Conn, error) {
	log := l.logger()

	l.mu.Lock()
	for !l.closed && l.cur != nil {
		l.cond.Wait()
	}
	if l.closed {
		l.mu.Unlock()
		return nil, errors.New("serial listener: closed")
	}
	l.nextSessionID++
	sessionID := l.nextSessionID
	conn := &serialConn{
		rwc:         l.rwc,
		laddr:       l.addr,
		raddr:       l.addr,
		parent:      l,
		sessionID:   sessionID,
		idleTimeout: l.idleTimeout,
		nowFn:       l.nowFn,
		log:         log,
		closeOnce:   sync.Once{},
	}
	l.cur = conn
	l.mu.Unlock()

	log.Info("serial listener: session started",
		"path", l.devPath,
		"session_id", sessionID)
	return conn, nil
}

// Close releases the listener and closes the underlying device. Any
// active Conn is signaled to return EOF on its next Read.
func (l *SerialListener) Close() error {
	log := l.logger()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.cur = nil
	l.cond.Broadcast()
	l.mu.Unlock()

	log.Info("serial listener: closing", "path", l.devPath)
	err := l.rwc.Close()
	if err != nil {
		log.Error("serial listener: device close failed",
			"path", l.devPath,
			"err", err)
		return fmt.Errorf("serial listener: close %s: %w", l.devPath, err)
	}
	log.Info("serial listener: closed", "path", l.devPath)
	return nil
}

// Addr returns a stable address representation referencing the device
// file path.
func (l *SerialListener) Addr() net.Addr { return l.addr }

func (l *SerialListener) sessionEnded(c *serialConn) {
	l.mu.Lock()
	released := false
	if l.cur == c {
		l.cur = nil
		released = true
		l.cond.Broadcast()
	}
	closed := l.closed
	l.mu.Unlock()
	l.logger().Info("serial listener: session ended",
		"path", l.devPath,
		"session_id", c.sessionID,
		"released", released,
		"listener_closed", closed)
}

func (l *SerialListener) logger() *slog.Logger {
	if l.log != nil {
		return l.log
	}
	return slog.Default()
}

// serialAddr is a minimal [net.Addr] backed by the device path.
type serialAddr string

func (serialAddr) Network() string  { return "virtio-serial" }
func (s serialAddr) String() string { return string(s) }

// serialConn wraps the persistent ReadWriteCloser to satisfy
// [net.Conn]. Multiple serialConns over the same listener share the
// underlying rwc but only one is "active" at a time per the listener's
// session gating. Closing a serialConn does NOT close the underlying
// device.
type serialConn struct {
	rwc         io.ReadWriteCloser
	laddr       serialAddr
	raddr       serialAddr
	parent      *SerialListener
	sessionID   uint64
	idleTimeout time.Duration
	nowFn       func() time.Time
	log         *slog.Logger

	closeOnce sync.Once
}

func (c *serialConn) Read(b []byte) (int, error) {
	if c.idleTimeout > 0 && c.nowFn != nil {
		_ = c.setReadDeadline(c.nowFn().Add(c.idleTimeout))
	}
	n, err := c.rwc.Read(b)
	switch {
	case n == 0 && isTimeoutError(err):
		c.logger().Info("serial conn: idle timeout returns EOF",
			"session_id", c.sessionID,
			"idle_s", c.idleTimeout.Seconds(),
			"err", err)
		err = io.EOF
	case errors.Is(err, io.EOF):
		c.logger().Info("serial conn: read EOF",
			"session_id", c.sessionID,
			"n", n)
	case err != nil:
		c.logger().Warn("serial conn: read error",
			"session_id", c.sessionID,
			"n", n,
			"err", err)
	}
	if debugSerialIO {
		var sample []byte
		if n > 0 {
			sample = b[:min(n, 32)]
		}
		c.logger().DebugContext(context.Background(), "serial read",
			"session_id", c.sessionID,
			"n", n,
			"err", err,
			"first32hex", hex.EncodeToString(sample))
	}
	if err == nil {
		return n, nil
	}
	return n, err
}

func (c *serialConn) Write(b []byte) (int, error) {
	n, err := c.rwc.Write(b)
	if debugSerialIO {
		var sample []byte
		if len(b) > 0 {
			sample = b[:min(len(b), 32)]
		}
		c.logger().DebugContext(context.Background(), "serial write",
			"session_id", c.sessionID,
			"n", n,
			"err", err,
			"first32hex", hex.EncodeToString(sample))
	}
	if err != nil {
		return n, fmt.Errorf("serial conn: write: %w", err)
	}
	return n, nil
}

func (c *serialConn) LocalAddr() net.Addr  { return c.laddr }
func (c *serialConn) RemoteAddr() net.Addr { return c.raddr }

// Close signals the listener that this session is done. It does NOT
// close the underlying device; the device lives until Listener.Close.
func (c *serialConn) Close() error {
	c.closeOnce.Do(func() {
		log := c.logger()
		log.Info("serial conn: close (session end, device stays open)",
			"session_id", c.sessionID)
		c.parent.sessionEnded(c)
	})
	return nil
}

// SetDeadline / SetReadDeadline / SetWriteDeadline are best-effort.
// Honored when the underlying ReadWriteCloser supports them ([os.File]
// does on Unix; tests that need them implement the deadline
// interfaces).
func (c *serialConn) SetDeadline(t time.Time) error {
	rwc, ok := c.rwc.(deadlineReadWriteCloser)
	if !ok {
		return nil
	}
	err := rwc.SetDeadline(t)
	if err != nil {
		return fmt.Errorf("serial conn: SetDeadline: %w", err)
	}
	return nil
}

func (c *serialConn) SetReadDeadline(t time.Time) error {
	return c.setReadDeadline(t)
}

func (c *serialConn) SetWriteDeadline(t time.Time) error {
	rwc, ok := c.rwc.(writeDeadliner)
	if !ok {
		return nil
	}
	err := rwc.SetWriteDeadline(t)
	if err != nil {
		return fmt.Errorf("serial conn: SetWriteDeadline: %w", err)
	}
	return nil
}

func (c *serialConn) setReadDeadline(t time.Time) error {
	rwc, ok := c.rwc.(readDeadliner)
	if !ok {
		return nil
	}
	err := rwc.SetReadDeadline(t)
	if err != nil {
		return fmt.Errorf("serial conn: SetReadDeadline: %w", err)
	}
	return nil
}

func (c *serialConn) logger() *slog.Logger {
	if c.log != nil {
		return c.log
	}
	return slog.Default()
}

type readDeadliner interface {
	SetReadDeadline(t time.Time) error
}

type writeDeadliner interface {
	SetWriteDeadline(t time.Time) error
}

type deadlineReadWriteCloser interface {
	readDeadliner
	writeDeadliner
	SetDeadline(t time.Time) error
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
