package opnsenseclient

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

type versionOnlyServer struct {
	mwanv1.UnimplementedMWANOPNsenseServiceServer
	versionCalls atomic.Int32
}

func (s *versionOnlyServer) Version(
	context.Context,
	*mwanv1.VersionRequest,
) (*mwanv1.VersionResponse, error) {
	s.versionCalls.Add(1)
	return &mwanv1.VersionResponse{Version: "test"}, nil
}

type countingListener struct {
	net.Listener
	accepted atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.accepted.Add(1)
	return conn, nil
}

type closeFirstListener struct {
	*countingListener
	closedFirst atomic.Bool
}

func (l *closeFirstListener) Accept() (net.Conn, error) {
	conn, err := l.countingListener.Accept()
	if err != nil {
		return nil, err
	}
	if l.closedFirst.CompareAndSwap(false, true) {
		_ = conn.Close()
	}
	return conn, nil
}

// TestDialReturnsImmediately exercises the new contract: Dial does
// NOT block on connect. Even with no listener at the target, Dial
// returns nil error and produces a usable Client whose channel is in
// IDLE/CONNECTING state. The bridge relies on this so it can start
// serving local probes before the upstream daemon is reachable.
func TestDialReturnsImmediately(t *testing.T) {
	socketPath := testUnixSocketPath(t)
	// Intentionally do not create a listener.

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	startedAt := time.Now()
	client, err := Dial(ctx, Config{
		Target: "unix://" + socketPath,
	})
	elapsed := time.Since(startedAt)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close: %v", closeErr)
		}
	}()
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Dial took %s, expected < 500ms (NewClient must be non-blocking)", elapsed)
	}
}

// TestWaitForReadyAcrossUpstreamFlap exercises the gRPC reconnect
// loop. The listener closes its first accepted connection mid-handshake
// to simulate a transient virtio-serial chardev failure. WaitForReady
// must persist across that failure and return nil once the second
// accept succeeds.
func TestWaitForReadyAcrossUpstreamFlap(t *testing.T) {
	socketPath := testUnixSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}

	server := &versionOnlyServer{}
	grpcServer := grpc.NewServer()
	mwanv1.RegisterMWANOPNsenseServiceServer(grpcServer, server)

	counting := &countingListener{Listener: listener}
	done := serveTestGRPC(t, grpcServer, &closeFirstListener{countingListener: counting})
	defer stopTestGRPC(t, grpcServer, done)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := Dial(ctx, Config{
		Target: "unix://" + socketPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	if err := client.WaitForReady(ctx); err != nil {
		t.Fatalf("WaitForReady: %v", err)
	}

	if counting.accepted.Load() < 2 {
		t.Fatalf("accepted connections = %d, want at least 2", counting.accepted.Load())
	}
	if server.versionCalls.Load() < 1 {
		t.Fatalf("version calls = %d, want at least 1", server.versionCalls.Load())
	}
}

// TestDialCreatesIndependentUnixConnections asserts that two
// successive Dial+RPC cycles each open a fresh unix connection (i.e.
// closing one Client does not pin the listener side, and the second
// Dial truly reconnects).
func TestDialCreatesIndependentUnixConnections(t *testing.T) {
	socketPath := testUnixSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}

	server := &versionOnlyServer{}
	grpcServer := grpc.NewServer()
	mwanv1.RegisterMWANOPNsenseServiceServer(grpcServer, server)

	counting := &countingListener{Listener: listener}
	done := serveTestGRPC(t, grpcServer, counting)
	defer stopTestGRPC(t, grpcServer, done)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := range 2 {
		client, err := Dial(ctx, Config{
			Target: "unix://" + socketPath,
		})
		if err != nil {
			t.Fatalf("dial %d: %v", i+1, err)
		}
		if _, err := client.RPC().Version(ctx, &mwanv1.VersionRequest{}, grpc.WaitForReady(true)); err != nil {
			t.Fatalf("version %d: %v", i+1, err)
		}
		if err := client.Close(); err != nil {
			t.Fatalf("close %d: %v", i+1, err)
		}
	}

	if counting.accepted.Load() < 2 {
		t.Fatalf("accepted connections = %d, want at least 2", counting.accepted.Load())
	}
	if server.versionCalls.Load() != 2 {
		t.Fatalf("version calls = %d, want 2", server.versionCalls.Load())
	}
}

// TestDialEmptyTargetRejected covers the only structural error Dial
// can produce now that handshake is delegated to gRPC.
func TestDialEmptyTargetRejected(t *testing.T) {
	_, err := Dial(context.Background(), Config{Target: ""})
	if err == nil {
		t.Fatal("Dial with empty target: want error, got nil")
	}
}

func TestUnixTargetPath(t *testing.T) {
	path, ok := unixTargetPath("unix:///tmp/mwanrpc.sock")
	if !ok {
		t.Fatal("expected unix target")
	}
	if path != "/tmp/mwanrpc.sock" {
		t.Fatalf("path = %q", path)
	}

	path, ok = unixTargetPath("tcp://localhost:1234")
	if ok {
		t.Fatalf("unexpected unix target %q", path)
	}
}

func testUnixSocketPath(t *testing.T) string {
	t.Helper()
	t.Setenv("TMPDIR", "/tmp")
	dir := t.TempDir()
	return dir + "/sock"
}

func serveTestGRPC(
	t *testing.T,
	grpcServer *grpc.Server,
	listener net.Listener,
) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- grpcServer.Serve(listener)
	}()
	return done
}

func stopTestGRPC(t *testing.T, grpcServer *grpc.Server, done <-chan error) {
	t.Helper()
	grpcServer.Stop()
	if err := <-done; err != nil {
		t.Fatalf("grpc serve: %v", err)
	}
}
