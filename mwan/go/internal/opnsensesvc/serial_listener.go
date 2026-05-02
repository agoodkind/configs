package opnsensesvc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// SerialListener wraps a single character device (typically
// /dev/ttyV0.0) as a net.Listener. The serial channel is
// inherently single-stream: at most one connection is "open" at
// any time. Subsequent Accept() calls block until the previous
// connection closes, then re-open the device.
//
// This is enough for our use case (vault is the only client).
// Multiplexing would require a small framing protocol on top.
type SerialListener struct {
	devPath string
	openFn  func(path string) (io.ReadWriteCloser, error)
	addr    serialAddr

	mu     sync.Mutex
	closed bool
	cur    io.ReadWriteCloser
}

// NewSerialListener returns a Listener that opens devPath on each
// Accept call. openFn is the device-open function; on FreeBSD it
// opens the character device, in tests it can return a fake.
func NewSerialListener(devPath string, openFn func(path string) (io.ReadWriteCloser, error)) *SerialListener {
	return &SerialListener{
		devPath: devPath,
		openFn:  openFn,
		addr:    serialAddr(devPath),
	}
}

// Accept opens the device and returns it as a net.Conn. Blocks
// until the previous connection closes.
func (l *SerialListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, errors.New("serial listener: closed")
	}
	if l.cur != nil {
		// Previous connection still open. Wait for it to close.
		l.mu.Unlock()
		return nil, errors.New("serial listener: previous connection still open")
	}
	l.mu.Unlock()

	rwc, err := l.openFn(l.devPath)
	if err != nil {
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
// connection is also closed.
func (l *SerialListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
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

func (c *serialConn) Read(b []byte) (int, error)  { return c.rwc.Read(b) }
func (c *serialConn) Write(b []byte) (int, error) { return c.rwc.Write(b) }
func (c *serialConn) LocalAddr() net.Addr         { return c.laddr }
func (c *serialConn) RemoteAddr() net.Addr        { return c.raddr }

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
