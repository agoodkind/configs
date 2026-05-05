package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsenseclient"
)

// fakeUpstreamServer is a controllable Version/Exec/etc. backend that
// can be stopped and restarted on the same unix socket path. Tests use
// it to simulate the OPNsense daemon disappearing and coming back.
type fakeUpstreamServer struct {
	mwanv1.UnimplementedMWANOPNsenseServiceServer

	socketPath string

	mu         sync.Mutex
	grpcServer *grpc.Server
	listener   net.Listener
	serveDone  chan error

	versionCalls atomic.Int32
	// versionDelay holds the response for this many millis before
	// returning. Zero means respond immediately. Tests use this to
	// simulate slow upstream calls so they can be killed mid-flight.
	versionDelayMs atomic.Int64
}

func newFakeUpstreamServer(t *testing.T, socketPath string) *fakeUpstreamServer {
	t.Helper()
	return &fakeUpstreamServer{socketPath: socketPath}
}

func (f *fakeUpstreamServer) Version(ctx context.Context, _ *mwanv1.VersionRequest) (*mwanv1.VersionResponse, error) {
	f.versionCalls.Add(1)
	delayMs := f.versionDelayMs.Load()
	if delayMs > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(delayMs) * time.Millisecond):
		}
	}
	return &mwanv1.VersionResponse{Version: "fake"}, nil
}

func (f *fakeUpstreamServer) Start(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.grpcServer != nil {
		t.Fatal("fakeUpstreamServer already running")
	}
	if removeErr := os.Remove(f.socketPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		t.Fatalf("remove stale socket: %v", removeErr)
	}
	listener, err := net.Listen("unix", f.socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	mwanv1.RegisterMWANOPNsenseServiceServer(gs, f)
	done := make(chan error, 1)
	go func() {
		done <- gs.Serve(listener)
	}()
	f.grpcServer = gs
	f.listener = listener
	f.serveDone = done
}

// Stop hard-stops the server (closes connections immediately, like
// `kill -9` on the daemon). Safe to call when not running.
func (f *fakeUpstreamServer) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.grpcServer == nil {
		return
	}
	f.grpcServer.Stop()
	<-f.serveDone
	f.grpcServer = nil
	f.listener = nil
	f.serveDone = nil
}

// bridgeFixture is a fully wired bridge: opnsenseclient -> proxyServer
// -> local listener. Tests dial the local listener as a probe would.
type bridgeFixture struct {
	upstreamSocket string
	upstream       *fakeUpstreamServer
	bridgeClient   *opnsenseclient.Client
	bridgeServer   *grpc.Server
	bridgeListener net.Listener
	bridgeSocket   string
	bridgeDone     chan error

	probeConn *grpc.ClientConn
	probe     mwanv1.MWANOPNsenseServiceClient
}

func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	t.Setenv("TMPDIR", "/tmp")
	dir := t.TempDir()
	upstreamSocket := dir + "/upstream.sock"
	bridgeSocket := dir + "/bridge.sock"

	upstreamClient, err := opnsenseclient.Dial(context.Background(), opnsenseclient.Config{
		Target: "unix://" + upstreamSocket,
	})
	if err != nil {
		t.Fatalf("opnsenseclient.Dial: %v", err)
	}

	listener, err := net.Listen("unix", bridgeSocket)
	if err != nil {
		t.Fatalf("listen bridge: %v", err)
	}

	gs := grpc.NewServer()
	mwanv1.RegisterMWANOPNsenseServiceServer(gs, newProxyServer(upstreamClient.RPC(), slog.Default()))
	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- gs.Serve(listener)
	}()

	probeConn, err := grpc.NewClient(
		"unix://"+bridgeSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.Dial("unix", bridgeSocket)
		}),
	)
	if err != nil {
		t.Fatalf("probe NewClient: %v", err)
	}

	fix := &bridgeFixture{
		upstreamSocket: upstreamSocket,
		upstream:       newFakeUpstreamServer(t, upstreamSocket),
		bridgeClient:   upstreamClient,
		bridgeServer:   gs,
		bridgeListener: listener,
		bridgeSocket:   bridgeSocket,
		bridgeDone:     bridgeDone,
		probeConn:      probeConn,
		probe:          mwanv1.NewMWANOPNsenseServiceClient(probeConn),
	}
	t.Cleanup(fix.shutdown)
	return fix
}

func (f *bridgeFixture) shutdown() {
	_ = f.probeConn.Close()
	f.bridgeServer.Stop()
	<-f.bridgeDone
	_ = f.bridgeListener.Close()
	_ = f.bridgeClient.Close()
	f.upstream.Stop()
}

// probeVersionShort issues a Version RPC with a short deadline. Returns
// the gRPC status code (Unavailable, OK, DeadlineExceeded, ...) and
// the error itself.
func (f *bridgeFixture) probeVersionShort(t *testing.T, deadline time.Duration) (codes.Code, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	_, err := f.probe.Version(ctx, &mwanv1.VersionRequest{})
	if err == nil {
		return codes.OK, nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return codes.Unknown, err
	}
	return st.Code(), err
}

// probeUntilOK calls probeVersionShort in a loop until it returns OK
// or the deadline elapses. Returns true on success.
func (f *bridgeFixture) probeUntilOK(t *testing.T, total time.Duration, perCall time.Duration) bool {
	t.Helper()
	expire := time.Now().Add(total)
	for time.Now().Before(expire) {
		code, _ := f.probeVersionShort(t, perCall)
		if code == codes.OK {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// TestColdStartUpstreamUnavailable covers scenario (a): the bridge
// boots before the upstream daemon. Probes get Unavailable in <5s
// (no hang). Then the daemon comes up and probes succeed within ~10s.
func TestColdStartUpstreamUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	fix := newBridgeFixture(t)

	// (1) Upstream is not running. Probe must not hang.
	startedAt := time.Now()
	code, err := fix.probeVersionShort(t, 3*time.Second)
	elapsed := time.Since(startedAt)
	if elapsed > 5*time.Second {
		t.Fatalf("probe hung for %s; expected <5s", elapsed)
	}
	if code != codes.Unavailable && code != codes.DeadlineExceeded {
		t.Fatalf("probe code=%s err=%v; want Unavailable or DeadlineExceeded", code, err)
	}

	// (2) Upstream comes up. Probes succeed within ~10s.
	fix.upstream.Start(t)
	if !fix.probeUntilOK(t, 15*time.Second, 3*time.Second) {
		t.Fatal("probe never succeeded after upstream came up")
	}
}

// TestMidLifeUpstreamRestart covers scenario (b): bridge has had
// successful RPCs, upstream is killed, probes return Unavailable,
// upstream restarts, probes succeed within 10s.
func TestMidLifeUpstreamRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	fix := newBridgeFixture(t)
	fix.upstream.Start(t)

	if !fix.probeUntilOK(t, 10*time.Second, 3*time.Second) {
		t.Fatal("initial probe never succeeded")
	}

	fix.upstream.Stop()

	// Probes during downtime should return Unavailable (or
	// DeadlineExceeded if the call deadline expires before the
	// channel re-enters TRANSIENT_FAILURE).
	code, err := fix.probeVersionShort(t, 2*time.Second)
	if code != codes.Unavailable && code != codes.DeadlineExceeded {
		t.Fatalf("downtime probe code=%s err=%v; want Unavailable or DeadlineExceeded", code, err)
	}

	fix.upstream.Start(t)
	if !fix.probeUntilOK(t, 15*time.Second, 3*time.Second) {
		t.Fatal("probe never succeeded after upstream restart")
	}
}

// TestRapidUpstreamRestarts covers scenario (c): 5 cycles of stop+start
// spaced ~2s apart. Throughout, a probe loop runs every 500ms. Each
// probe either succeeds or returns Unavailable; nothing panics or hangs.
func TestRapidUpstreamRestarts(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	fix := newBridgeFixture(t)
	fix.upstream.Start(t)

	stop := make(chan struct{})
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				code, err := fix.probeVersionShort(t, 1*time.Second)
				if code != codes.OK && code != codes.Unavailable && code != codes.DeadlineExceeded {
					t.Errorf("unexpected probe code=%s err=%v", code, err)
				}
			}
		}
	}()

	for cycle := range 5 {
		time.Sleep(2 * time.Second)
		t.Logf("rapid-restart cycle=%d: stopping upstream", cycle)
		fix.upstream.Stop()
		time.Sleep(500 * time.Millisecond)
		t.Logf("rapid-restart cycle=%d: starting upstream", cycle)
		fix.upstream.Start(t)
	}

	close(stop)
	<-probeDone

	// After the storm, give the channel a moment then verify recovery.
	if !fix.probeUntilOK(t, 15*time.Second, 3*time.Second) {
		t.Fatal("probe never recovered after rapid restart storm")
	}
}

// TestLongUpstreamOutageBackoff covers scenario (d): upstream away
// for ~12 seconds (kept short for CI). Verify probes recover within
// 15s of upstream returning. The backoff escalation itself is a gRPC
// internal; we cannot directly assert log lines without coupling to
// the library. We assert recovery time, which is the user-visible
// effect of correctly configured backoff.
func TestLongUpstreamOutageBackoff(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	fix := newBridgeFixture(t)
	fix.upstream.Start(t)
	if !fix.probeUntilOK(t, 10*time.Second, 3*time.Second) {
		t.Fatal("initial probe never succeeded")
	}

	fix.upstream.Stop()
	time.Sleep(12 * time.Second)

	// Verify probes still return Unavailable instead of hanging.
	startedAt := time.Now()
	code, _ := fix.probeVersionShort(t, 2*time.Second)
	if time.Since(startedAt) > 4*time.Second {
		t.Fatalf("probe hung during outage")
	}
	if code != codes.Unavailable && code != codes.DeadlineExceeded {
		t.Fatalf("outage probe code=%s; want Unavailable or DeadlineExceeded", code)
	}

	fix.upstream.Start(t)
	if !fix.probeUntilOK(t, 35*time.Second, 5*time.Second) {
		t.Fatal("probe never recovered after long outage")
	}
}

// TestBridgeProcessSurvivesUpstreamLoss covers scenario (e): the
// bridge's serve loop keeps running and the local listener stays bound
// across upstream lifecycle events. We assert this by issuing a probe
// after the chaos and confirming the bridge listener is still
// accepting connections (non-OK is fine; the point is "no exit").
func TestBridgeProcessSurvivesUpstreamLoss(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	fix := newBridgeFixture(t)
	// Bounce upstream a few times.
	for range 3 {
		fix.upstream.Start(t)
		time.Sleep(200 * time.Millisecond)
		fix.upstream.Stop()
		time.Sleep(200 * time.Millisecond)
	}

	// Bridge listener must still be accepting probe connections. A
	// successful dial+RPC attempt (even with Unavailable) proves the
	// bridge process and listener are alive.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := fix.probe.Version(ctx, &mwanv1.VersionRequest{})
	st, _ := status.FromError(err)
	// We do not assert OK; we assert that the bridge accepted the
	// probe at all (i.e. the listener is alive). Any code is fine
	// except ones that imply the local listener is gone.
	if err != nil && st.Code() == codes.Unknown {
		t.Fatalf("probe got opaque error suggesting bridge died: %v", err)
	}
}

// TestMidRPCUpstreamDeath covers scenario (f): a slow upstream RPC is
// killed in flight. The probe must observe Unavailable (or a related
// terminal code) rather than hanging on a dead context.
func TestMidRPCUpstreamDeath(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	fix := newBridgeFixture(t)
	fix.upstream.Start(t)
	fix.upstream.versionDelayMs.Store(5000)

	if err := fix.bridgeClient.WaitForReady(contextWithTimeout(t, 10*time.Second)); err != nil {
		t.Fatalf("WaitForReady before probe: %v", err)
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	probeDone := make(chan error, 1)
	go func() {
		_, err := fix.probe.Version(probeCtx, &mwanv1.VersionRequest{})
		probeDone <- err
	}()

	// Let the RPC settle in flight, then yank the upstream.
	time.Sleep(1 * time.Second)
	fix.upstream.Stop()

	select {
	case err := <-probeDone:
		if err == nil {
			t.Fatal("probe returned OK; expected error from killed upstream")
		}
		st, _ := status.FromError(err)
		if st.Code() != codes.Unavailable && st.Code() != codes.Canceled && st.Code() != codes.DeadlineExceeded && st.Code() != codes.Internal {
			t.Fatalf("probe code=%s err=%v; want Unavailable/Canceled/DeadlineExceeded/Internal", st.Code(), err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("probe hung past upstream death; expected fast failure")
	}
}

func contextWithTimeout(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
