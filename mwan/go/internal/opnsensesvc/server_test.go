package opnsensesvc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

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
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}

func TestServeContextCancelClosesBlockedListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	openStarted := make(chan struct{})
	server := NewServer(slog.Default(), "/tmp/nonexistent-config.xml", t.TempDir())
	done := make(chan error, 1)

	go func() {
		done <- Serve(ctx, ServeOpts{
			SerialPath: "/tmp/test-serial",
			OpenSerial: func(_ string) (io.ReadWriteCloser, error) {
				close(openStarted)
				return &closeAwareRWC{closed: make(chan struct{})}, nil
			},
			Server: server,
			Log:    slog.Default(),
		})
	}()

	select {
	case <-openStarted:
	case <-time.After(time.Second):
		t.Fatal("Serve did not enter OpenSerial")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error after context cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve goroutine did not exit after listener close")
	}
}

// TestGRPCOverPersistentListenerMultipleRPCs validates that the
// persistent-device listener supports many sequential RPCs over a
// single gRPC ClientConn. This is the bridge architecture: one
// long-lived ClientConn with many RPCs multiplexed via HTTP/2 streams.
func TestGRPCOverPersistentListenerMultipleRPCs(t *testing.T) {
	// Single-pipe opener: device is opened ONCE, returns one fd-equivalent
	// (the server side of a net.Pipe). The client side is what gRPC dials.
	serverConn, clientConn := net.Pipe()
	listener, err := NewSerialListener("/tmp/test-serial", func(_ string) (io.ReadWriteCloser, error) {
		return serverConn, nil
	})
	if err != nil {
		t.Fatalf("NewSerialListener: %v", err)
	}

	grpcServer := grpc.NewServer()
	mwanv1.RegisterMWANOPNsenseServiceServer(
		grpcServer,
		NewServer(slog.Default(), "/tmp/nonexistent-config.xml", t.TempDir()),
	)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- grpcServer.Serve(listener)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///persistent",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return clientConn, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client := mwanv1.NewMWANOPNsenseServiceClient(conn)

	// Issue several sequential RPCs over the SAME ClientConn (HTTP/2
	// stream multiplex). All must succeed.
	for i := range 5 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		resp, err := client.Version(ctx, &mwanv1.VersionRequest{})
		cancel()
		if err != nil {
			_ = conn.Close()
			t.Fatalf("Version RPC %d: %v", i+1, err)
		}
		if strings.TrimSpace(resp.GetVersion()) == "" {
			_ = conn.Close()
			t.Fatalf("Version RPC %d returned empty version", i+1)
		}
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("client close: %v", err)
	}

	grpcServer.Stop()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("grpc serve: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("gRPC serve did not exit")
	}
}
