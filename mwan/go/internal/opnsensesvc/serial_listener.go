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
// /dev/ttyV0.1) as a net.Listener. The serial channel is
// inherently single-stream: at most one connection is "open" at
// any time. Accept blocks until either the previous connection
// closes (so we can re-open) or the listener itself is closed.
//
// gRPC's Serve loop expects Accept to block when no connection is
// available rather than return an error, so this matters for
// correctness (errored Accepts cause gRPC to give up entirely).
//
// This is enough for our use case (vault is the only client).
// Multiplexing would require a small framing protocol on top.
type SerialListener struct {
	devPath string
	openFn  func(path string) (io.ReadWriteCloser, error)
	addr    serialAddr

	mu     sync.Mutex
	cond   *sync.Cond
	closed bool
	cur    io.ReadWriteCloser
}

// NewSerialListener returns a Listener that opens devPath on each
// Accept call. openFn is the device-open function; on FreeBSD it
// opens the character device, in tests it can return a fake.
func NewSerialListener(devPath string, openFn func(path string) (io.ReadWriteCloser, error)) *SerialListener {
	l := &SerialListener{
		devPath: devPath,
		openFn:  openFn,
		addr:    serialAddr(devPath),
	}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// Accept opens the device and returns it as a net.Conn. If a
// previous connection is still open it blocks until that
// connection closes before opening the device again. Returns an
// error only when the listener itself is closed.
func (l *SerialListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	for !l.closed && l.cur != nil {
		l.cond.Wait()
	}
	if l.closed {
		l.mu.Unlock()
		return nil, errors.New("serial listener: closed")
	}
	l.mu.Unlock()

	rwc, err := l.openFn(l.devPath)
	if err != nil {
		slog.Error("serial listener: open failed", "path", l.devPath, "err", err)
		return nil, fmt.Errorf("serial listener: open %s: %w", l.devPath, err)
	}

	l.mu.Lock()
	l.cur = rwc
	l.mu.Unlock()

	return &serialConn{
		rwc:    rwc,
		laddr:  l.addr,
		raddr:  l.addr,
		parent: l,
	}, nil
}

// Close releases the listener. If a connection is open, that
// connection is also closed. Wakes any pending Accept callers so
// they observe the closed state.
func (l *SerialListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	l.cond.Broadcast()
	if l.cur != nil {
		err := l.cur.Close()
		l.cur = nil
		return err
	}
	return nil
}

// Addr returns a stable address representation referencing the
// device file path.
func (l *SerialListener) Addr() net.Addr {
	return l.addr
}

func (l *SerialListener) connClosed(rwc io.ReadWriteCloser) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cur == rwc {
		l.cur = nil
		l.cond.Broadcast()
	}
}

// serialAddr is a minimal net.Addr backed by the device path.
type serialAddr string

func (s serialAddr) Network() string { return "virtio-serial" }
func (s serialAddr) String() string  { return string(s) }

// serialConn wraps an io.ReadWriteCloser to satisfy net.Conn.
// Deadline support is best-effort and only takes effect when the
// underlying file supports it (os.File does on Unix-like systems).
type serialConn struct {
	rwc    io.ReadWriteCloser
	laddr  serialAddr
	raddr  serialAddr
	parent *SerialListener

	closeOnce sync.Once
	closeErr  error
}

func (c *serialConn) Read(b []byte) (int, error) {
	n, err := c.rwc.Read(b)
	if debugSerialIO {
		var sample []byte
		if n > 0 {
			sample = b[:min(n, 32)]
		}
		slog.DebugContext(context.Background(), "serial read", "n", n, "err", err, "first32hex", hex.EncodeToString(sample))
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
		slog.DebugContext(context.Background(), "serial write", "n", n, "err", err, "first32hex", hex.EncodeToString(sample))
	}
	return n, err
}
func (c *serialConn) LocalAddr() net.Addr  { return c.laddr }
func (c *serialConn) RemoteAddr() net.Addr { return c.raddr }

func (c *serialConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.rwc.Close()
		c.parent.connClosed(c.rwc)
	})
	return c.closeErr
}

// SetDeadline / SetReadDeadline / SetWriteDeadline are best-effort.
// If the underlying ReadWriteCloser is an *os.File the kernel will
// honor them (on Linux/FreeBSD via SO_RCVTIMEO/SO_SNDTIMEO style
// poll-based deadlines for char devices). For fake implementations
// in tests, they are no-ops.
func (c *serialConn) SetDeadline(t time.Time) error {
	if f, ok := c.rwc.(*os.File); ok {
		return f.SetDeadline(t)
	}
	return nil
}

func (c *serialConn) SetReadDeadline(t time.Time) error {
	if f, ok := c.rwc.(*os.File); ok {
		return f.SetReadDeadline(t)
	}
	return nil
}

func (c *serialConn) SetWriteDeadline(t time.Time) error {
	if f, ok := c.rwc.(*os.File); ok {
		return f.SetWriteDeadline(t)
	}
	return nil
}
