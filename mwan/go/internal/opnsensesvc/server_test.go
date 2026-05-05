package opnsensesvc

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// closeAwareRWC is an io.ReadWriteCloser that blocks Read until Close
// is called, then returns io.EOF. Used to verify that Serve's context
// cancellation tears down the dispatcher and underlying device.
type closeAwareRWC struct {
	opened chan struct{}
	closed chan struct{}
	once   sync.Once
}

func newCloseAwareRWC() *closeAwareRWC {
	return &closeAwareRWC{
		opened: make(chan struct{}),
		closed: make(chan struct{}),
	}
}

func (r *closeAwareRWC) Read(_ []byte) (int, error) {
	<-r.closed
	return 0, io.EOF
}

func (r *closeAwareRWC) Write(bytes []byte) (int, error) {
	return len(bytes), nil
}

func (r *closeAwareRWC) Close() error {
	r.once.Do(func() {
		close(r.closed)
	})
	return nil
}

func TestServeReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	serialDevice := newCloseAwareRWC()
	server := NewServer(slog.Default(), "/tmp/nonexistent-config.xml", t.TempDir())
	done := make(chan error, 1)

	go func() {
		done <- Serve(ctx, ServeOpts{
			SerialPath: "/tmp/test-serial",
			OpenSerial: func(_ string) (io.ReadWriteCloser, error) {
				close(serialDevice.opened)
				return serialDevice, nil
			},
			Server: server,
			Log:    slog.Default(),
		})
	}()

	select {
	case <-serialDevice.opened:
	case <-time.After(time.Second):
		t.Fatal("Serve did not open serial device")
	}

	cancel()

	select {
	case <-serialDevice.closed:
	case <-time.After(time.Second):
		t.Fatal("Serve context cancel did not close active serial device")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error after context cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}
