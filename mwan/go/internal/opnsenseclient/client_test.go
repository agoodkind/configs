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

func TestDialRetriesUnixHandshake(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Dial(ctx, Config{
		Target:      "unix://" + socketPath,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	if counting.accepted.Load() < 2 {
		t.Fatalf("accepted connections = %d, want at least 2", counting.accepted.Load())
	}
	if server.versionCalls.Load() != 1 {
		t.Fatalf("version calls = %d, want 1", server.versionCalls.Load())
	}
}

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := range 2 {
		client, err := Dial(ctx, Config{
			Target:      "unix://" + socketPath,
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("dial %d: %v", i+1, err)
		}
		if _, err := client.RPC().Version(ctx, &mwanv1.VersionRequest{}); err != nil {
			t.Fatalf("version %d: %v", i+1, err)
		}
		if err := client.Close(); err != nil {
			t.Fatalf("close %d: %v", i+1, err)
		}
	}

	if counting.accepted.Load() != 2 {
		t.Fatalf("accepted connections = %d, want 2", counting.accepted.Load())
	}
	if server.versionCalls.Load() != 4 {
		t.Fatalf("version calls = %d, want 4", server.versionCalls.Load())
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
