package opnsensesvc

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// blockingRWC models the virtio-serial device on an idle line: Read
// blocks until Close is called, exactly like the FreeBSD virtio_console
// read that never returns on its own (no host-disconnect signal). This
// is the state that wedged the daemon on stop and orphaned its child.
type blockingRWC struct {
	mu      sync.Mutex
	closed  bool
	release chan struct{}
}

func newBlockingRWC() *blockingRWC {
	return &blockingRWC{release: make(chan struct{})}
}

func (b *blockingRWC) Read(p []byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

func (b *blockingRWC) Write(p []byte) (int, error) {
	return len(p), nil
}

func (b *blockingRWC) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.release)
	}
	return nil
}

func (b *blockingRWC) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// TestServe_ReturnsOnCtxCancelIdleLine is the headline regression for the
// stop-orphan defect. With the serial line idle (the read parked), Serve
// must return within the StopTimeout bound after the serve ctx is
// cancelled. Before the fix it hangs forever, because the shutdown path
// blocks on a read that never returns.
func TestServe_ReturnsOnCtxCancelIdleLine(t *testing.T) {
	t.Parallel()

	const stopTimeout = 2 * time.Second
	fake := newBlockingRWC()
	srv := NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)), "", "")

	opts := ServeOpts{
		SerialPath:   "/dev/fake-serial",
		OpenSerial:   func(string) (io.ReadWriteCloser, error) { return fake, nil },
		Server:       srv,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnSerialOpen: nil,
		OnGRPCAccept: nil,
		Transfer:     nil,
		StopTimeout:  stopTimeout,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, opts) }()

	// Let the daemon establish its session and park recvLoop in the read.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		if !fake.isClosed() {
			t.Fatal("Serve returned but never closed the serial fd")
		}
	case <-time.After(stopTimeout + 3*time.Second):
		t.Fatal("Serve did not return within the bound after ctx cancel: the stop-orphan wedge")
	}
}
