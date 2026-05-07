package mwn1

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type receivedMessage struct {
	methodID uint16
	corrID   uint64
	flags    Flags
	payload  []byte
}

func linkedConns(t *testing.T) (*Conn, *Conn) {
	t.Helper()
	left, right := net.Pipe()
	connLeft := NewConn(left, slog.Default())
	connRight := NewConn(right, slog.Default())
	t.Cleanup(func() {
		_ = connLeft.Close()
		_ = connRight.Close()
	})
	return connLeft, connRight
}

func waitForMessage(t *testing.T, ch <-chan receivedMessage) receivedMessage {
	t.Helper()
	select {
	case message := <-ch:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
	return receivedMessage{}
}

func sendAndReceive(t *testing.T, payload []byte) receivedMessage {
	t.Helper()
	sender, receiver := linkedConns(t)
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{
			methodID: methodID,
			corrID:   corrID,
			flags:    flags,
			payload:  gotPayload,
		}
	})
	err := sender.SendMessage(7, 99, FlagRequest, payload)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	return waitForMessage(t, messageCh)
}

func TestConn_RoundTripEmpty(t *testing.T) {
	got := sendAndReceive(t, nil)
	if got.methodID != 7 || got.corrID != 99 {
		t.Fatalf("wrong message identity: %+v", got)
	}
	if got.flags != FlagRequest {
		t.Fatalf("flags = %08b, want %08b", got.flags, FlagRequest)
	}
	if len(got.payload) != 0 {
		t.Fatalf("payload length = %d, want 0", len(got.payload))
	}
}

func TestConn_RoundTripSubFrame(t *testing.T) {
	payload := []byte("sub-frame payload")
	got := sendAndReceive(t, payload)
	if !bytes.Equal(got.payload, payload) {
		t.Fatalf("payload mismatch")
	}
	if got.flags != FlagRequest {
		t.Fatalf("unexpected flags: %08b", got.flags)
	}
}

func TestConn_ExactFrameUsesOneFinalFrame(t *testing.T) {
	payload := bytes.Repeat([]byte{0xa5}, MaxPayload)
	outbound, inbound := net.Pipe()
	conn := NewConn(outbound, slog.Default())
	t.Cleanup(func() {
		_ = conn.Close()
		_ = inbound.Close()
	})
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- conn.SendMessage(3, 4, FlagRequest, payload)
	}()
	got, err := readFrame(inbound, slog.Default())
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got.flagsForTest() != FlagRequest {
		t.Fatalf("flags = %08b, want %08b", got.Flags, FlagRequest)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Fatalf("payload mismatch")
	}
	select {
	case err := <-sendDone:
		if err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for SendMessage")
	}
}

func TestConn_MultiFrameReassembly(t *testing.T) {
	payload := bytes.Repeat([]byte{0x42}, MaxPayload*2+17)
	got := sendAndReceive(t, payload)
	if !bytes.Equal(got.payload, payload) {
		t.Fatalf("payload mismatch")
	}
	if got.flags != FlagRequest {
		t.Fatalf("flags = %08b, want %08b", got.flags, FlagRequest)
	}
}

func TestConn_SendCancelRoundTrip(t *testing.T) {
	sender, receiver := linkedConns(t)
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{
			methodID: methodID,
			corrID:   corrID,
			flags:    flags,
			payload:  gotPayload,
		}
	})
	if err := sender.SendCancel(11, 99); err != nil {
		t.Fatalf("SendCancel: %v", err)
	}
	got := waitForMessage(t, messageCh)
	if got.methodID != 11 || got.corrID != 99 {
		t.Fatalf("wrong cancel identity: %+v", got)
	}
	if got.flags != FlagCancel {
		t.Fatalf("flags = %08b, want %08b", got.flags, FlagCancel)
	}
	if len(got.payload) != 0 {
		t.Fatalf("payload length = %d, want 0", len(got.payload))
	}
}

func TestConn_SendAckIsConsumedInternally(t *testing.T) {
	sender, receiver := linkedConns(t)
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{
			methodID: methodID,
			corrID:   corrID,
			flags:    flags,
			payload:  gotPayload,
		}
	})
	if err := sender.SendAck(11, 99); err != nil {
		t.Fatalf("SendAck: %v", err)
	}
	select {
	case got := <-messageCh:
		t.Fatalf("ACK delivered to handler: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestConn_SendStreamMessageWaitsForAck(t *testing.T) {
	sender, receiver := linkedConns(t)
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{methodID: methodID, corrID: corrID, flags: flags, payload: gotPayload}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- sender.SendStreamMessage(ctx, 11, 99, FlagRequest|FlagStreaming, []byte("chunk"))
	}()

	got := waitForMessage(t, messageCh)
	if got.methodID != 11 || got.corrID != 99 || string(got.payload) != "chunk" {
		t.Fatalf("stream message = %+v", got)
	}
	select {
	case err := <-sendDone:
		t.Fatalf("SendStreamMessage returned before ACK: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := receiver.SendAck(11, 99); err != nil {
		t.Fatalf("SendAck: %v", err)
	}
	select {
	case err := <-sendDone:
		if err != nil {
			t.Fatalf("SendStreamMessage: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendStreamMessage did not return after ACK")
	}
}

func TestConn_SendStreamMessageAcksTransportFragments(t *testing.T) {
	sender, receiver := linkedConns(t)
	payload := bytes.Repeat([]byte("a"), streamFramePayload*2+17)
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{methodID: methodID, corrID: corrID, flags: flags, payload: gotPayload}
		if err := receiver.SendAck(methodID, corrID); err != nil {
			t.Errorf("SendAck: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sender.SendStreamMessage(ctx, 11, 99, FlagRequest|FlagStreaming, payload); err != nil {
		t.Fatalf("SendStreamMessage: %v", err)
	}
	got := waitForMessage(t, messageCh)
	if !bytes.Equal(got.payload, payload) {
		t.Fatalf("payload mismatch len=%d want=%d", len(got.payload), len(payload))
	}
}

func TestConn_WrongCorrIDAckDoesNotUnblock(t *testing.T) {
	sender, receiver := linkedConns(t)
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{methodID: methodID, corrID: corrID, flags: flags, payload: gotPayload}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- sender.SendStreamMessage(ctx, 11, 99, FlagRequest|FlagStreaming, []byte("chunk"))
	}()
	_ = waitForMessage(t, messageCh)
	if err := receiver.SendAck(11, 100); err != nil {
		t.Fatalf("wrong SendAck: %v", err)
	}
	select {
	case err := <-sendDone:
		t.Fatalf("SendStreamMessage returned after wrong ACK: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := receiver.SendAck(11, 99); err != nil {
		t.Fatalf("right SendAck: %v", err)
	}
	select {
	case err := <-sendDone:
		if err != nil {
			t.Fatalf("SendStreamMessage: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendStreamMessage did not return after right ACK")
	}
}

func TestConn_WrongMethodAckDoesNotUnblock(t *testing.T) {
	sender, receiver := linkedConns(t)
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{methodID: methodID, corrID: corrID, flags: flags, payload: gotPayload}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- sender.SendStreamMessage(ctx, 11, 99, FlagRequest|FlagStreaming, []byte("chunk"))
	}()
	_ = waitForMessage(t, messageCh)
	if err := receiver.SendAck(12, 99); err != nil {
		t.Fatalf("wrong method SendAck: %v", err)
	}
	select {
	case err := <-sendDone:
		t.Fatalf("SendStreamMessage returned after wrong method ACK: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := receiver.SendAck(11, 99); err != nil {
		t.Fatalf("right method SendAck: %v", err)
	}
	select {
	case err := <-sendDone:
		if err != nil {
			t.Fatalf("SendStreamMessage: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendStreamMessage did not return after right method ACK")
	}
}

func TestConn_SendStreamMessageContextTimeoutCleansWaiter(t *testing.T) {
	sender, receiver := linkedConns(t)
	receiver.OnMessage(func(uint16, uint64, Flags, []byte) {})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := sender.SendStreamMessage(ctx, 11, 99, FlagRequest|FlagStreaming, []byte("chunk"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendStreamMessage err=%v want deadline", err)
	}
	sender.ackMu.Lock()
	waiterCount := len(sender.ackWaiters)
	sender.ackMu.Unlock()
	if waiterCount != 0 {
		t.Fatalf("ack waiter leak: %d", waiterCount)
	}
}

func TestConn_SendCancelCleansAckWaiter(t *testing.T) {
	sender, receiver := linkedConns(t)
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{methodID: methodID, corrID: corrID, flags: flags, payload: gotPayload}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- sender.SendStreamMessage(ctx, 11, 99, FlagRequest|FlagStreaming, []byte("chunk"))
	}()
	_ = waitForMessage(t, messageCh)
	if err := sender.SendCancel(11, 99); err != nil {
		t.Fatalf("SendCancel: %v", err)
	}
	select {
	case err := <-sendDone:
		if !errors.Is(err, ErrStreamCanceled) {
			t.Fatalf("SendStreamMessage err=%v want ErrStreamCanceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendStreamMessage did not return after cancel")
	}
}

func TestConn_ConcurrentStreamMessagesDifferentCorrIDs(t *testing.T) {
	sender, receiver := linkedConns(t)
	messageCh := make(chan receivedMessage, 8)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{methodID: methodID, corrID: corrID, flags: flags, payload: gotPayload}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const count = 4
	errs := make(chan error, count)
	for i := range count {
		corrID := uint64(100 + i)
		go func() {
			errs <- sender.SendStreamMessage(ctx, 11, corrID, FlagRequest|FlagStreaming, []byte("chunk"))
		}()
	}
	seen := make(map[uint64]struct{}, count)
	for range count {
		got := waitForMessage(t, messageCh)
		seen[got.corrID] = struct{}{}
	}
	for corrID := range seen {
		if err := receiver.SendAck(11, corrID); err != nil {
			t.Fatalf("SendAck corr=%d: %v", corrID, err)
		}
	}
	for range count {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("SendStreamMessage: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for SendStreamMessage")
		}
	}
}

func TestConn_CancelBypassesQueuedNormalFrame(t *testing.T) {
	rw := newBlockingWriteRWC()
	conn := NewConn(rw, slog.Default())
	t.Cleanup(func() {
		_ = conn.Close()
	})

	if err := conn.SendMessage(1, 1, FlagRequest, []byte("first")); err != nil {
		t.Fatalf("send first: %v", err)
	}
	firstWrite := rw.waitForWrite(t)
	if firstWrite.CorrID != 1 {
		t.Fatalf("first write corr=%d want 1", firstWrite.CorrID)
	}
	if err := conn.SendMessage(1, 2, FlagRequest, []byte("second")); err != nil {
		t.Fatalf("send second: %v", err)
	}
	if err := conn.SendCancel(1, 3); err != nil {
		t.Fatalf("send cancel: %v", err)
	}

	rw.releaseOne()
	secondWrite := rw.waitForWrite(t)
	if secondWrite.Flags != FlagCancel || secondWrite.CorrID != 3 {
		t.Fatalf("second write = flags %08b corr %d, want cancel corr 3",
			secondWrite.Flags, secondWrite.CorrID)
	}
	rw.releaseOne()
	thirdWrite := rw.waitForWrite(t)
	if thirdWrite.CorrID != 2 {
		t.Fatalf("third write corr=%d want queued normal corr 2", thirdWrite.CorrID)
	}
	rw.releaseOne()
}

func TestConn_MaxMessageAtConfiguredLimit(t *testing.T) {
	payload := bytes.Repeat([]byte{0x11}, MaxPayload*3+31)
	left, right := net.Pipe()
	sender := NewConn(left, slog.Default())
	receiver := newConnWithReassemblyLimit(right, slog.Default(), len(payload))
	t.Cleanup(func() {
		_ = sender.Close()
		_ = receiver.Close()
	})
	messageCh := make(chan receivedMessage, 1)
	receiver.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		messageCh <- receivedMessage{methodID: methodID, corrID: corrID, flags: flags, payload: gotPayload}
	})
	if err := sender.SendMessage(8, 55, FlagRequest, payload); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	got := waitForMessage(t, messageCh)
	if !bytes.Equal(got.payload, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestConn_ReassemblyCapExceededSendsError(t *testing.T) {
	left, right := net.Pipe()
	sender := NewConn(left, slog.Default())
	receiver := newConnWithReassemblyLimit(right, slog.Default(), MaxPayload+1)
	t.Cleanup(func() {
		_ = sender.Close()
		_ = receiver.Close()
	})
	delivered := make(chan struct{}, 1)
	receiver.OnMessage(func(uint16, uint64, Flags, []byte) {
		delivered <- struct{}{}
	})
	errorCh := make(chan receivedMessage, 1)
	sender.OnMessage(func(methodID uint16, corrID uint64, flags Flags, gotPayload []byte) {
		errorCh <- receivedMessage{methodID: methodID, corrID: corrID, flags: flags, payload: gotPayload}
	})
	payload := bytes.Repeat([]byte{0x7f}, MaxPayload+2)
	if err := sender.SendMessage(9, 77, FlagRequest, payload); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	got := waitForMessage(t, errorCh)
	if got.methodID != 9 || got.corrID != 77 {
		t.Fatalf("wrong error identity: %+v", got)
	}
	if got.flags != FlagResponse|FlagError {
		t.Fatalf("flags = %08b, want %08b", got.flags, FlagResponse|FlagError)
	}
	if string(got.payload) != reassemblyOverflowResponse {
		t.Fatalf("payload = %q, want %q", got.payload, reassemblyOverflowResponse)
	}
	select {
	case <-delivered:
		t.Fatalf("oversized message was delivered")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestConn_ReaderEOFCancelsWriter(t *testing.T) {
	left, right := net.Pipe()
	conn := NewConn(left, slog.Default())
	_ = right.Close()
	select {
	case <-conn.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("Done did not close after peer EOF")
	}
	if conn.Err() == nil {
		t.Fatalf("Err is nil after peer EOF")
	}
	err := conn.SendMessage(1, 1, FlagRequest, []byte("late"))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("SendMessage = %v, want ErrClosed", err)
	}
}

func TestConn_WriterErrorCancelsReader(t *testing.T) {
	rw := newFailingReadWriteCloser()
	conn := NewConn(rw, slog.Default())
	err := conn.SendMessage(1, 1, FlagRequest, []byte("payload"))
	if err != nil {
		t.Fatalf("initial SendMessage: %v", err)
	}
	select {
	case <-conn.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("Done did not close after writer error")
	}
	if conn.Err() == nil {
		t.Fatalf("Err is nil after writer error")
	}
	if !rw.closed.Load() {
		t.Fatalf("underlying rwc was not closed")
	}
}

func TestConn_ConcurrentSenders(t *testing.T) {
	const senderCount = 8
	const perSender = 50
	sender, receiver := linkedConns(t)
	seen := make(chan uint64, senderCount*perSender)
	receiver.OnMessage(func(_ uint16, corrID uint64, _ Flags, _ []byte) {
		seen <- corrID
	})
	var waitGroup sync.WaitGroup
	for senderIndex := 0; senderIndex < senderCount; senderIndex++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			for i := 0; i < perSender; i++ {
				corrID := uint64(index*1000 + i)
				err := sender.SendMessage(1, corrID, FlagRequest, []byte("payload"))
				if err != nil {
					t.Errorf("SendMessage: %v", err)
					return
				}
			}
		}(senderIndex)
	}
	waitGroup.Wait()
	wantCount := senderCount * perSender
	got := make(map[uint64]bool, wantCount)
	for len(got) < wantCount {
		select {
		case corrID := <-seen:
			got[corrID] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for messages; got %d/%d", len(got), wantCount)
		}
	}
}

func TestConn_ThousandFrameThroughput(t *testing.T) {
	const frameCount = 1000
	payload := bytes.Repeat([]byte{0x23}, MaxPayload*frameCount)
	startedAt := time.Now()
	got := sendAndReceive(t, payload)
	if !bytes.Equal(got.payload, payload) {
		t.Fatalf("payload mismatch")
	}
	if time.Since(startedAt) > 5*time.Second {
		t.Fatalf("1000-frame round trip took %s", time.Since(startedAt))
	}
}

type failingReadWriteCloser struct {
	closed atomic.Bool
	pipeR  net.Conn
	pipeW  net.Conn
}

type blockingWriteRWC struct {
	writeCh   chan frame
	releaseCh chan struct{}
	closeCh   chan struct{}
	closeOnce sync.Once
}

func newBlockingWriteRWC() *blockingWriteRWC {
	return &blockingWriteRWC{
		writeCh:   make(chan frame, 8),
		releaseCh: make(chan struct{}),
		closeCh:   make(chan struct{}),
	}
}

func (r *blockingWriteRWC) Read([]byte) (int, error) {
	<-r.closeCh
	return 0, io.EOF
}

func (r *blockingWriteRWC) Write(payload []byte) (int, error) {
	got, err := readFrame(bytes.NewReader(payload), slog.Default())
	if err != nil {
		return 0, err
	}
	select {
	case r.writeCh <- got:
	case <-r.closeCh:
		return 0, io.ErrClosedPipe
	}
	select {
	case <-r.releaseCh:
		return len(payload), nil
	case <-r.closeCh:
		return 0, io.ErrClosedPipe
	}
}

func (r *blockingWriteRWC) Close() error {
	r.closeOnce.Do(func() {
		close(r.closeCh)
	})
	return nil
}

func (r *blockingWriteRWC) waitForWrite(t *testing.T) frame {
	t.Helper()
	select {
	case got := <-r.writeCh:
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for write")
		return frame{}
	}
}

func (r *blockingWriteRWC) releaseOne() {
	r.releaseCh <- struct{}{}
}

func newFailingReadWriteCloser() *failingReadWriteCloser {
	pipeR, pipeW := net.Pipe()
	return &failingReadWriteCloser{pipeR: pipeR, pipeW: pipeW}
}

func (f *failingReadWriteCloser) Read(payload []byte) (int, error) {
	return f.pipeR.Read(payload)
}

func (f *failingReadWriteCloser) Write([]byte) (int, error) {
	return 0, errors.New("forced write failure")
}

func (f *failingReadWriteCloser) Close() error {
	if f.closed.Swap(true) {
		return nil
	}
	_ = f.pipeR.Close()
	return f.pipeW.Close()
}

type nullCloser struct {
	io.ReadWriter
}

func (nullCloser) Close() error {
	return nil
}

func (f frame) flagsForTest() Flags {
	return f.Flags
}
