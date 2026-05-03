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

type serializedPipeOpen struct {
	t              *testing.T
	mu             sync.Mutex
	pendingClients []net.Conn
	clientReady    chan struct{}
}

func newSerializedPipeOpen(t *testing.T) *serializedPipeOpen {
	t.Helper()
	return &serializedPipeOpen{
		t:           t,
		clientReady: make(chan struct{}, 1),
	}
}

func (o *serializedPipeOpen) open(_ string) (io.ReadWriteCloser, error) {
	serverConn, clientConn := net.Pipe()
	o.mu.Lock()
	o.pendingClients = append(o.pendingClients, clientConn)
	o.mu.Unlock()
	o.signalClientReady()
	return serverConn, nil
}

func (o *serializedPipeOpen) dial(ctx context.Context, _ string) (net.Conn, error) {
	for {
		o.mu.Lock()
		if len(o.pendingClients) > 0 {
			clientConn := o.pendingClients[0]
			o.pendingClients = o.pendingClients[1:]
			o.mu.Unlock()
			return clientConn, nil
		}
		o.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-o.clientReady:
		}
	}
}

func (o *serializedPipeOpen) signalClientReady() {
	select {
	case o.clientReady <- struct{}{}:
	default:
	}
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

func TestGRPCOverSerializedListenerAllowsIndependentClients(t *testing.T) {
	opener := newSerializedPipeOpen(t)
	listener := NewSerialListener("/tmp/test-serial", opener.open)
	listener.postCloseDelay = 0

	grpcServer := grpc.NewServer()
	mwanv1.RegisterMWANOPNsenseServiceServer(
		grpcServer,
		NewServer(slog.Default(), "/tmp/nonexistent-config.xml", t.TempDir()),
	)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- grpcServer.Serve(listener)
	}()

	for i := range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := grpc.NewClient(
			"passthrough:///serialized-pipe",
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(opener.dial),
		)
		if err != nil {
			cancel()
			t.Fatalf("NewClient: %v", err)
		}
		client := mwanv1.NewMWANOPNsenseServiceClient(conn)
		resp, err := client.Version(ctx, &mwanv1.VersionRequest{})
		if err != nil {
			cancel()
			_ = conn.Close()
			t.Fatalf("Version client %d: %v", i+1, err)
		}
		if strings.TrimSpace(resp.GetVersion()) == "" {
			cancel()
			_ = conn.Close()
			t.Fatalf("Version client %d returned empty version", i+1)
		}
		if err := conn.Close(); err != nil {
			cancel()
			t.Fatalf("client close %d: %v", i+1, err)
		}
		cancel()
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
