package mwn1

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// sendQueueDepth is the buffered capacity of the writer goroutine's
// inbound channel. 64 is enough to absorb a small burst of streamed
// chunk frames without head-of-line blocking the caller, while
// remaining small enough that backpressure surfaces quickly when the
// underlying Write stalls.
const sendQueueDepth = 64

// ErrClosed is returned by Send after Close has been called or after
// the writer goroutine has exited because of a previous write error.
var ErrClosed = errors.New("mwn1: connection closed")

// Conn wraps an [io.ReadWriteCloser] as a frame-oriented bidirectional
// channel. It owns one reader goroutine and one writer goroutine. The
// reader decodes frames from the underlying stream and hands each one
// to the user-supplied onFrame callback. The writer drains an internal
// buffered channel and serializes WriteFrame calls.
//
// Multiple goroutines may invoke [Conn.Send] concurrently; the writer
// goroutine guarantees that frames are written one-at-a-time to the
// underlying ReadWriteCloser.
type Conn struct {
	rw        io.ReadWriteCloser
	log       *slog.Logger
	onFrame   func(Frame)
	sendCh    chan Frame
	done      chan struct{}
	closeOnce sync.Once

	mu     sync.Mutex
	closed bool
	err    error
}

// NewConn starts the reader and writer goroutines for rw and returns
// the wrapper. onFrame is invoked from the reader goroutine for every
// successfully decoded frame; it must not block long-term, since the
// reader cannot make progress while it runs.
//
// The returned Conn owns rw: callers must not Read or Write rw
// directly. Close the Conn (not rw) to shut down cleanly.
func NewConn(rw io.ReadWriteCloser, log *slog.Logger, onFrame func(Frame)) *Conn {
	if log == nil {
		log = slog.Default()
	}
	c := &Conn{
		rw:        rw,
		log:       log,
		onFrame:   onFrame,
		sendCh:    make(chan Frame, sendQueueDepth),
		done:      make(chan struct{}),
		closeOnce: sync.Once{},
		mu:        sync.Mutex{},
		closed:    false,
		err:       nil,
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.log.Error("mwn1: writeLoop panic recovered",
					slog.String("err", fmt.Sprintf("%v", r)))
			}
		}()
		c.writeLoop()
	}()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.log.Error("mwn1: readLoop panic recovered",
					slog.String("err", fmt.Sprintf("%v", r)))
			}
		}()
		c.readLoop()
	}()
	return c
}

// Send queues f for transmission by the writer goroutine. It returns
// ErrClosed if Close has been called or if a previous frame failed to
// write (in which case the underlying error is also retrievable via
// [Conn.Err]).
func (c *Conn) Send(f Frame) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.mu.Unlock()
	select {
	case c.sendCh <- f:
		return nil
	case <-c.done:
		// Reader exited; writer will drain and close shortly.
		return ErrClosed
	}
}

// Close signals both goroutines to exit and closes the underlying
// ReadWriteCloser. It is idempotent and safe to call from any
// goroutine.
func (c *Conn) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		if err := c.rw.Close(); err != nil {
			c.log.Warn("mwn1: close transport", slog.String("err", err.Error()))
			closeErr = fmt.Errorf("mwn1: close transport: %w", err)
		}
		// Wake the writer if it is parked on the channel.
		close(c.sendCh)
	})
	return closeErr
}

// Done returns a channel that is closed when the reader goroutine has
// exited. Callers can use it to wait for end-of-stream.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns the first non-nil error recorded by either goroutine,
// or nil if neither has errored yet. It is goroutine-safe.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// recordErr stores err if no earlier error was recorded.
func (c *Conn) recordErr(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
}

// readLoop runs in its own goroutine. It reads frames until the
// underlying Read returns an error, at which point it records the
// error and signals shutdown by closing c.done.
func (c *Conn) readLoop() {
	defer close(c.done)
	for {
		f, err := ReadFrame(c.rw, c.log)
		if err != nil {
			c.recordErr(err)
			return
		}
		if c.onFrame != nil {
			c.onFrame(f)
		}
	}
}

// drainSendQueue reads and discards every remaining frame on ch until
// the channel is closed, returning the number of frames it drained.
// Used by writeLoop to unblock parked senders after a write error has
// shut the connection down. The count is returned so the caller can
// log how many frames were dropped instead of written.
func drainSendQueue(ch <-chan Frame) int {
	dropped := 0
	for range ch {
		dropped++
	}
	return dropped
}

// writeLoop runs in its own goroutine. It drains sendCh and writes
// frames one at a time. On Write error it records the error, marks
// the conn closed (so subsequent Send calls return ErrClosed), drains
// any buffered frames without writing them, and exits.
func (c *Conn) writeLoop() {
	for f := range c.sendCh {
		if err := WriteFrame(c.rw, f, c.log); err != nil {
			c.recordErr(err)
			c.mu.Lock()
			c.closed = true
			c.mu.Unlock()
			// Drain remaining frames so senders do not block on the
			// buffered channel. Log how many were dropped to aid
			// post-mortem of the write failure.
			dropped := drainSendQueue(c.sendCh)
			if dropped > 0 {
				c.log.Warn("mwn1: dropped queued frames after write error",
					slog.Int("dropped", dropped))
			}
			return
		}
	}
}
