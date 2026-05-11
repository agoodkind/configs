package opnsensesvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
)

type pipeRWC struct {
	net.Conn
}

type rpcResponse struct {
	methodID uint16
	corrID   uint64
	flags    mwn1.Flags
	payload  []byte
}

type testClient struct {
	conn    *mwn1.Conn
	reg     *mwn1.Registry
	pending map[uint64]chan rpcResponse
	mu      sync.Mutex
}

func newRegistryOrFail(t *testing.T) *mwn1.Registry {
	t.Helper()
	reg, err := mwn1.NewMWANOPNsenseRegistry()
	if err != nil {
		t.Fatalf("NewMWANOPNsenseRegistry: %v", err)
	}
	return reg
}

func newTestClient(rwc io.ReadWriteCloser, reg *mwn1.Registry) *testClient {
	client := &testClient{
		conn:    mwn1.NewConn(rwc, slog.New(slog.NewTextHandler(io.Discard, nil))),
		reg:     reg,
		pending: make(map[uint64]chan rpcResponse),
	}
	client.conn.OnMessage(func(methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) {
		client.mu.Lock()
		ch := client.pending[corrID]
		client.mu.Unlock()
		if ch != nil {
			ch <- rpcResponse{
				methodID: methodID,
				corrID:   corrID,
				flags:    flags,
				payload:  payload,
			}
		}
	})
	return client
}

func (c *testClient) call(
	t *testing.T,
	methodID uint16,
	corrID uint64,
	req proto.Message,
) rpcResponse {
	t.Helper()
	payload, _, err := mwn1.MarshalRequest(c.reg, methodID, req)
	if err != nil {
		t.Fatalf("MarshalRequest method %d: %v", methodID, err)
	}
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[corrID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, corrID)
		c.mu.Unlock()
	}()
	if err := c.conn.SendMessage(methodID, corrID, mwn1.FlagRequest, payload); err != nil {
		t.Fatalf("SendMessage method %d: %v", methodID, err)
	}
	select {
	case resp := <-ch:
		if resp.methodID != methodID {
			t.Fatalf("method_id mismatch: got %d want %d", resp.methodID, methodID)
		}
		return resp
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for method %d corr %d", methodID, corrID)
		return rpcResponse{}
	}
}

func (c *testClient) streamDeploy(
	t *testing.T,
	corrID uint64,
	chunks []*mwanv1.Chunk,
	separateFinal bool,
) rpcResponse {
	t.Helper()
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[corrID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, corrID)
		c.mu.Unlock()
	}()
	for i, chunk := range chunks {
		payload, _, err := mwn1.MarshalRequest(c.reg, mwn1.MethodDeploy, chunk)
		if err != nil {
			t.Fatalf("MarshalRequest Deploy chunk %d: %v", i, err)
		}
		flags := mwn1.FlagRequest | mwn1.FlagStreaming
		if i == len(chunks)-1 && !separateFinal {
			flags |= mwn1.FlagFinal
		}
		if err := c.conn.SendMessage(mwn1.MethodDeploy, corrID, flags, payload); err != nil {
			t.Fatalf("SendMessage Deploy chunk %d: %v", i, err)
		}
	}
	if separateFinal {
		err := c.conn.SendMessage(
			mwn1.MethodDeploy,
			corrID,
			mwn1.FlagRequest|mwn1.FlagStreaming|mwn1.FlagFinal,
			nil,
		)
		if err != nil {
			t.Fatalf("SendMessage Deploy final: %v", err)
		}
	}
	select {
	case resp := <-ch:
		return resp
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for Deploy corr %d", corrID)
		return rpcResponse{}
	}
}

func (c *testClient) registerResponse(corrID uint64) chan rpcResponse {
	ch := make(chan rpcResponse, 8)
	c.mu.Lock()
	c.pending[corrID] = ch
	c.mu.Unlock()
	return ch
}

func (c *testClient) unregisterResponse(corrID uint64) {
	c.mu.Lock()
	delete(c.pending, corrID)
	c.mu.Unlock()
}

func (c *testClient) sendDeployChunk(
	t *testing.T,
	corrID uint64,
	chunk *mwanv1.Chunk,
	flags mwn1.Flags,
) {
	t.Helper()
	payload, _, err := mwn1.MarshalRequest(c.reg, mwn1.MethodDeploy, chunk)
	if err != nil {
		t.Fatalf("MarshalRequest Deploy chunk: %v", err)
	}
	if err := c.conn.SendMessage(mwn1.MethodDeploy, corrID, flags, payload); err != nil {
		t.Fatalf("SendMessage Deploy chunk: %v", err)
	}
}

func startDispatcher(t *testing.T, srv *Server) (*testClient, func()) {
	t.Helper()
	srvSide, cliSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	d, err := NewDispatcher(DispatcherConfig{
		Registry: nil,
		Server:   srv,
		Workers:  4,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- d.Serve(ctx, &pipeRWC{srvSide})
	}()
	client := newTestClient(&pipeRWC{cliSide}, newRegistryOrFail(t))
	stop := func() {
		cancel()
		_ = client.conn.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("dispatcher Serve did not exit")
		}
	}
	return client, stop
}

func newTestServer(t *testing.T, configContent []byte) *Server {
	t.Helper()
	tempDir := t.TempDir()
	configPath := tempDir + "/config.xml"
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	deployManager := NewDeployManager(slog.New(slog.NewTextHandler(io.Discard, nil)), DeployConfig{
		BinaryDir:   tempDir,
		StatePath:   tempDir + "/state.json",
		PendingPath: tempDir + "/pending-verify",
		ReExecFn:    func(_ string, _ []string, _ []string) error { return nil },
	})
	return NewServerWithDeploy(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		configPath,
		tempDir,
		deployManager,
	)
}

func dispatcherSampleConfig() []byte {
	return []byte(`<opnsense><system><hostname>mwan</hostname></system><gateways><gateway_item><name>WAN_GW6</name><gateway>fe80::1</gateway></gateway_item></gateways></opnsense>`)
}

func assertNoErrorFrame(t *testing.T, resp rpcResponse) {
	t.Helper()
	if resp.flags&mwn1.FlagError != 0 {
		st := &status.Status{}
		_ = proto.Unmarshal(resp.payload, st)
		t.Fatalf("unexpected error frame: %s", st.GetMessage())
	}
}

func assertErrorMessageContains(t *testing.T, resp rpcResponse, want string) {
	t.Helper()
	if resp.flags&mwn1.FlagError == 0 {
		t.Fatalf("expected FlagError, got flags=%x", resp.flags)
	}
	st := &status.Status{}
	if err := proto.Unmarshal(resp.payload, st); err != nil {
		t.Fatalf("unmarshal Status: %v", err)
	}
	if !strings.Contains(st.GetMessage(), want) {
		t.Fatalf("expected message to contain %q, got %q", want, st.GetMessage())
	}
}

func TestDispatcherAllRPCs(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcher(t, srv)
	defer stop()

	cases := []struct {
		name     string
		methodID uint16
		request  proto.Message
		wantErr  string
	}{
		{"Version", mwn1.MethodVersion, &mwanv1.VersionRequest{}, ""},
		{"Exec", mwn1.MethodExec, &mwanv1.ExecRequest{Command: "/bin/echo", Args: []string{"ok"}}, ""},
		{"ReadConfigXML", mwn1.MethodReadConfigXML, &mwanv1.ReadConfigXMLRequest{}, ""},
		{"WriteConfigXML", mwn1.MethodWriteConfigXML, &mwanv1.WriteConfigXMLRequest{Content: dispatcherSampleConfig(), Label: "write"}, ""},
		{"BackupConfigXML", mwn1.MethodBackupConfigXML, &mwanv1.BackupConfigXMLRequest{Label: "backup"}, ""},
		{"XPathGet", mwn1.MethodXPathGet, &mwanv1.XPathGetRequest{Expression: "/opnsense/system/hostname/text()"}, ""},
		{"XPathSet", mwn1.MethodXPathSet, &mwanv1.XPathSetRequest{Expression: "/opnsense/system/hostname", NewValue: "mwan2"}, ""},
		{"XPathDelete", mwn1.MethodXPathDelete, &mwanv1.XPathDeleteRequest{Expression: "/opnsense/system/nonexistent"}, ""},
		{"StripGatewayV6", mwn1.MethodStripGatewayV6, &mwanv1.StripGatewayV6Request{}, ""},
		{"InjectGatewayV6", mwn1.MethodInjectGatewayV6, &mwanv1.InjectGatewayV6Request{GatewayName: "WAN_GW6"}, ""},
		{"DeployStatus", mwn1.MethodDeployStatus, &mwanv1.DeployStatusRequest{}, ""},
		{"Revert", mwn1.MethodRevert, &mwanv1.RevertRequest{}, "previous absent"},
		{"Reset", mwn1.MethodReset, &mwanv1.ResetRequest{}, ""},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := client.call(t, tc.methodID, uint64(i+1), tc.request)
			if tc.wantErr != "" {
				assertErrorMessageContains(t, resp, tc.wantErr)
				return
			}
			if resp.flags&mwn1.FlagError != 0 {
				st := &status.Status{}
				_ = proto.Unmarshal(resp.payload, st)
				t.Fatalf("method %s returned error: %s", tc.name, st.GetMessage())
			}
		})
	}

	resp := deployRoundTrip(t, client, 100, false)
	assertNoErrorFrame(t, resp)
}

// TestDispatcherReset_DrainsQueuedJobs proves that the Reset RPC
// pulls pending dispatchJobs out of frameCh and reports the count.
// The test uses a single-worker dispatcher so the second and third
// Exec frames sit in the queue while the first is held by a long
// sleep. Reset then cancels the in-flight handler and drains the
// two queued jobs. The DrainedJobs count surfaces in the response
// payload and is the operator-visible signal that the reset
// happened.
func TestDispatcherReset_DrainsQueuedJobs(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcherWithWorkers(t, srv, 1)
	defer stop()

	// Three Exec(sleep 10) jobs. With Workers=1 the first occupies
	// the worker and the rest fill frameCh (capacity = workers*2 = 2).
	holdCh1 := client.registerResponse(900)
	holdCh2 := client.registerResponse(901)
	holdCh3 := client.registerResponse(902)
	defer client.unregisterResponse(900)
	defer client.unregisterResponse(901)
	defer client.unregisterResponse(902)

	sendExecSleep(t, client, 900)
	sendExecSleep(t, client, 901)
	sendExecSleep(t, client, 902)

	// Give the dispatcher time to drain the conn buffer into frameCh.
	time.Sleep(200 * time.Millisecond)

	resetResp := client.call(t, mwn1.MethodReset, 950, &mwanv1.ResetRequest{})
	assertNoErrorFrame(t, resetResp)
	resp := decodeResetResponse(t, resetResp.payload)
	if resp.GetDrainedJobs() < 1 {
		t.Fatalf("DrainedJobs=%d; expected at least 1 queued job to drain", resp.GetDrainedJobs())
	}

	// In-flight sleep is cancelled by Reset; the Exec handler returns
	// with ExitCode=-1 (signal killed). Drain the response channels so
	// the client does not retain stale frames.
	drainExecResponse(t, holdCh1)
	// The two queued jobs were drained without being executed, so they
	// produce no response. The client never sees them complete.
	select {
	case unexpected := <-holdCh2:
		t.Fatalf("expected drained queue entry to produce no response, got %+v", unexpected)
	case <-time.After(200 * time.Millisecond):
	}
	select {
	case unexpected := <-holdCh3:
		t.Fatalf("expected drained queue entry to produce no response, got %+v", unexpected)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestDispatcherReset_CancelsInflightHandler proves that an in-flight
// Exec handler sees its context cancelled when Reset fires and
// returns with ExitCode=-1. This is the visibility signal that the
// reset actually reached the running handler and not just the queue.
func TestDispatcherReset_CancelsInflightHandler(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcherWithWorkers(t, srv, 1)
	defer stop()

	execCh := client.registerResponse(910)
	defer client.unregisterResponse(910)
	sendExecSleep(t, client, 910)

	// Give the worker time to pick the job up and enter sleep.
	time.Sleep(200 * time.Millisecond)

	resetResp := client.call(t, mwn1.MethodReset, 951, &mwanv1.ResetRequest{})
	assertNoErrorFrame(t, resetResp)

	execResp := drainExecResponse(t, execCh)
	if execResp.GetExitCode() == 0 {
		t.Fatalf("expected Exec to be cancelled (non-zero exit), got ExitCode=0 stdout=%q stderr=%q",
			execResp.GetStdout(), execResp.GetStderr())
	}
}

// startDispatcherWithWorkers mirrors startDispatcher but lets the
// caller pin the worker count. Returns the dispatcher so watchdog
// tests can call its methods directly.
func startDispatcherWithWorkers(t *testing.T, srv *Server, workers int) (*testClient, func()) {
	t.Helper()
	client, _, stop := startDispatcherWithWorkersAndRef(t, srv, workers)
	return client, stop
}

// startDispatcherWithWorkersAndRef is like startDispatcherWithWorkers
// but also returns the dispatcher reference so watchdog tests can
// inspect wedgeCount and invoke RunWatchdog directly.
func startDispatcherWithWorkersAndRef(t *testing.T, srv *Server, workers int) (*testClient, *Dispatcher, func()) {
	t.Helper()
	srvSide, cliSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	d, err := NewDispatcher(DispatcherConfig{
		Registry:     nil,
		Server:       srv,
		Workers:      workers,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnFrameError: nil,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- d.Serve(ctx, &pipeRWC{srvSide})
	}()
	client := newTestClient(&pipeRWC{cliSide}, newRegistryOrFail(t))
	stop := func() {
		cancel()
		_ = client.conn.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("dispatcher Serve did not exit")
		}
	}
	return client, d, stop
}

// sendExecSleep submits an Exec(/bin/sleep 10) RPC without waiting
// for the response. The caller registers a response channel ahead of
// time via client.registerResponse so the handler reply is captured
// once Reset fires.
func sendExecSleep(t *testing.T, client *testClient, corrID uint64) {
	t.Helper()
	req := &mwanv1.ExecRequest{
		Command:        "/bin/sleep",
		Args:           []string{"10"},
		TimeoutSeconds: 30,
	}
	payload, _, err := mwn1.MarshalRequest(client.reg, mwn1.MethodExec, req)
	if err != nil {
		t.Fatalf("MarshalRequest Exec: %v", err)
	}
	if err := client.conn.SendMessage(mwn1.MethodExec, corrID, mwn1.FlagRequest, payload); err != nil {
		t.Fatalf("SendMessage Exec corr=%d: %v", corrID, err)
	}
}

// drainExecResponse waits for the Exec response and unmarshals the
// payload. Used in Reset tests where the Exec handler is expected to
// return after its context is cancelled.
func drainExecResponse(t *testing.T, ch chan rpcResponse) *mwanv1.ExecResponse {
	t.Helper()
	select {
	case resp := <-ch:
		if resp.flags&mwn1.FlagError != 0 {
			st := &status.Status{}
			_ = proto.Unmarshal(resp.payload, st)
			t.Fatalf("Exec returned error frame: %s", st.GetMessage())
		}
		out := &mwanv1.ExecResponse{}
		if err := proto.Unmarshal(resp.payload, out); err != nil {
			t.Fatalf("unmarshal ExecResponse: %v", err)
		}
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Exec response after Reset")
		return nil
	}
}

// decodeResetResponse unmarshals a Reset response payload.
func decodeResetResponse(t *testing.T, payload []byte) *mwanv1.ResetResponse {
	t.Helper()
	out := &mwanv1.ResetResponse{}
	if err := proto.Unmarshal(payload, out); err != nil {
		t.Fatalf("unmarshal ResetResponse: %v", err)
	}
	return out
}

func TestDispatcherReadConfigXMLLargeResponseFragments(t *testing.T) {
	largeContent := []byte("<opnsense><system><hostname>")
	largeContent = append(largeContent, []byte(strings.Repeat("a", mwn1.MaxPayload+4096))...)
	largeContent = append(largeContent, []byte("</hostname></system></opnsense>")...)
	srv := newTestServer(t, largeContent)
	client, stop := startDispatcher(t, srv)
	defer stop()

	resp := client.call(t, mwn1.MethodReadConfigXML, 42, &mwanv1.ReadConfigXMLRequest{})
	assertNoErrorFrame(t, resp)
	out := &mwanv1.ReadConfigXMLResponse{}
	if err := proto.Unmarshal(resp.payload, out); err != nil {
		t.Fatalf("unmarshal ReadConfigXMLResponse: %v", err)
	}
	if len(out.GetContent()) != len(largeContent) {
		t.Fatalf("content size got %d want %d", len(out.GetContent()), len(largeContent))
	}
	if !proto.Equal(out, &mwanv1.ReadConfigXMLResponse{
		Content:   largeContent,
		SizeBytes: int64(len(largeContent)),
		Sha256:    dispatcherSHA256Hex(largeContent),
	}) {
		t.Fatalf("large response content mismatch")
	}
}

func TestDispatcherStreamingDeploy(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcher(t, srv)
	defer stop()

	resp := deployRoundTrip(t, client, 555, false)
	assertNoErrorFrame(t, resp)
	out := &mwanv1.DeployResponse{}
	if err := proto.Unmarshal(resp.payload, out); err != nil {
		t.Fatalf("unmarshal DeployResponse: %v", err)
	}
	if out.GetStagedSha256() == "" {
		t.Fatalf("empty staged sha")
	}
}

func TestDispatcherStreamingDeployWithSeparateFinal(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcher(t, srv)
	defer stop()

	resp := deployRoundTrip(t, client, 556, true)
	assertNoErrorFrame(t, resp)
	out := &mwanv1.DeployResponse{}
	if err := proto.Unmarshal(resp.payload, out); err != nil {
		t.Fatalf("unmarshal DeployResponse: %v", err)
	}
	if out.GetStagedSha256() == "" {
		t.Fatalf("empty staged sha")
	}
}

func TestDispatcherCancelTombstonesDeployStream(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcher(t, srv)
	defer stop()

	const corrID uint64 = 700
	responseCh := client.registerResponse(corrID)
	defer client.unregisterResponse(corrID)

	client.sendDeployChunk(
		t,
		corrID,
		&mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{Label: "cancel-test"}}},
		mwn1.FlagRequest|mwn1.FlagStreaming,
	)
	if err := client.conn.SendCancel(mwn1.MethodDeploy, corrID); err != nil {
		t.Fatalf("SendCancel: %v", err)
	}
	client.sendDeployChunk(
		t,
		corrID,
		&mwanv1.Chunk{Body: &mwanv1.Chunk_Data{Data: []byte("stale")}},
		mwn1.FlagRequest|mwn1.FlagStreaming,
	)
	if err := client.conn.SendMessage(
		mwn1.MethodDeploy,
		corrID,
		mwn1.FlagRequest|mwn1.FlagStreaming|mwn1.FlagFinal,
		nil,
	); err != nil {
		t.Fatalf("SendMessage stale final: %v", err)
	}

	select {
	case resp := <-responseCh:
		t.Fatalf("unexpected response after cancel: %+v", resp)
	case <-time.After(200 * time.Millisecond):
	}

	resp := client.call(t, mwn1.MethodVersion, 701, &mwanv1.VersionRequest{})
	assertNoErrorFrame(t, resp)
}

func TestDispatcherAcksAcceptedStreamingChunk(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcher(t, srv)
	defer stop()

	payload, _, err := mwn1.MarshalRequest(
		client.reg,
		mwn1.MethodDeploy,
		&mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{Label: "ack-test"}}},
	)
	if err != nil {
		t.Fatalf("MarshalRequest Deploy header: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.conn.SendStreamMessage(
		ctx,
		mwn1.MethodDeploy,
		702,
		mwn1.FlagRequest|mwn1.FlagStreaming,
		payload,
	); err != nil {
		t.Fatalf("SendStreamMessage header: %v", err)
	}
	if err := client.conn.SendCancel(mwn1.MethodDeploy, 702); err != nil {
		t.Fatalf("SendCancel: %v", err)
	}
}

func TestDispatcherDoesNotAckMalformedStreamingPayload(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcher(t, srv)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := client.conn.SendStreamMessage(
		ctx,
		mwn1.MethodDeploy,
		703,
		mwn1.FlagRequest|mwn1.FlagStreaming,
		[]byte("not protobuf"),
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendStreamMessage err=%v want context deadline", err)
	}
}

func TestDispatcherHandlerErrorReturnsFlagError(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcher(t, srv)
	defer stop()

	resp := client.call(
		t,
		mwn1.MethodWriteConfigXML,
		11,
		&mwanv1.WriteConfigXMLRequest{},
	)
	assertErrorMessageContains(t, resp, "content empty")
}

func TestDispatcherServeReturnsOnConnectionClose(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	srvSide, cliSide := net.Pipe()
	ctx := context.Background()
	d, err := NewDispatcher(DispatcherConfig{
		Server: srv,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- d.Serve(ctx, &pipeRWC{srvSide})
	}()
	_ = cliSide.Close()
	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "connection closed") &&
			!errors.Is(err, io.ErrClosedPipe) &&
			!errors.Is(err, io.EOF) {
			t.Fatalf("expected clean return or connection close error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestDispatcherServeReturnsOnContextCancel(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	srvSide, cliSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	d, err := NewDispatcher(DispatcherConfig{
		Server: srv,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- d.Serve(ctx, &pipeRWC{srvSide})
	}()
	cancel()
	_ = cliSide.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("expected nil err on ctx cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func deployRoundTrip(
	t *testing.T,
	client *testClient,
	corrID uint64,
	separateFinal bool,
) rpcResponse {
	t.Helper()
	dataParts := [][]byte{
		[]byte("part-1-bytes"),
		[]byte("part-2-bytes-larger"),
		[]byte("part-3-end"),
	}
	hasher := sha256.New()
	for _, part := range dataParts {
		_, _ = hasher.Write(part)
	}
	sumHex := hex.EncodeToString(hasher.Sum(nil))

	chunks := []*mwanv1.Chunk{
		{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{Label: "test"}}},
		{Body: &mwanv1.Chunk_Data{Data: dataParts[0]}},
		{Body: &mwanv1.Chunk_Data{Data: dataParts[1]}},
		{Body: &mwanv1.Chunk_Data{Data: dataParts[2]}},
		{Body: &mwanv1.Chunk_Trailer{Trailer: &mwanv1.ChunkTrailer{Sha256Hex: sumHex}}},
	}
	return client.streamDeploy(t, corrID, chunks, separateFinal)
}

func dispatcherSHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
