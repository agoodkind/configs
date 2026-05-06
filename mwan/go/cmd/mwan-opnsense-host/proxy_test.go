package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
	"goodkind.io/mwan/internal/opnsenseclient"
)

// fakeMWN1Daemon is a goroutine that accepts a single unix socket
// connection, reads MWN1 frames, and replies according to handleFn.
// It is the test fixture's stand-in for the real mwan-opnsense daemon
// running inside the OPNsense VM.
type fakeMWN1Daemon struct {
	socketPath string
	listener   net.Listener
	reg        *mwn1.Registry

	mu       sync.Mutex
	handleFn func(req mwn1.Frame) (mwn1.Frame, bool)
	conns    []net.Conn

	versionCalls atomic.Int32
	closed       atomic.Bool
}

func newFakeMWN1Daemon(t *testing.T) *fakeMWN1Daemon {
	t.Helper()
	t.Setenv("TMPDIR", "/tmp")
	dir := t.TempDir()
	socketPath := dir + "/upstream.sock"
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}
	reg, err := mwn1.NewMWANOPNsenseRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	d := &fakeMWN1Daemon{
		socketPath: socketPath,
		listener:   listener,
		reg:        reg,
	}
	d.handleFn = d.defaultEcho
	return d
}

func (d *fakeMWN1Daemon) defaultEcho(req mwn1.Frame) (mwn1.Frame, bool) {
	if req.MethodID == mwn1.MethodVersion {
		d.versionCalls.Add(1)
		resp := &mwanv1.VersionResponse{Version: "fake"}
		payload, _, _ := mwn1.MarshalResponse(d.reg, mwn1.MethodVersion, resp)
		return mwn1.Frame{
			Flags:    mwn1.FlagFinal,
			MethodID: req.MethodID,
			CorrID:   req.CorrID,
			Payload:  payload,
		}, true
	}
	resp, ok := d.reg.NewResponse(req.MethodID)
	if !ok {
		return mwn1.Frame{}, false
	}
	payload, _, _ := mwn1.MarshalResponse(d.reg, req.MethodID, resp)
	return mwn1.Frame{
		Flags:    mwn1.FlagFinal,
		MethodID: req.MethodID,
		CorrID:   req.CorrID,
		Payload:  payload,
	}, true
}

func (d *fakeMWN1Daemon) setHandler(fn func(req mwn1.Frame) (mwn1.Frame, bool)) {
	d.mu.Lock()
	d.handleFn = fn
	d.mu.Unlock()
}

func (d *fakeMWN1Daemon) currentHandler() func(req mwn1.Frame) (mwn1.Frame, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.handleFn
}

func (d *fakeMWN1Daemon) Serve(t *testing.T) {
	t.Helper()
	go func() {
		for {
			conn, err := d.listener.Accept()
			if err != nil {
				return
			}
			d.mu.Lock()
			d.conns = append(d.conns, conn)
			d.mu.Unlock()
			go d.serveConn(t, conn)
		}
	}()
}

func (d *fakeMWN1Daemon) serveConn(t *testing.T, conn net.Conn) {
	t.Helper()
	defer func() { _ = conn.Close() }()
	for {
		req, readErr := mwn1.ReadFrame(conn, nil)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && !d.closed.Load() {
				t.Logf("daemon read: %v", readErr)
			}
			return
		}
		if req.Flags&mwn1.FlagStreaming != 0 && req.Flags&mwn1.FlagFinal == 0 {
			continue
		}
		handler := d.currentHandler()
		resp, ok := handler(req)
		if !ok {
			continue
		}
		if writeErr := mwn1.WriteFrame(conn, resp, nil); writeErr != nil {
			return
		}
	}
}

func (d *fakeMWN1Daemon) Stop() {
	d.closed.Store(true)
	_ = d.listener.Close()
	d.mu.Lock()
	conns := d.conns
	d.conns = nil
	d.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// bridgeFixture wires together a fake MWN1 daemon, an opnsenseclient
// dialed at it, and a gRPC bridge serving a unix socket. Probes
// connect to the bridge socket as real probes would.
type bridgeFixture struct {
	daemon       *fakeMWN1Daemon
	upstreamCli  *opnsenseclient.Client
	bridgeServer *grpc.Server
	bridgeSocket string
	bridgeDone   chan error
	probeConn    *grpc.ClientConn
	probe        mwanv1.MWANOPNsenseServiceClient
}

func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	daemon := newFakeMWN1Daemon(t)
	daemon.Serve(t)

	t.Setenv("TMPDIR", "/tmp")
	bridgeSocket := t.TempDir() + "/bridge.sock"

	cli, err := opnsenseclient.Dial(context.Background(),
		opnsenseclient.Config{Target: "unix://" + daemon.socketPath, Log: slog.Default()})
	if err != nil {
		t.Fatalf("opnsenseclient.Dial: %v", err)
	}

	listener, err := net.Listen("unix", bridgeSocket)
	if err != nil {
		t.Fatalf("listen bridge: %v", err)
	}
	gs := grpc.NewServer()
	mwanv1.RegisterMWANOPNsenseServiceServer(gs,
		newProxyServer(cli.RPC(), slog.Default()))
	bridgeDone := make(chan error, 1)
	go func() { bridgeDone <- gs.Serve(listener) }()

	probeConn, err := grpc.NewClient("unix://"+bridgeSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.Dial("unix", bridgeSocket)
		}))
	if err != nil {
		t.Fatalf("probe NewClient: %v", err)
	}

	fix := &bridgeFixture{
		daemon:       daemon,
		upstreamCli:  cli,
		bridgeServer: gs,
		bridgeSocket: bridgeSocket,
		bridgeDone:   bridgeDone,
		probeConn:    probeConn,
		probe:        mwanv1.NewMWANOPNsenseServiceClient(probeConn),
	}
	t.Cleanup(fix.shutdown)
	return fix
}

func (f *bridgeFixture) shutdown() {
	_ = f.probeConn.Close()
	f.bridgeServer.Stop()
	<-f.bridgeDone
	_ = f.upstreamCli.Close()
	f.daemon.Stop()
}

// TestProxy_SingleVersion covers the happy-path translation: probe
// gRPC Version -> bridge proxy -> MWN1 frame -> daemon -> response
// frame -> bridge -> probe gRPC response.
func TestProxy_SingleVersion(t *testing.T) {
	fix := newBridgeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := fix.probe.Version(ctx, &mwanv1.VersionRequest{})
	if err != nil {
		t.Fatalf("probe Version: %v", err)
	}
	if resp.GetVersion() != "fake" {
		t.Fatalf("Version=%q want fake", resp.GetVersion())
	}
	if fix.daemon.versionCalls.Load() != 1 {
		t.Fatalf("daemon versionCalls=%d want 1", fix.daemon.versionCalls.Load())
	}
}

// TestProxy_50Sequential covers serial round-trip stability across
// many calls on one MWN1 connection.
func TestProxy_50Sequential(t *testing.T) {
	fix := newBridgeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := range 50 {
		_, err := fix.probe.Version(ctx, &mwanv1.VersionRequest{})
		if err != nil {
			t.Fatalf("probe Version[%d]: %v", i, err)
		}
	}
	if fix.daemon.versionCalls.Load() != 50 {
		t.Fatalf("daemon versionCalls=%d want 50", fix.daemon.versionCalls.Load())
	}
}

// TestProxy_50Concurrent covers concurrent CorrID routing through the
// bridge: 50 goroutines each issue a probe gRPC Version, every result
// must come back to its caller without cross-routing.
func TestProxy_50Concurrent(t *testing.T) {
	fix := newBridgeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			resp, err := fix.probe.Version(ctx, &mwanv1.VersionRequest{})
			if err != nil {
				errs <- err
				return
			}
			if resp.GetVersion() != "fake" {
				errs <- errors.New("unexpected response")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent Version: %v", e)
	}
	if fix.daemon.versionCalls.Load() != n {
		t.Fatalf("daemon versionCalls=%d want %d", fix.daemon.versionCalls.Load(), n)
	}
}

// TestProxy_DaemonReturnsError covers the FlagError path: the daemon
// answers with FlagError, the bridge surfaces the resulting error to
// the probe, and the probe sees a wrapped gRPC error.
func TestProxy_DaemonReturnsError(t *testing.T) {
	fix := newBridgeFixture(t)
	fix.daemon.setHandler(func(req mwn1.Frame) (mwn1.Frame, bool) {
		return mwn1.Frame{
			Flags:    mwn1.FlagFinal | mwn1.FlagError,
			MethodID: req.MethodID,
			CorrID:   req.CorrID,
			Payload:  []byte("daemon refused"),
		}, true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := fix.probe.Version(ctx, &mwanv1.VersionRequest{})
	if err == nil {
		t.Fatal("probe Version: want error from FlagError frame, got nil")
	}
}

// TestProxy_ContextCancellation covers ctx cancellation propagating
// through the bridge: probe cancels, Call returns ctx.Err(), and the
// pending map in the upstream client does not leak.
func TestProxy_ContextCancellation(t *testing.T) {
	fix := newBridgeFixture(t)
	fix.daemon.setHandler(func(req mwn1.Frame) (mwn1.Frame, bool) {
		_ = req
		return mwn1.Frame{}, false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := fix.probe.Version(ctx, &mwanv1.VersionRequest{})
	if err == nil {
		t.Fatal("probe Version: want ctx error, got nil")
	}

	// Allow the bridge handler goroutine to unwind. The internal
	// pending map cleanup is exercised by client_test.go directly.
	time.Sleep(100 * time.Millisecond)
}

// TestProxy_DaemonClosesMidCall covers the case where the upstream
// MWN1 socket dies mid-call. The probe sees an error, not a hang.
func TestProxy_DaemonClosesMidCall(t *testing.T) {
	fix := newBridgeFixture(t)
	gotReq := make(chan struct{})
	fix.daemon.setHandler(func(req mwn1.Frame) (mwn1.Frame, bool) {
		_ = req
		select {
		case gotReq <- struct{}{}:
		default:
		}
		return mwn1.Frame{}, false
	})

	probeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, callErr := fix.probe.Version(ctx, &mwanv1.VersionRequest{})
		probeDone <- callErr
	}()
	select {
	case <-gotReq:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not receive request")
	}
	fix.daemon.Stop()
	select {
	case err := <-probeDone:
		if err == nil {
			t.Fatal("probe Version: want error after daemon close, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("probe Version hung past daemon close")
	}
}
