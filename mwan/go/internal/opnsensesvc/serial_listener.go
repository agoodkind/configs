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
// /dev/ttyV0.1) as a net.Listener. The serial channel is inherently
// single-stream: at most one connection is "open" at any time. Accept
// blocks while a wrapper is active and resumes after the wrapper is
// closed.
//
// Each Accept opens the device fresh; each wrapper Close releases it.
// A small grace period on close gives qemu chardev time to flush its
// disconnect notification and accept a new host peer before the next
// Accept opens a fresh fd. Without this grace the next Accept races
// the disconnect/reconnect handshake on virtio-serial, producing an
// every-other-RPC failure pattern.
//
// gRPC's Serve loop expects Accept to block when no connection is
// available rather than return an error, so this matters for
// correctness (errored Accepts cause gRPC to give up entirely).
//
// Single-peer use case (the host process that owns the unix socket
// on the Proxmox side). Multiplexing would require a small framing
// protocol on top.
type SerialListener struct {
	devPath          string
	openFn           func(path string) (io.ReadWriteCloser, error)
	addr             serialAddr
	postCloseDelay   time.Duration
	staleReadTimeout time.Duration
	nowFn            func() time.Time
	log              *slog.Logger

	mu            sync.Mutex
	cond          *sync.Cond
	closed        bool
	cur           io.ReadWriteCloser
	nextSessionID uint64
}

// NewSerialListener returns a Listener that opens devPath on each
// Accept call. openFn is the device-open function; on FreeBSD it
// opens the character device, in tests it can return a fake.
func NewSerialListener(devPath string, openFn func(path string) (io.ReadWriteCloser, error)) *SerialListener {
	l := &SerialListener{
		devPath:          devPath,
		openFn:           openFn,
		addr:             serialAddr(devPath),
		postCloseDelay:   1200 * time.Millisecond,
		staleReadTimeout: time.Second,
		nowFn:            time.Now,
		log:              slog.Default(),
	}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// Accept opens the device and returns it as a net.Conn. If a previous
// connection is still open (or in its post-close grace) it blocks
// until the device is free. Returns an error only when the listener
// itself is closed.
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
	log.Info("serial listener: accept opening device",
		"path", l.devPath,
		"session_id", sessionID)
	l.mu.Unlock()

	rwc, err := l.openFn(l.devPath)
	if err != nil {
		log.Error("serial listener: open failed",
			"path", l.devPath,
			"session_id", sessionID,
			"err", err)
		return nil, fmt.Errorf("serial listener: open %s: %w", l.devPath, err)
	}
	log.Info("serial listener: device opened",
		"path", l.devPath,
		"session_id", sessionID)

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		log.Info("serial listener: closing device opened after listener close",
			"path", l.devPath,
			"session_id", sessionID)
		if err := rwc.Close(); err != nil {
			log.Error("serial listener: close after listener close failed",
				"path", l.devPath,
				"session_id", sessionID,
				"err", err)
		}
		return nil, errors.New("serial listener: closed")
	}
	l.cur = rwc
	log.Info("serial listener: connection accepted",
		"path", l.devPath,
		"session_id", sessionID)
	l.mu.Unlock()

	return &serialConn{
		rwc:              rwc,
		laddr:            l.addr,
		raddr:            l.addr,
		parent:           l,
		sessionID:        sessionID,
		staleReadTimeout: l.staleReadTimeout,
		nowFn:            l.nowFn,
		log:              log,
	}, nil
}

// Close releases the listener. If a connection is open, that
// connection is also closed. Wakes any pending Accept callers so they
// observe the closed state.
func (l *SerialListener) Close() error {
	log := l.logger()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	cur := l.cur
	l.cur = nil
	l.cond.Broadcast()
	l.mu.Unlock()

	log.Info("serial listener: closing", "path", l.devPath)
	if cur == nil {
		log.Info("serial listener: closed", "path", l.devPath)
		return nil
	}

	log.Info("serial listener: closing active device", "path", l.devPath)
	err := cur.Close()
	if err != nil {
		log.Error("serial listener: active device close failed",
			"path", l.devPath,
			"err", err)
		return err
	}
	log.Info("serial listener: closed active device", "path", l.devPath)
	return nil
}

// Addr returns a stable address representation referencing the device
// file path.
func (l *SerialListener) Addr() net.Addr {
	return l.addr
}

func (l *SerialListener) connClosed(rwc io.ReadWriteCloser, sessionID uint64) {
	log := l.logger()
	// Hold the device-busy state for a short grace period after the
	// gRPC server closed its end. This lets qemu's chardev see the
	// host disconnect and accept the next host peer cleanly before
	// the next Accept reopens /dev/ttyV0.x. Without this grace the
	// reopen races the disconnect/reconnect on virtio-serial and the
	// next session sees stale bytes from the previous one.
	if l.shouldRunPostCloseDelay(rwc) {
		log.Info("serial listener: post-close delay started",
			"path", l.devPath,
			"session_id", sessionID,
			"duration", l.postCloseDelay.String())
		timer := time.NewTimer(l.postCloseDelay)
		<-timer.C
		log.Info("serial listener: post-close delay finished",
			"path", l.devPath,
			"session_id", sessionID,
			"duration", l.postCloseDelay.String())
	}
	l.mu.Lock()
	released := false
	if l.cur == rwc {
		l.cur = nil
		released = true
		l.cond.Broadcast()
	}
	closed := l.closed
	l.mu.Unlock()
	log.Info("serial listener: connection closed",
		"path", l.devPath,
		"session_id", sessionID,
		"released", released,
		"listener_closed", closed)
}

func (l *SerialListener) shouldRunPostCloseDelay(rwc io.ReadWriteCloser) bool {
	if l.postCloseDelay <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.closed && l.cur == rwc
}

func (l *SerialListener) logger() *slog.Logger {
	if l.log != nil {
		return l.log
	}
	return slog.Default()
}

// serialAddr is a minimal net.Addr backed by the device path.
type serialAddr string

func (s serialAddr) Network() string { return "virtio-serial" }
func (s serialAddr) String() string  { return string(s) }

// serialConn wraps an io.ReadWriteCloser to satisfy net.Conn.
// Deadline support is best-effort and only takes effect when the
// underlying file supports it (os.File does on Unix-like systems).
type serialConn struct {
	rwc              io.ReadWriteCloser
	laddr            serialAddr
	raddr            serialAddr
	parent           *SerialListener
	sessionID        uint64
	staleReadTimeout time.Duration
	nowFn            func() time.Time
	log              *slog.Logger

	closeOnce sync.Once
	closeErr  error
}

func (c *serialConn) Read(b []byte) (int, error) {
	if c.staleReadTimeout > 0 && c.nowFn != nil {
		_ = c.setReadDeadline(c.nowFn().Add(c.staleReadTimeout))
	}
	n, err := c.rwc.Read(b)
	switch {
	case n == 0 && isTimeoutError(err):
		c.logger().Info("serial connection: read timeout returned EOF",
			"session_id", c.sessionID,
			"err", err)
		err = io.EOF
	case errors.Is(err, io.EOF):
		c.logger().Info("serial connection: read EOF",
			"session_id", c.sessionID,
			"n", n)
	case err != nil:
		c.logger().Warn("serial connection: read error",
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
	return n, err
}
func (c *serialConn) LocalAddr() net.Addr  { return c.laddr }
func (c *serialConn) RemoteAddr() net.Addr { return c.raddr }

func (c *serialConn) Close() error {
	c.closeOnce.Do(func() {
		log := c.logger()
		log.Info("serial connection: closing", "session_id", c.sessionID)
		c.closeErr = c.rwc.Close()
		if c.closeErr != nil {
			log.Error("serial connection: close failed",
				"session_id", c.sessionID,
				"err", c.closeErr)
		} else {
			log.Info("serial connection: closed", "session_id", c.sessionID)
		}
		c.parent.connClosed(c.rwc, c.sessionID)
	})
	return c.closeErr
}

func (c *serialConn) logger() *slog.Logger {
	if c.log != nil {
		return c.log
	}
	return slog.Default()
}

// SetDeadline / SetReadDeadline / SetWriteDeadline are best-effort.
// If the underlying ReadWriteCloser supports deadlines, the kernel or
// fake test object honors them. Otherwise these methods are no-ops.
func (c *serialConn) SetDeadline(t time.Time) error {
	if rwc, ok := c.rwc.(deadlineReadWriteCloser); ok {
		return rwc.SetDeadline(t)
	}
	return nil
}

func (c *serialConn) SetReadDeadline(t time.Time) error {
	return c.setReadDeadline(t)
}

func (c *serialConn) SetWriteDeadline(t time.Time) error {
	if rwc, ok := c.rwc.(writeDeadliner); ok {
		return rwc.SetWriteDeadline(t)
	}
	return nil
}

func (c *serialConn) setReadDeadline(t time.Time) error {
	if rwc, ok := c.rwc.(readDeadliner); ok {
		return rwc.SetReadDeadline(t)
	}
	return nil
}

type readDeadliner interface {
	SetReadDeadline(time.Time) error
}

type writeDeadliner interface {
	SetWriteDeadline(time.Time) error
}

type deadlineReadWriteCloser interface {
	readDeadliner
	writeDeadliner
	SetDeadline(time.Time) error
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
