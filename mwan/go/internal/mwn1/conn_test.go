package mwn1

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pipeConns returns a pair of *Conn linked via net.Pipe. onA/onB are
// the per-side frame callbacks. The caller must Close both when done.
func pipeConns(t *testing.T, onA, onB func(Frame)) (*Conn, *Conn) {
	t.Helper()
	a, b := net.Pipe()
	connA := NewConn(a, slog.Default(), onA)
	connB := NewConn(b, slog.Default(), onB)
	return connA, connB
}

func TestConn_ConcurrentSend(t *testing.T) {
	const senders = 4
	const perSender = 25
	var (
		mu      sync.Mutex
		got     []Frame
		recv    = make(chan struct{})
		wantTot = senders * perSender
	)
	onB := func(f Frame) {
		mu.Lock()
		got = append(got, f)
		if len(got) == wantTot {
			close(recv)
		}
		mu.Unlock()
	}
	a, b := pipeConns(t, nil, onB)
	defer a.Close()
	defer b.Close()
	var wg sync.WaitGroup
	for s := 0; s < senders; s++ {
		wg.Add(1)
		go func(sender int) {
			defer wg.Done()
			for i := 0; i < perSender; i++ {
				f := Frame{
					Flags:    FlagRequest | FlagFinal,
					MethodID: 1,
					CorrID:   uint64(sender*1000 + i),
					Payload:  []byte("hi"),
				}
				if err := a.Send(f); err != nil {
					t.Errorf("send: %v", err)
					return
				}
			}
		}(s)
	}
	wg.Wait()
	select {
	case <-recv:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %d frames; got %d", wantTot, len(got))
	}
	if len(got) != wantTot {
		t.Fatalf("got %d frames, want %d", len(got), wantTot)
	}
}

func TestConn_CloseRejectsSend(t *testing.T) {
	a, b := pipeConns(t, nil, nil)
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	defer b.Close()
	err := a.Send(Frame{Flags: FlagFinal, MethodID: 1, CorrID: 1})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("want ErrClosed, got %v", err)
	}
}

func TestConn_ReaderEOF(t *testing.T) {
	a, b := net.Pipe()
	conn := NewConn(a, slog.Default(), nil)
	// Closing b causes a's Read to return EOF / closed-pipe error.
	_ = b.Close()
	select {
	case <-conn.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("Done did not close after peer EOF")
	}
	if err := conn.Err(); err == nil {
		t.Fatalf("want non-nil Err after peer EOF")
	}
	_ = conn.Close()
}

func TestConn_OrderedThroughput(t *testing.T) {
	const n = 1000
	var (
		mu   sync.Mutex
		got  []uint64
		recv = make(chan struct{})
	)
	onB := func(f Frame) {
		mu.Lock()
		got = append(got, f.CorrID)
		if len(got) == n {
			close(recv)
		}
		mu.Unlock()
	}
	a, b := pipeConns(t, nil, onB)
	defer a.Close()
	defer b.Close()
	for i := 0; i < n; i++ {
		f := Frame{Flags: FlagFinal, MethodID: 1, CorrID: uint64(i), Payload: []byte{byte(i & 0xff)}}
		if err := a.Send(f); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	select {
	case <-recv:
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout; got %d/%d", len(got), n)
	}
	for i, id := range got {
		if id != uint64(i) {
			t.Fatalf("out-of-order at %d: got corr=%d", i, id)
		}
	}
}

func TestConn_CloseIdempotent(t *testing.T) {
	a, b := pipeConns(t, nil, nil)
	defer b.Close()
	if err := a.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	// Second close must not panic and may return any error from the
	// underlying Closer's idempotency contract; we accept nil too.
	_ = a.Close()
}

// failingWriter is an io.ReadWriteCloser whose Write always errors.
// Used to force the writer goroutine to bail and verify Send afterwards
// returns ErrClosed.
type failingWriter struct {
	closed atomic.Bool
	pipeR  net.Conn
	pipeW  net.Conn
}

func newFailingWriter() *failingWriter {
	r, w := net.Pipe()
	return &failingWriter{pipeR: r, pipeW: w}
}

func (f *failingWriter) Read(p []byte) (int, error) {
	// Block forever until Close is called, then return EOF.
	return f.pipeR.Read(p)
}

func (f *failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("forced write failure")
}

func (f *failingWriter) Close() error {
	if f.closed.Swap(true) {
		return nil
	}
	_ = f.pipeR.Close()
	return f.pipeW.Close()
}

func TestConn_WriteErrorClosesSend(t *testing.T) {
	rw := newFailingWriter()
	defer rw.Close()
	conn := NewConn(rw, slog.Default(), nil)
	defer conn.Close()
	// First send may or may not race the writer; loop until Send sees
	// ErrClosed (writer drained the channel and exited).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := conn.Send(Frame{Flags: FlagFinal, MethodID: 1, CorrID: 1})
		if errors.Is(err, ErrClosed) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Send never returned ErrClosed; conn.Err=%v", conn.Err())
}

// nullCloser exists only to satisfy io.ReadWriteCloser in tests where
// the underlying connection is already shut down.
type nullCloser struct{ io.ReadWriter }

func (nullCloser) Close() error { return nil }
