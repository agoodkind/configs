package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"goodkind.io/mwan/internal/mwn1"
)

type bridgeMessage struct {
	methodID uint16
	corrID   uint64
	flags    mwn1.Flags
	payload  []byte
}

type testClient struct {
	conn *mwn1.Conn
	recv chan bridgeMessage
}

type proxyFixture struct {
	ctx          context.Context
	cancel       context.CancelFunc
	listenPath   string
	upstreamPeer net.Conn
	upstreamConn *mwn1.Conn
	bridgeDone   chan error
}

func newProxyFixture(
	t *testing.T,
	upstreamHandler func(*mwn1.Conn, bridgeMessage),
) *proxyFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	upstreamBridge, upstreamPeer := net.Pipe()

	tempDir, err := os.MkdirTemp("/tmp", "mwan-bridge-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})
	listenPath := filepath.Join(tempDir, "bridge.sock")
	listener, err := net.Listen("unix", listenPath)
	if err != nil {
		t.Fatalf("listen bridge: %v", err)
	}

	bridge := newFanInBridge(upstreamBridge, listener, slog.Default())
	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- bridge.serve(ctx)
	}()

	upstreamConn := mwn1.NewConn(upstreamPeer, slog.Default())
	upstreamConn.OnMessage(func(methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) {
		upstreamHandler(upstreamConn, bridgeMessage{
			methodID: methodID,
			corrID:   corrID,
			flags:    flags,
			payload:  payload,
		})
	})

	fixture := &proxyFixture{
		ctx:          ctx,
		cancel:       cancel,
		listenPath:   listenPath,
		upstreamPeer: upstreamPeer,
		upstreamConn: upstreamConn,
		bridgeDone:   bridgeDone,
	}
	t.Cleanup(func() {
		cancel()
		_ = upstreamConn.Close()
		_ = upstreamPeer.Close()
		if fixture.bridgeDone == nil {
			return
		}
		select {
		case <-fixture.bridgeDone:
		case <-time.After(2 * time.Second):
			t.Fatalf("bridge did not stop")
		}
	})
	return fixture
}

func (f *proxyFixture) dialClient(t *testing.T) *testClient {
	t.Helper()
	var dialer net.Dialer
	conn, err := dialer.DialContext(f.ctx, "unix", f.listenPath)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	client := &testClient{
		recv: make(chan bridgeMessage, 128),
	}
	client.conn = mwn1.NewConn(conn, slog.Default())
	client.conn.OnMessage(func(methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) {
		client.recv <- bridgeMessage{
			methodID: methodID,
			corrID:   corrID,
			flags:    flags,
			payload:  payload,
		}
	})
	t.Cleanup(func() {
		_ = client.conn.Close()
	})
	return client
}

func (c *testClient) close() {
	_ = c.conn.Close()
}

func (c *testClient) recvMessage(t *testing.T) bridgeMessage {
	t.Helper()
	return recvMessage(t, c.recv)
}

func sendMessage(conn *mwn1.Conn, methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) error {
	return conn.SendMessage(methodID, corrID, flags, payload)
}

func recvMessage(t *testing.T, recv <-chan bridgeMessage) bridgeMessage {
	t.Helper()
	select {
	case message := <-recv:
		return message
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for message")
	}
	return bridgeMessage{}
}

func echoHandler(t *testing.T, seen chan<- bridgeMessage) func(*mwn1.Conn, bridgeMessage) {
	t.Helper()
	return func(conn *mwn1.Conn, message bridgeMessage) {
		select {
		case seen <- message:
		default:
		}
		err := sendMessage(conn, message.methodID, message.corrID, mwn1.FlagResponse, message.payload)
		if err != nil && !errors.Is(err, mwn1.ErrClosed) {
			t.Errorf("upstream send: %v", err)
		}
	}
}

func TestProxy_SingleProbeEquivalent(t *testing.T) {
	seen := make(chan bridgeMessage, 1)
	fixture := newProxyFixture(t, echoHandler(t, seen))
	client := fixture.dialClient(t)

	if err := sendMessage(client.conn, mwn1.MethodVersion, 42, mwn1.FlagRequest, []byte("probe")); err != nil {
		t.Fatalf("send probe: %v", err)
	}
	got := client.recvMessage(t)
	if got.methodID != mwn1.MethodVersion {
		t.Fatalf("method=%d want %d", got.methodID, mwn1.MethodVersion)
	}
	if got.corrID != 42 {
		t.Fatalf("corr=%d want 42", got.corrID)
	}
	if string(got.payload) != "probe" {
		t.Fatalf("payload=%q want probe", got.payload)
	}

	upstream := recvMessage(t, seen)
	if upstream.corrID == 42 {
		t.Fatalf("upstream corr id was not rewritten")
	}
}

func TestProxy_ConcurrentCorrIDRouting(t *testing.T) {
	const probeCount = 50
	seen := make(chan bridgeMessage, probeCount)
	fixture := newProxyFixture(t, echoHandler(t, seen))

	var wg sync.WaitGroup
	errs := make(chan error, probeCount)
	for i := range probeCount {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := fixture.dialClient(t)
			payload := []byte{byte(i)}
			if err := sendMessage(client.conn, mwn1.MethodVersion, 1, mwn1.FlagRequest, payload); err != nil {
				errs <- err
				return
			}
			got := client.recvMessage(t)
			if got.corrID != 1 || !bytes.Equal(got.payload, payload) {
				errs <- errors.New("misrouted response")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("probe failed: %v", err)
	}

	upstreamCorrs := make(map[uint64]struct{}, probeCount)
	for range probeCount {
		message := recvMessage(t, seen)
		upstreamCorrs[message.corrID] = struct{}{}
	}
	if len(upstreamCorrs) != probeCount {
		t.Fatalf("upstream corr ids=%d want %d", len(upstreamCorrs), probeCount)
	}
}

func TestProxy_InboundCloseMidCallLeavesUpstreamAlive(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	seenCount := 0
	fixture := newProxyFixture(t, func(conn *mwn1.Conn, message bridgeMessage) {
		seenCount++
		if seenCount == 1 {
			requestSeen <- struct{}{}
			<-releaseFirst
			return
		}
		err := sendMessage(conn, message.methodID, message.corrID, mwn1.FlagResponse, message.payload)
		if err != nil && !errors.Is(err, mwn1.ErrClosed) {
			t.Errorf("upstream send: %v", err)
		}
	})

	first := fixture.dialClient(t)
	if err := sendMessage(first.conn, mwn1.MethodVersion, 7, mwn1.FlagRequest, []byte("first")); err != nil {
		t.Fatalf("send first: %v", err)
	}
	<-requestSeen
	first.close()
	close(releaseFirst)

	second := fixture.dialClient(t)
	if err := sendMessage(second.conn, mwn1.MethodVersion, 8, mwn1.FlagRequest, []byte("second")); err != nil {
		t.Fatalf("send second: %v", err)
	}
	got := second.recvMessage(t)
	if got.corrID != 8 || string(got.payload) != "second" {
		t.Fatalf("second response = %+v", got)
	}
}

func TestProxy_InboundCloseMidStreamCancelsUpstream(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	cancelSeen := make(chan bridgeMessage, 1)
	fixture := newProxyFixture(t, func(conn *mwn1.Conn, message bridgeMessage) {
		if message.flags&mwn1.FlagCancel != 0 {
			cancelSeen <- message
			return
		}
		if message.methodID == mwn1.MethodDeploy {
			select {
			case requestSeen <- struct{}{}:
			default:
			}
			if err := conn.SendAck(message.methodID, message.corrID); err != nil &&
				!errors.Is(err, mwn1.ErrClosed) {
				t.Errorf("upstream ack: %v", err)
			}
			return
		}
		err := sendMessage(conn, message.methodID, message.corrID, mwn1.FlagResponse, message.payload)
		if err != nil && !errors.Is(err, mwn1.ErrClosed) {
			t.Errorf("upstream send: %v", err)
		}
	})

	client := fixture.dialClient(t)
	if err := sendMessage(
		client.conn,
		mwn1.MethodDeploy,
		7,
		mwn1.FlagRequest|mwn1.FlagStreaming,
		[]byte("header"),
	); err != nil {
		t.Fatalf("send stream: %v", err)
	}
	<-requestSeen
	client.close()

	cancel := recvMessage(t, cancelSeen)
	if cancel.methodID != mwn1.MethodDeploy || cancel.flags != mwn1.FlagCancel {
		t.Fatalf("cancel = %+v", cancel)
	}

	second := fixture.dialClient(t)
	if err := sendMessage(second.conn, mwn1.MethodVersion, 8, mwn1.FlagRequest, []byte("second")); err != nil {
		t.Fatalf("send second: %v", err)
	}
	got := second.recvMessage(t)
	if got.corrID != 8 || string(got.payload) != "second" {
		t.Fatalf("second response = %+v", got)
	}

	select {
	case err := <-fixture.bridgeDone:
		if err != nil {
			t.Fatalf("bridge returned error: %v", err)
		}
		fixture.bridgeDone = nil
		t.Fatal("bridge stopped after abandoned stream")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestProxy_InboundCloseCancelsWhileUpstreamAckBlocked(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	cancelSeen := make(chan bridgeMessage, 1)
	fixture := newProxyFixture(t, func(_ *mwn1.Conn, message bridgeMessage) {
		if message.flags&mwn1.FlagCancel != 0 {
			cancelSeen <- message
			return
		}
		requestSeen <- struct{}{}
	})
	client := fixture.dialClient(t)

	if err := sendMessage(
		client.conn,
		mwn1.MethodDeploy,
		7,
		mwn1.FlagRequest|mwn1.FlagStreaming,
		[]byte("header"),
	); err != nil {
		t.Fatalf("send stream: %v", err)
	}
	select {
	case <-requestSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive stream")
	}
	client.close()

	cancel := recvMessage(t, cancelSeen)
	if cancel.methodID != mwn1.MethodDeploy || cancel.flags != mwn1.FlagCancel {
		t.Fatalf("cancel = %+v", cancel)
	}
}

func TestProxy_UpstreamDeathStopsBridge(t *testing.T) {
	fixture := newProxyFixture(t, func(*mwn1.Conn, bridgeMessage) {})
	_ = fixture.upstreamPeer.Close()
	select {
	case err := <-fixture.bridgeDone:
		if err != nil {
			t.Fatalf("bridge returned error: %v", err)
		}
		fixture.bridgeDone = nil
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop after upstream death")
	}
}

func TestProxy_LargeMessageRoundTrip(t *testing.T) {
	seen := make(chan bridgeMessage, 1)
	fixture := newProxyFixture(t, echoHandler(t, seen))
	client := fixture.dialClient(t)
	payload := bytes.Repeat([]byte{0x42}, mwn1.MaxPayload*2+17)

	if err := sendMessage(client.conn, mwn1.MethodReadConfigXML, 99, mwn1.FlagRequest, payload); err != nil {
		t.Fatalf("send large message: %v", err)
	}
	got := client.recvMessage(t)
	if got.corrID != 99 || !bytes.Equal(got.payload, payload) {
		t.Fatalf("large round trip mismatch")
	}
}

func TestOpenLocalListener(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.sock")
	listener, err := openLocalListener(context.Background(), path)
	if err != nil {
		t.Fatalf("openLocalListener: %v", err)
	}
	defer func() { _ = listener.Close() }()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%s is not a socket: %v", path, info.Mode())
	}
}
