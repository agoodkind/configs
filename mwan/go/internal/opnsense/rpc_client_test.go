package opnsense

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
	"goodkind.io/mwan/internal/opnsensesvc"
)

type fakeRequest struct {
	conn     *mwn1.Conn
	methodID uint16
	corrID   uint64
	flags    mwn1.Flags
	payload  []byte
}

type fakeResponse struct {
	methodID uint16
	flags    mwn1.Flags
	payload  []byte
	send     bool
}

type fakeMWN1Daemon struct {
	socketPath string
	listener   net.Listener
	reg        *mwn1.Registry

	mu       sync.Mutex
	handleFn func(fakeRequest) fakeResponse
	ackFn    func(fakeRequest) fakeResponse
	conns    []*mwn1.Conn

	versionCalls atomic.Int32
	closed       atomic.Bool
}

func newFakeMWN1Daemon(t *testing.T) *fakeMWN1Daemon {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mwan-opnsense-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	socketPath := filepath.Join(dir, "sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	reg, err := mwn1.NewMWANOPNsenseRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	daemon := &fakeMWN1Daemon{
		socketPath: socketPath,
		listener:   listener,
		reg:        reg,
	}
	daemon.handleFn = daemon.defaultEcho
	daemon.ackFn = daemon.defaultAck
	return daemon
}

func (d *fakeMWN1Daemon) defaultEcho(req fakeRequest) fakeResponse {
	if req.methodID == mwn1.MethodVersion {
		d.versionCalls.Add(1)
		resp := &mwanv1.VersionResponse{Version: "fake", BuildCommit: fmt.Sprintf("%d", req.corrID)}
		payload, _, _ := mwn1.MarshalResponse(d.reg, mwn1.MethodVersion, resp)
		return fakeResponse{
			methodID: req.methodID,
			flags:    mwn1.FlagResponse,
			payload:  payload,
			send:     true,
		}
	}
	resp, ok := d.reg.NewResponse(req.methodID)
	if !ok {
		return fakeResponse{}
	}
	payload, _, _ := mwn1.MarshalResponse(d.reg, req.methodID, resp)
	return fakeResponse{
		methodID: req.methodID,
		flags:    mwn1.FlagResponse,
		payload:  payload,
		send:     true,
	}
}

func (d *fakeMWN1Daemon) defaultAck(req fakeRequest) fakeResponse {
	if req.flags&mwn1.FlagStreaming == 0 || req.flags&mwn1.FlagCancel != 0 {
		return fakeResponse{}
	}
	return fakeResponse{
		methodID: req.methodID,
		flags:    mwn1.FlagAck,
		payload:  nil,
		send:     true,
	}
}

func (d *fakeMWN1Daemon) setHandler(fn func(fakeRequest) fakeResponse) {
	d.mu.Lock()
	d.handleFn = fn
	d.mu.Unlock()
}

func (d *fakeMWN1Daemon) setAckHandler(fn func(fakeRequest) fakeResponse) {
	d.mu.Lock()
	d.ackFn = fn
	d.mu.Unlock()
}

func (d *fakeMWN1Daemon) currentHandler() func(fakeRequest) fakeResponse {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.handleFn
}

func (d *fakeMWN1Daemon) currentAckHandler() func(fakeRequest) fakeResponse {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ackFn
}

func (d *fakeMWN1Daemon) Serve(t *testing.T) {
	t.Helper()
	go func() {
		for {
			conn, err := d.listener.Accept()
			if err != nil {
				if !d.closed.Load() {
					t.Logf("fake daemon accept: %v", err)
				}
				return
			}
			d.serveConn(conn)
		}
	}()
}

func (d *fakeMWN1Daemon) serveConn(conn net.Conn) {
	mwnConn := mwn1.NewConn(conn, slog.Default())
	d.mu.Lock()
	d.conns = append(d.conns, mwnConn)
	d.mu.Unlock()
	mwnConn.OnMessage(func(methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) {
		req := fakeRequest{
			conn:     mwnConn,
			methodID: methodID,
			corrID:   corrID,
			flags:    flags,
			payload:  payload,
		}
		ackHandler := d.currentAckHandler()
		ack := ackHandler(req)
		if ack.send {
			_ = mwnConn.SendMessage(ack.methodID, corrID, ack.flags, ack.payload)
		}
		handler := d.currentHandler()
		resp := handler(req)
		if !resp.send {
			return
		}
		_ = mwnConn.SendMessage(resp.methodID, corrID, resp.flags, resp.payload)
	})
}

func (d *fakeMWN1Daemon) Stop() {
	d.closed.Store(true)
	_ = d.listener.Close()
	d.mu.Lock()
	conns := d.conns
	d.conns = nil
	d.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}

func TestCall_SequentialRouting(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	daemon.Serve(t)
	defer daemon.Stop()

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := range 50 {
		resp, err := client.RPC().Version(ctx, &mwanv1.VersionRequest{})
		if err != nil {
			t.Fatalf("Version %d: %v", i, err)
		}
		if resp.GetVersion() != "fake" {
			t.Fatalf("Version=%q want fake", resp.GetVersion())
		}
	}
	if got := daemon.versionCalls.Load(); got != 50 {
		t.Fatalf("versionCalls=%d want 50", got)
	}
}

func TestWriteConfigXMLLargePayloadThroughDispatcher(t *testing.T) {
	tempDir, err := os.MkdirTemp("/tmp", "mwan126-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})
	socketPath := filepath.Join(tempDir, "mwan-opnsense.sock")
	configPath := filepath.Join(tempDir, "config.xml")
	backupDir := filepath.Join(tempDir, "backup")
	initialConfig := []byte("<opnsense><system><hostname>old</hostname></system></opnsense>")
	if err := os.WriteFile(configPath, initialConfig, 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := opnsensesvc.NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		configPath,
		backupDir,
	)
	dispatcher, err := opnsensesvc.NewDispatcher(opnsensesvc.DispatcherConfig{
		Server: server,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serveDone <- acceptErr
			return
		}
		serveDone <- dispatcher.Serve(ctx, conn)
	}()

	client, err := Dial("unix://" + socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	content := []byte("<opnsense><system><hostname>")
	content = append(content, bytes.Repeat([]byte("m"), 155_546-len(content)-len("</hostname></system></opnsense>"))...)
	content = append(content, []byte("</hostname></system></opnsense>")...)
	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()
	resp, err := client.RPC().WriteConfigXML(callCtx, &mwanv1.WriteConfigXMLRequest{
		Content: content,
		Label:   "large-local",
	})
	if err != nil {
		t.Fatalf("WriteConfigXML: %v", err)
	}
	if resp.GetBytesWritten() != int64(len(content)) {
		t.Fatalf("bytes_written=%d want %d", resp.GetBytesWritten(), len(content))
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("written config mismatch")
	}
	backup, err := os.ReadFile(resp.GetBackupPath())
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(backup, initialConfig) {
		t.Fatalf("backup mismatch")
	}

	cancel()
	_ = client.Close()
	select {
	case serveErr := <-serveDone:
		if serveErr != nil {
			t.Fatalf("dispatcher Serve: %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("dispatcher did not stop")
	}
}

func TestCall_ConcurrentRouting(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	daemon.Serve(t)
	defer daemon.Stop()

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const callCount = 50
	var wg sync.WaitGroup
	errs := make(chan error, callCount)
	for range callCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, callErr := client.RPC().Version(ctx, &mwanv1.VersionRequest{})
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
	for err := range errs {
		t.Errorf("concurrent Version: %v", err)
	}
}

func TestCall_FlagErrorStatus(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	statusPayload, err := proto.Marshal(&spb.Status{
		Code:    int32(codes.FailedPrecondition),
		Message: "remote went boom",
	})
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	daemon.setHandler(func(req fakeRequest) fakeResponse {
		return fakeResponse{
			methodID: req.methodID,
			flags:    mwn1.FlagResponse | mwn1.FlagError,
			payload:  statusPayload,
			send:     true,
		}
	})
	daemon.Serve(t)
	defer daemon.Stop()

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err = client.RPC().Version(context.Background(), &mwanv1.VersionRequest{})
	if err == nil {
		t.Fatal("Version: want error, got nil")
	}
	if got, want := grpcstatus.Code(err), codes.FailedPrecondition; got != want {
		t.Fatalf("status.Code=%v want %v (err=%v)", got, want, err)
	}
}

func TestCall_ContextCancelNoLeak(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	daemon.setHandler(func(fakeRequest) fakeResponse {
		return fakeResponse{}
	})
	daemon.Serve(t)
	defer daemon.Stop()

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = client.RPC().Version(ctx, &mwanv1.VersionRequest{})
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("Version: want context error, got %v", err)
	}
	client.mu.Lock()
	pendingCount := len(client.pending)
	client.mu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("pending leak: %d entries left", pendingCount)
	}
}

func TestCallStream_ContextCancelSendsCancelNoLeak(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	cancelSeen := make(chan fakeRequest, 1)
	daemon.setHandler(func(req fakeRequest) fakeResponse {
		if req.flags&mwn1.FlagCancel != 0 {
			cancelSeen <- req
		}
		return fakeResponse{}
	})
	daemon.Serve(t)
	defer daemon.Stop()

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	producerStarted := make(chan struct{})
	callDone := make(chan error, 1)
	go func() {
		_, callErr := client.CallStream(ctx, mwn1.MethodDeploy, func(send func(proto.Message) error) error {
			chunk := &mwanv1.Chunk{
				Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{Label: "cancel-test"}},
			}
			if sendErr := send(chunk); sendErr != nil {
				return sendErr
			}
			close(producerStarted)
			<-ctx.Done()
			return ctx.Err()
		})
		callDone <- callErr
	}()

	select {
	case <-producerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not start")
	}
	cancel()

	select {
	case callErr := <-callDone:
		if !errors.Is(callErr, context.Canceled) {
			t.Fatalf("CallStream error = %v, want context.Canceled", callErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CallStream did not return after context cancel")
	}

	select {
	case got := <-cancelSeen:
		if got.methodID != mwn1.MethodDeploy || got.flags != mwn1.FlagCancel {
			t.Fatalf("cancel frame = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not receive cancel")
	}

	client.mu.Lock()
	pendingCount := len(client.pending)
	client.mu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("pending leak: %d entries left", pendingCount)
	}
}

func TestCallStreamSendWaitsForAck(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	releaseAck := make(chan struct{})
	firstStreamingFrame := make(chan struct{}, 1)
	daemon.setAckHandler(func(req fakeRequest) fakeResponse {
		if req.flags&mwn1.FlagStreaming == 0 || req.flags&mwn1.FlagCancel != 0 {
			return fakeResponse{}
		}
		firstStreamingFrame <- struct{}{}
		<-releaseAck
		return daemon.defaultAck(req)
	})
	daemon.setHandler(func(fakeRequest) fakeResponse {
		return fakeResponse{}
	})
	daemon.Serve(t)
	defer daemon.Stop()

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstSendReturned := make(chan struct{})
	go func() {
		_, _ = client.CallStream(ctx, mwn1.MethodDeploy, func(send func(proto.Message) error) error {
			chunk := &mwanv1.Chunk{
				Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{Label: "ack-test"}},
			}
			_ = send(chunk)
			close(firstSendReturned)
			return context.Canceled
		})
	}()

	select {
	case <-firstStreamingFrame:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not receive first streaming frame")
	}
	select {
	case <-firstSendReturned:
		t.Fatal("stream Send returned before ACK")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseAck)
	select {
	case <-firstSendReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("stream Send did not return after ACK")
	}
}

func TestCallStreamNoAckBlocksSecondSendUntilContextCancel(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	requests := make(chan fakeRequest, 4)
	daemon.setAckHandler(func(req fakeRequest) fakeResponse {
		if req.flags&mwn1.FlagStreaming != 0 && req.flags&mwn1.FlagCancel == 0 {
			requests <- req
		}
		return fakeResponse{}
	})
	daemon.setHandler(func(fakeRequest) fakeResponse {
		return fakeResponse{}
	})
	daemon.Serve(t)
	defer daemon.Stop()

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	sendCount := atomic.Int32{}
	_, err = client.CallStream(ctx, mwn1.MethodDeploy, func(send func(proto.Message) error) error {
		chunk := &mwanv1.Chunk{
			Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{Label: "no-ack"}},
		}
		sendCount.Add(1)
		if sendErr := send(chunk); sendErr != nil {
			return sendErr
		}
		sendCount.Add(1)
		return send(chunk)
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallStream error = %v, want context deadline", err)
	}
	if got := sendCount.Load(); got != 1 {
		t.Fatalf("sendCount=%d want 1", got)
	}
	if got := len(requests); got != 1 {
		t.Fatalf("streaming requests=%d want 1", got)
	}
}

func TestCallStreamWrongMethodAckBlocksSecondSendUntilContextCancel(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	requests := make(chan fakeRequest, 4)
	daemon.setAckHandler(func(req fakeRequest) fakeResponse {
		if req.flags&mwn1.FlagStreaming == 0 || req.flags&mwn1.FlagCancel != 0 {
			return fakeResponse{}
		}
		requests <- req
		return fakeResponse{
			methodID: req.methodID + 1,
			flags:    mwn1.FlagAck,
			payload:  nil,
			send:     true,
		}
	})
	daemon.setHandler(func(fakeRequest) fakeResponse {
		return fakeResponse{}
	})
	daemon.Serve(t)
	defer daemon.Stop()

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	sendCount := atomic.Int32{}
	_, err = client.CallStream(ctx, mwn1.MethodDeploy, func(send func(proto.Message) error) error {
		chunk := &mwanv1.Chunk{
			Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{Label: "wrong-method-ack"}},
		}
		sendCount.Add(1)
		if sendErr := send(chunk); sendErr != nil {
			return sendErr
		}
		sendCount.Add(1)
		return send(chunk)
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallStream error = %v, want context deadline", err)
	}
	if got := sendCount.Load(); got != 1 {
		t.Fatalf("sendCount=%d want 1", got)
	}
	if got := len(requests); got != 1 {
		t.Fatalf("streaming requests=%d want 1", got)
	}
}

func TestCall_ConnectionLossMidCall(t *testing.T) {
	daemon := newFakeMWN1Daemon(t)
	requestSeen := make(chan struct{}, 1)
	daemon.setHandler(func(fakeRequest) fakeResponse {
		requestSeen <- struct{}{}
		return fakeResponse{}
	})
	daemon.Serve(t)

	client, err := Dial("unix://" + daemon.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	callDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, callErr := client.RPC().Version(ctx, &mwanv1.VersionRequest{})
		callDone <- callErr
	}()

	select {
	case <-requestSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon never received request")
	}
	daemon.Stop()

	select {
	case callErr := <-callDone:
		if callErr == nil {
			t.Fatal("Version: want error after connection loss, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Call hung past connection loss")
	}
}
