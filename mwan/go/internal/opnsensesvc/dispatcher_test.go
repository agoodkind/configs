package opnsensesvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
)

// pipeRWC adapts a net.Conn to io.ReadWriteCloser.
type pipeRWC struct {
	net.Conn
}

// startDispatcher wires a Server to an in-memory net.Pipe and returns
// the client-side rwc plus a stop func that cancels the dispatcher and
// waits for it to exit.
func newRegistryOrFail(t *testing.T) *mwn1.Registry {
	t.Helper()
	reg, err := mwn1.NewMWANOPNsenseRegistry()
	if err != nil {
		t.Fatalf("NewMWANOPNsenseRegistry: %v", err)
	}
	return reg
}

func startDispatcher(t *testing.T, srv *Server) (clientRWC io.ReadWriteCloser, stop func()) {
	t.Helper()
	srvSide, cliSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	d, err := NewDispatcher(DispatcherConfig{
		Registry:     nil,
		Server:       srv,
		Workers:      4,
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
	stop = func() {
		cancel()
		_ = cliSide.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("dispatcher Serve did not exit")
		}
	}
	return &pipeRWC{cliSide}, stop
}

// roundTrip writes a single request frame and reads a single response
// frame, asserting the corr_id matches.
func roundTrip(t *testing.T, w io.Writer, r io.Reader, methodID uint16, corrID uint64, payload []byte, flags mwn1.Flags) mwn1.Frame {
	t.Helper()
	if err := mwn1.WriteFrame(w, mwn1.Frame{
		Flags:    flags | mwn1.FlagRequest,
		MethodID: methodID,
		CorrID:   corrID,
		Payload:  payload,
	}, slog.Default()); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	resp, err := mwn1.ReadFrame(r, slog.Default())
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if resp.CorrID != corrID {
		t.Fatalf("corr_id mismatch: got %d want %d", resp.CorrID, corrID)
	}
	return resp
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)),
		"/tmp/nonexistent-config.xml", t.TempDir())
}

func mustMarshalRequest(t *testing.T, reg *mwn1.Registry, methodID uint16, msg proto.Message) []byte {
	t.Helper()
	payload, _, err := mwn1.MarshalRequest(reg, methodID, msg)
	if err != nil {
		t.Fatalf("MarshalRequest: %v", err)
	}
	return payload
}

func TestDispatcherSingleVersionRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	rwc, stop := startDispatcher(t, srv)
	defer stop()

	reg := newRegistryOrFail(t)
	payload := mustMarshalRequest(t, reg, mwn1.MethodVersion, &mwanv1.VersionRequest{})
	resp := roundTrip(t, rwc, rwc, mwn1.MethodVersion, 42, payload, mwn1.FlagFinal)

	if resp.Flags&mwn1.FlagError != 0 {
		t.Fatalf("unexpected error frame: %x", resp.Payload)
	}
	out := &mwanv1.VersionResponse{}
	if err := proto.Unmarshal(resp.Payload, out); err != nil {
		t.Fatalf("unmarshal VersionResponse: %v", err)
	}
	if out.GetVersion() == "" {
		t.Fatalf("empty Version: %+v", out)
	}
}

func TestDispatcherSequentialRoundTrips(t *testing.T) {
	srv := newTestServer(t)
	rwc, stop := startDispatcher(t, srv)
	defer stop()

	reg := newRegistryOrFail(t)
	payload := mustMarshalRequest(t, reg, mwn1.MethodVersion, &mwanv1.VersionRequest{})

	for i := uint64(1); i <= 100; i++ {
		resp := roundTrip(t, rwc, rwc, mwn1.MethodVersion, i, payload, mwn1.FlagFinal)
		if resp.Flags&mwn1.FlagError != 0 {
			t.Fatalf("iter %d: error frame", i)
		}
		if resp.CorrID != i {
			t.Fatalf("iter %d: corr_id mismatch %d", i, resp.CorrID)
		}
	}
}

// TestDispatcherConcurrentRoundTrips fires 100 goroutines, each
// sending a request and waiting for the matching response. A single
// goroutine reads responses off the wire and demultiplexes them by
// CorrID into per-request channels.
func TestDispatcherConcurrentRoundTrips(t *testing.T) {
	srv := newTestServer(t)
	rwc, stop := startDispatcher(t, srv)
	defer stop()

	reg := newRegistryOrFail(t)
	payload := mustMarshalRequest(t, reg, mwn1.MethodVersion, &mwanv1.VersionRequest{})

	const concurrency = 100
	pending := make(map[uint64]chan mwn1.Frame, concurrency)
	var pendingMu sync.Mutex
	for i := uint64(1); i <= concurrency; i++ {
		pending[i] = make(chan mwn1.Frame, 1)
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for i := 0; i < concurrency; i++ {
			f, err := mwn1.ReadFrame(rwc, slog.Default())
			if err != nil {
				return
			}
			pendingMu.Lock()
			ch := pending[f.CorrID]
			pendingMu.Unlock()
			if ch != nil {
				ch <- f
			}
		}
	}()

	// Serialize writes (the wire is a single io.Writer; concurrent
	// WriteFrame calls would interleave bytes).
	var writeMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(concurrency)
	failures := atomic.Int32{}
	for i := uint64(1); i <= concurrency; i++ {
		go func(corr uint64) {
			defer wg.Done()
			writeMu.Lock()
			err := mwn1.WriteFrame(rwc, mwn1.Frame{
				Flags:    mwn1.FlagRequest | mwn1.FlagFinal,
				MethodID: mwn1.MethodVersion,
				CorrID:   corr,
				Payload:  payload,
			}, slog.Default())
			writeMu.Unlock()
			if err != nil {
				failures.Add(1)
				return
			}
			select {
			case f := <-pending[corr]:
				if f.Flags&mwn1.FlagError != 0 {
					failures.Add(1)
				}
			case <-time.After(5 * time.Second):
				failures.Add(1)
			}
		}(i)
	}
	wg.Wait()
	<-readerDone
	if failures.Load() != 0 {
		t.Fatalf("%d concurrent round-trips failed", failures.Load())
	}
}

func TestDispatcherUnknownMethodIDReturnsError(t *testing.T) {
	srv := newTestServer(t)
	rwc, stop := startDispatcher(t, srv)
	defer stop()

	// Method id 9999 is not registered.
	resp := roundTrip(t, rwc, rwc, 9999, 7, nil, mwn1.FlagFinal)
	if resp.Flags&mwn1.FlagError == 0 {
		t.Fatalf("expected FlagError, got flags=%x", resp.Flags)
	}
	st := &status.Status{}
	if err := proto.Unmarshal(resp.Payload, st); err != nil {
		t.Fatalf("unmarshal Status: %v", err)
	}
	if !strings.Contains(st.GetMessage(), "9999") {
		t.Fatalf("expected message to mention method id, got %q", st.GetMessage())
	}
}

func TestDispatcherHandlerErrorReturnsErrorFrame(t *testing.T) {
	srv := newTestServer(t)
	rwc, stop := startDispatcher(t, srv)
	defer stop()

	reg := newRegistryOrFail(t)
	// WriteConfigXML with empty content returns "content empty".
	payload := mustMarshalRequest(t, reg, mwn1.MethodWriteConfigXML, &mwanv1.WriteConfigXMLRequest{})
	resp := roundTrip(t, rwc, rwc, mwn1.MethodWriteConfigXML, 11, payload, mwn1.FlagFinal)
	if resp.Flags&mwn1.FlagError == 0 {
		t.Fatalf("expected FlagError")
	}
	st := &status.Status{}
	if err := proto.Unmarshal(resp.Payload, st); err != nil {
		t.Fatalf("unmarshal Status: %v", err)
	}
	if !strings.Contains(st.GetMessage(), "content empty") {
		t.Fatalf("expected handler error message, got %q", st.GetMessage())
	}
}

// TestDispatcherGarbagePrefix injects 200 random bytes ahead of a real
// request frame. The framer's resync logic must drop the garbage and
// the handler must still run.
func TestDispatcherGarbagePrefix(t *testing.T) {
	srv := newTestServer(t)
	rwc, stop := startDispatcher(t, srv)
	defer stop()

	reg := newRegistryOrFail(t)
	payload := mustMarshalRequest(t, reg, mwn1.MethodVersion, &mwanv1.VersionRequest{})

	garbage := make([]byte, 200)
	if _, err := rand.Read(garbage); err != nil {
		t.Fatalf("rand: %v", err)
	}
	// Stamp out any accidental "MWN1" magic in the garbage so we are
	// not dependent on the framer's CRC catching a crafted lookalike.
	for i := 0; i+4 <= len(garbage); i++ {
		if string(garbage[i:i+4]) == mwn1.Magic {
			garbage[i] = 0
		}
	}
	if _, err := rwc.Write(garbage); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	resp := roundTrip(t, rwc, rwc, mwn1.MethodVersion, 99, payload, mwn1.FlagFinal)
	if resp.Flags&mwn1.FlagError != 0 {
		t.Fatalf("unexpected error frame after garbage: %x", resp.Payload)
	}
	out := &mwanv1.VersionResponse{}
	if err := proto.Unmarshal(resp.Payload, out); err != nil {
		t.Fatalf("unmarshal VersionResponse: %v", err)
	}
}

// TestDispatcherStreamingDeploy sends header, three data chunks, and a
// trailer as five frames sharing one CorrID. The handler must
// reassemble them and produce one DeployResponse frame.
func TestDispatcherStreamingDeploy(t *testing.T) {
	tmpBinaryDir := t.TempDir()
	deployManager := NewDeployManager(slog.Default(), DeployConfig{
		BinaryDir: tmpBinaryDir,
		StatePath: tmpBinaryDir + "/state.json",
		ReExecFn:  func(_ string, _ []string, _ []string) error { return nil },
	})
	srv := NewServerWithDeploy(slog.New(slog.NewTextHandler(io.Discard, nil)),
		"/tmp/nonexistent-config.xml", t.TempDir(), deployManager)

	rwc, stop := startDispatcher(t, srv)
	defer stop()

	reg := newRegistryOrFail(t)
	const corr = uint64(555)
	dataParts := [][]byte{
		[]byte("part-1-bytes"),
		[]byte("part-2-bytes-larger"),
		[]byte("part-3-end"),
	}
	hasher := sha256.New()
	for _, part := range dataParts {
		hasher.Write(part)
	}
	sumHex := hex.EncodeToString(hasher.Sum(nil))

	frames := []*mwanv1.Chunk{
		{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{Label: "test"}}},
		{Body: &mwanv1.Chunk_Data{Data: dataParts[0]}},
		{Body: &mwanv1.Chunk_Data{Data: dataParts[1]}},
		{Body: &mwanv1.Chunk_Data{Data: dataParts[2]}},
		{Body: &mwanv1.Chunk_Trailer{Trailer: &mwanv1.ChunkTrailer{Sha256Hex: sumHex}}},
	}

	for i, chunk := range frames {
		payload := mustMarshalRequest(t, reg, mwn1.MethodDeploy, chunk)
		flags := mwn1.FlagRequest | mwn1.FlagStreaming
		if i == len(frames)-1 {
			flags |= mwn1.FlagFinal
		}
		if err := mwn1.WriteFrame(rwc, mwn1.Frame{
			Flags:    flags,
			MethodID: mwn1.MethodDeploy,
			CorrID:   corr,
			Payload:  payload,
		}, slog.Default()); err != nil {
			t.Fatalf("WriteFrame chunk %d: %v", i, err)
		}
	}

	resp, err := mwn1.ReadFrame(rwc, slog.Default())
	if err != nil {
		t.Fatalf("ReadFrame response: %v", err)
	}
	if resp.CorrID != corr {
		t.Fatalf("corr_id mismatch: %d", resp.CorrID)
	}
	if resp.Flags&mwn1.FlagError != 0 {
		st := &status.Status{}
		_ = proto.Unmarshal(resp.Payload, st)
		t.Fatalf("Deploy returned error: %s", st.GetMessage())
	}
	out := &mwanv1.DeployResponse{}
	if err := proto.Unmarshal(resp.Payload, out); err != nil {
		t.Fatalf("unmarshal DeployResponse: %v", err)
	}
	if out.GetStagedSha256() != sumHex {
		t.Fatalf("staged sha mismatch: %s vs %s", out.GetStagedSha256(), sumHex)
	}
}

// TestDispatcherServeReturnsOnContextCancel verifies that a clean
// context cancellation produces a nil error from Serve.
func TestDispatcherServeReturnsOnContextCancel(t *testing.T) {
	srv := newTestServer(t)
	srvSide, cliSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	d, err := NewDispatcher(DispatcherConfig{
		Registry:     nil,
		Server:       srv,
		Workers:      0,
		Log:          slog.Default(),
		OnFrameError: nil,
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
