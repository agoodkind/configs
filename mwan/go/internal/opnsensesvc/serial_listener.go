package opnsensesvc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// SerialConn wraps an [io.ReadWriteCloser] as a [net.Conn] so gRPC's
// HTTP/2 transport can run over the raw-mode virtio-serial tty.
// Deadlines are no-ops; the underlying tty does not support them.
type SerialConn struct {
	rwc io.ReadWriteCloser
}

// NewSerialConn returns a [net.Conn] that reads and writes through rwc.
func NewSerialConn(rwc io.ReadWriteCloser) net.Conn {
	return &SerialConn{rwc: rwc}
}

// Read forwards to the underlying ReadWriteCloser.
func (s *SerialConn) Read(b []byte) (int, error) {
	n, err := s.rwc.Read(b)
	if err != nil {
		return n, fmt.Errorf("serial: read: %w", err)
	}
	return n, nil
}

// Write forwards to the underlying ReadWriteCloser.
func (s *SerialConn) Write(b []byte) (int, error) {
	n, err := s.rwc.Write(b)
	if err != nil {
		return n, fmt.Errorf("serial: write: %w", err)
	}
	return n, nil
}

// Close closes the underlying ReadWriteCloser.
func (s *SerialConn) Close() error {
	if err := s.rwc.Close(); err != nil {
		slog.ErrorContext(context.Background(), "serial: close failed", "err", err)
		return fmt.Errorf("serial: close: %w", err)
	}
	return nil
}

// LocalAddr returns a placeholder unix address.
func (s *SerialConn) LocalAddr() net.Addr { return serialAddr("local") }

// RemoteAddr returns a placeholder unix address.
func (s *SerialConn) RemoteAddr() net.Addr { return serialAddr("remote") }

// SetDeadline is a no-op for the virtio-serial transport.
func (s *SerialConn) SetDeadline(_ time.Time) error { return nil }

// SetReadDeadline is a no-op for the virtio-serial transport.
func (s *SerialConn) SetReadDeadline(_ time.Time) error { return nil }

// SetWriteDeadline is a no-op for the virtio-serial transport.
func (s *SerialConn) SetWriteDeadline(_ time.Time) error { return nil }

type serialAddr string

func (a serialAddr) Network() string { return "virtio-serial" }
func (a serialAddr) String() string  { return string(a) }

// OneShotListener is a [net.Listener] that hands a single pre-built
// [net.Conn] out on the first Accept() and then blocks on subsequent
// Accept() calls until Close() is invoked. gRPC's Serve loop calls
// Accept() in a loop; this listener returns one connection, then
// blocks the second call to keep gRPC quiescent on the one peer.
type OneShotListener struct {
	mu      sync.Mutex
	conn    net.Conn
	yielded bool
	closeCh chan struct{}
	closed  bool
}

// NewOneShotListener wraps conn in a single-Accept listener.
func NewOneShotListener(conn net.Conn) *OneShotListener {
	return &OneShotListener{
		mu:      sync.Mutex{},
		conn:    conn,
		yielded: false,
		closeCh: make(chan struct{}),
		closed:  false,
	}
}

// Accept returns the wrapped connection on first call. Subsequent
// calls block until Close().
func (l *OneShotListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.yielded {
		l.yielded = true
		c := l.conn
		l.mu.Unlock()
		if c == nil {
			return nil, errors.New("oneshot: nil conn")
		}
		return c, nil
	}
	l.mu.Unlock()
	<-l.closeCh
	return nil, net.ErrClosed
}

// Close releases any blocked Accept() call. It does not close the
// returned connection; gRPC owns its lifecycle.
func (l *OneShotListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	close(l.closeCh)
	return nil
}

// Addr returns the listener address.
func (l *OneShotListener) Addr() net.Addr { return serialAddr("listener") }
