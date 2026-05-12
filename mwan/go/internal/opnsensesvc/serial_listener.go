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

// SerialConn wraps the shared virtio-serial fd as a [net.Conn] so
// gRPC's HTTP/2 transport can run over it. The same underlying fd is
// reused across sequential client connections because the host-side
// chardev muxes connections into one byte stream on the guest side.
// Closing the SerialConn signals the listener that the next Accept()
// can return a fresh wrapper, but does not close the underlying fd.
// Deadlines are no-ops; the underlying tty does not support them.
type SerialConn struct {
	rwc      io.ReadWriteCloser
	listener *OneShotListener
	once     sync.Once
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

// Close finalizes this wrapper. The fd stays open because it is owned
// by the listener and reused for the next client connection. The
// listener is notified so the next Accept() can return a fresh
// wrapper.
func (s *SerialConn) Close() error {
	s.once.Do(func() {
		s.listener.notifyConnClosed(s)
	})
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

// OneShotListener is the [net.Listener] that the daemon hands to
// grpc.Server.Serve. The name predates the multi-accept rewrite and is
// kept for source compatibility. At any moment the listener serves at
// most one active client connection because the host-side chardev
// muxes connections sequentially into one byte stream on the guest
// side. Accept() blocks until the previously issued conn is closed by
// gRPC (which happens when the client disconnects and the daemon
// observes EOF on Read). It then returns a fresh SerialConn wrapper
// reading and writing the same fd, so the next probe invocation gets
// its own HTTP/2 connection over the same persistent guest-side fd.
// Close() shuts the listener down and closes the underlying fd.
type OneShotListener struct {
	mu         sync.Mutex
	cond       *sync.Cond
	rwc        io.ReadWriteCloser
	active     *SerialConn
	closed     bool
	yieldFirst bool
}

// NewOneShotListener wraps an [io.ReadWriteCloser] so its lifecycle is
// owned by the listener. The underlying fd is reused for every
// SerialConn yielded by Accept().
func NewOneShotListener(rwc io.ReadWriteCloser) *OneShotListener {
	l := &OneShotListener{
		mu:         sync.Mutex{},
		cond:       nil,
		rwc:        rwc,
		active:     nil,
		closed:     false,
		yieldFirst: false,
	}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// Accept blocks until the listener is in a state where it can issue a
// fresh SerialConn. The first call returns immediately. Subsequent
// calls block until the previously issued conn is Closed, then return
// a new wrapper around the same fd.
func (l *OneShotListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		if l.closed {
			return nil, net.ErrClosed
		}
		if !l.yieldFirst {
			l.yieldFirst = true
			if l.rwc == nil {
				return nil, errors.New("oneshot: nil rwc")
			}
			conn := &SerialConn{rwc: l.rwc, listener: l, once: sync.Once{}}
			l.active = conn
			return conn, nil
		}
		if l.active == nil {
			conn := &SerialConn{rwc: l.rwc, listener: l, once: sync.Once{}}
			l.active = conn
			return conn, nil
		}
		l.cond.Wait()
	}
}

// notifyConnClosed is called from SerialConn.Close() once the gRPC
// transport tears down the connection. It clears the active slot and
// signals any blocked Accept() so the next probe can be served.
func (l *OneShotListener) notifyConnClosed(conn *SerialConn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == conn {
		l.active = nil
	}
	l.cond.Broadcast()
}

// Close shuts the listener down. Blocked Accept() calls return
// [net.ErrClosed]. The underlying fd is closed so the daemon releases
// the virtio-serial device. Close uses [context.Background] for its
// internal slog event because the [net.Listener] interface forbids
// taking a context here.
func (l *OneShotListener) Close() error {
	return l.shutdown(context.Background())
}

// shutdown is the context-aware close path used by the daemon's
// stop-watcher goroutine so the slog event chains to the serve span.
func (l *OneShotListener) shutdown(ctx context.Context) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	rwc := l.rwc
	l.rwc = nil
	l.cond.Broadcast()
	l.mu.Unlock()
	if rwc == nil {
		return nil
	}
	if err := rwc.Close(); err != nil {
		slog.ErrorContext(ctx, "oneshot: close rwc failed", "err", err)
		return fmt.Errorf("oneshot: close rwc: %w", err)
	}
	return nil
}

// Shutdown is the context-aware variant of Close used by serve.go.
func (l *OneShotListener) Shutdown(ctx context.Context) error {
	return l.shutdown(ctx)
}

// Addr returns the listener address.
func (l *OneShotListener) Addr() net.Addr { return serialAddr("listener") }
