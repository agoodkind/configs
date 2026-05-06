package opnsenseclient

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
)

// fakeDaemon answers MWN1 frames on a single unix socket. It accepts
// one connection, reads request frames, and replies with a response
// crafted by handleFn (defaults to echoing the same Version response).
//
// Tests flip its atomics to inject errors, delays, or unknown method
// ids to exercise the full client surface.
type fakeDaemon struct {
	socketPath string
	listener   net.Listener
	reg        *mwn1.Registry

	mu       sync.Mutex
	handleFn func(req mwn1.Frame) (mwn1.Frame, bool)
	conns    []net.Conn

	versionCalls atomic.Int32
	connsServed  atomic.Int32
	closed       atomic.Bool
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	t.Setenv("TMPDIR", "/tmp")
	dir := t.TempDir()
	socketPath := dir + "/sock"
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	reg, err := mwn1.NewMWANOPNsenseRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	d := &fakeDaemon{
		socketPath: socketPath,
		listener:   listener,
		reg:        reg,
	}
	d.handleFn = d.defaultEcho
	return d
}

// defaultEcho responds to a Version request with a stable VersionResponse.
// All other methods echo back an empty response of the matching type.
func (d *fakeDaemon) defaultEcho(req mwn1.Frame) (mwn1.Frame, bool) {
	if req.MethodID == mwn1.MethodVersion {
		d.versionCalls.Add(1)
		resp := &mwanv1.VersionResponse{Version: "fake", BuildCommit: "x"}
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

func (d *fakeDaemon) setHandler(fn func(req mwn1.Frame) (mwn1.Frame, bool)) {
	d.mu.Lock()
	d.handleFn = fn
	d.mu.Unlock()
}

func (d *fakeDaemon) currentHandler() func(req mwn1.Frame) (mwn1.Frame, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.handleFn
}

// Serve accepts one connection and runs a request/response loop. Used
// by tests that need a live daemon.
func (d *fakeDaemon) Serve(t *testing.T) {
	t.Helper()
	go func() {
		for {
			conn, err := d.listener.Accept()
			if err != nil {
				if !d.closed.Load() {
					t.Logf("fakeDaemon accept: %v", err)
				}
				return
			}
			d.connsServed.Add(1)
			d.mu.Lock()
			d.conns = append(d.conns, conn)
			d.mu.Unlock()
			go d.serveConn(t, conn)
		}
	}()
}

func (d *fakeDaemon) serveConn(t *testing.T, conn net.Conn) {
	t.Helper()
	defer func() { _ = conn.Close() }()
	for {
		req, readErr := mwn1.ReadFrame(conn, nil)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && !d.closed.Load() {
				t.Logf("fakeDaemon read: %v", readErr)
			}
			return
		}
		// For streaming frames, do not respond until the FlagFinal arrives.
		if req.Flags&mwn1.FlagStreaming != 0 && req.Flags&mwn1.FlagFinal == 0 {
			continue
		}
		handler := d.currentHandler()
		resp, ok := handler(req)
		if !ok {
			continue
		}
		if writeErr := mwn1.WriteFrame(conn, resp, nil); writeErr != nil {
			t.Logf("fakeDaemon write: %v", writeErr)
			return
		}
	}
}

func (d *fakeDaemon) Stop() {
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

// TestDial_Smoke covers the happy path: Dial connects, Call returns
// a typed response, Close is clean.
func TestDial_Smoke(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.Serve(t)
	defer daemon.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := Dial(ctx, Config{Target: "unix://" + daemon.socketPath})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = cli.Close() }()

	resp, err := cli.RPC().Version(ctx, &mwanv1.VersionRequest{})
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if resp.GetVersion() != "fake" {
		t.Fatalf("Version=%q want %q", resp.GetVersion(), "fake")
	}
	if daemon.versionCalls.Load() != 1 {
		t.Fatalf("versionCalls=%d want 1", daemon.versionCalls.Load())
	}
}

// TestDial_NoListenerFails covers the new contract: Dial actively
// connects to the unix socket and returns an error when nothing is
// listening. The bridge daemon expects this and exits so systemd
// restarts it.
func TestDial_NoListenerFails(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp")
	dir := t.TempDir()
	socketPath := dir + "/sock"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Dial(ctx, Config{Target: "unix://" + socketPath})
	if err == nil {
		t.Fatal("Dial with no listener: want error, got nil")
	}
}

// TestDialEmptyTargetRejected covers the input-validation error path.
func TestDialEmptyTargetRejected(t *testing.T) {
	_, err := Dial(context.Background(), Config{Target: ""})
	if err == nil {
		t.Fatal("Dial with empty target: want error, got nil")
	}
}

// TestDial_NonUnixTargetRejected covers the only other structural error.
func TestDial_NonUnixTargetRejected(t *testing.T) {
	_, err := Dial(context.Background(), Config{Target: "tcp://localhost:1234"})
	if err == nil {
		t.Fatal("Dial with tcp:// target: want error, got nil")
	}
}

// TestUnixTargetPath covers the small URL parser used by Dial.
func TestUnixTargetPath(t *testing.T) {
	path, ok := unixTargetPath("unix:///tmp/mwanrpc.sock")
	if !ok {
		t.Fatal("expected unix target")
	}
	if path != "/tmp/mwanrpc.sock" {
		t.Fatalf("path = %q", path)
	}
	if _, ok = unixTargetPath("tcp://localhost:1234"); ok {
		t.Fatalf("unexpected unix target accepted")
	}
	if _, ok = unixTargetPath("unix://"); ok {
		t.Fatal("empty path should be rejected")
	}
}

// TestCall_RemoteError covers the FlagError frame path: the daemon
// answers with FlagError set and an opaque payload; the client
// surfaces a *RemoteError.
func TestCall_RemoteError(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.setHandler(func(req mwn1.Frame) (mwn1.Frame, bool) {
		return mwn1.Frame{
			Flags:    mwn1.FlagFinal | mwn1.FlagError,
			MethodID: req.MethodID,
			CorrID:   req.CorrID,
			Payload:  []byte("remote went boom"),
		}, true
	})
	daemon.Serve(t)
	defer daemon.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := Dial(ctx, Config{Target: "unix://" + daemon.socketPath})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = cli.Close() }()

	_, err = cli.RPC().Version(ctx, &mwanv1.VersionRequest{})
	if err == nil {
		t.Fatal("Version: want error, got nil")
	}
	var remote *RemoteError
	if !errors.As(err, &remote) {
		t.Fatalf("expected *RemoteError, got %T: %v", err, err)
	}
	if remote.MethodID != mwn1.MethodVersion {
		t.Fatalf("RemoteError.MethodID=%d want %d", remote.MethodID, mwn1.MethodVersion)
	}
	if string(remote.Payload) != "remote went boom" {
		t.Fatalf("RemoteError.Payload=%q", remote.Payload)
	}
}

// TestCall_ContextCancel covers ctx cancellation mid-call: the daemon
// never replies, the context fires, Call returns ctx.Err(), and the
// pending map entry is cleaned up.
func TestCall_ContextCancel(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.setHandler(func(req mwn1.Frame) (mwn1.Frame, bool) {
		// Swallow the request; never respond.
		_ = req
		return mwn1.Frame{}, false
	})
	daemon.Serve(t)
	defer daemon.Stop()

	cli, err := Dial(context.Background(), Config{Target: "unix://" + daemon.socketPath})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = cli.RPC().Version(ctx, &mwanv1.VersionRequest{})
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("Version: want ctx.Err(), got %v", err)
	}

	cli.mu.Lock()
	pendingCount := len(cli.pending)
	cli.mu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("pending leak: %d entries left after cancel", pendingCount)
	}
}

// TestCall_ConnectionLoss covers the case where the underlying
// transport closes mid-call. Pending Call returns an error.
func TestCall_ConnectionLoss(t *testing.T) {
	daemon := newFakeDaemon(t)
	connClosed := make(chan struct{})
	daemon.setHandler(func(req mwn1.Frame) (mwn1.Frame, bool) {
		_ = req
		close(connClosed)
		// Don't respond; return false so the loop continues, then
		// Stop() closes the listener.
		return mwn1.Frame{}, false
	})
	daemon.Serve(t)

	cli, err := Dial(context.Background(), Config{Target: "unix://" + daemon.socketPath})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = cli.Close() }()

	probeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, callErr := cli.RPC().Version(ctx, &mwanv1.VersionRequest{})
		probeDone <- callErr
	}()

	select {
	case <-connClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon never received the request")
	}
	// Force the daemon side to drop the connection.
	daemon.Stop()

	select {
	case callErr := <-probeDone:
		if callErr == nil {
			t.Fatal("Version: want error after connection loss, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Call hung past connection loss")
	}
}

// TestCall_ConcurrentRouting covers correct CorrID routing under
// load: 50 concurrent callers each get their own response.
func TestCall_ConcurrentRouting(t *testing.T) {
	daemon := newFakeDaemon(t)
	daemon.Serve(t)
	defer daemon.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := Dial(ctx, Config{Target: "unix://" + daemon.socketPath})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = cli.Close() }()

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			resp, callErr := cli.RPC().Version(ctx, &mwanv1.VersionRequest{})
			if callErr != nil {
				errs <- callErr
				return
			}
			if resp.GetVersion() != "fake" {
				errs <- errors.New("wrong version response")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent Version: %v", e)
	}
}

// proto.Marshal sanity check: we use it to roundtrip the Version
// response inside the daemon helper. If proto changes break that, the
// fake daemon stops working and the test will fail loudly.
var _ proto.Message = (*mwanv1.VersionRequest)(nil)
