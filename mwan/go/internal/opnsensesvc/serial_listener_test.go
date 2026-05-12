package opnsensesvc

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeRWC is a deterministic in-memory io.ReadWriteCloser used to
// stand in for the virtio-serial fd. Reads and writes are tracked so
// tests can confirm that multiple SerialConn wrappers actually share
// the same underlying byte stream.
type fakeRWC struct {
	mu       sync.Mutex
	readBuf  []byte
	writeBuf []byte
	readCh   chan []byte
	closed   bool
}

func newFakeRWC() *fakeRWC {
	return &fakeRWC{
		mu:       sync.Mutex{},
		readBuf:  nil,
		writeBuf: nil,
		readCh:   make(chan []byte, 16),
		closed:   false,
	}
}

func (f *fakeRWC) Read(p []byte) (int, error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return 0, io.EOF
	}
	if len(f.readBuf) > 0 {
		n := copy(p, f.readBuf)
		f.readBuf = f.readBuf[n:]
		f.mu.Unlock()
		return n, nil
	}
	f.mu.Unlock()
	chunk, ok := <-f.readCh
	if !ok {
		return 0, io.EOF
	}
	f.mu.Lock()
	f.readBuf = append(f.readBuf, chunk...)
	n := copy(p, f.readBuf)
	f.readBuf = f.readBuf[n:]
	f.mu.Unlock()
	return n, nil
}

func (f *fakeRWC) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, errors.New("fake: closed")
	}
	f.writeBuf = append(f.writeBuf, p...)
	return len(p), nil
}

func (f *fakeRWC) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.readCh)
	return nil
}

func (f *fakeRWC) pushRead(chunk []byte) {
	f.readCh <- chunk
}

func (f *fakeRWC) writes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]byte, len(f.writeBuf))
	copy(out, f.writeBuf)
	return out
}

// TestOneShotListener_MultipleAcceptsOverSameFd verifies that after
// the first SerialConn is Closed, a second Accept yields a fresh
// wrapper bound to the same underlying fd.
func TestOneShotListener_MultipleAcceptsOverSameFd(t *testing.T) {
	rwc := newFakeRWC()
	listener := NewOneShotListener(rwc)
	defer func() { _ = listener.Close() }()

	first, err := listener.Accept()
	if err != nil {
		t.Fatalf("first Accept: %v", err)
	}
	if _, writeErr := first.Write([]byte("alpha")); writeErr != nil {
		t.Fatalf("first Write: %v", writeErr)
	}
	if closeErr := first.Close(); closeErr != nil {
		t.Fatalf("first Close: %v", closeErr)
	}

	if rwc.closed {
		t.Fatalf("underlying rwc must stay open after first conn close")
	}

	second, err := listener.Accept()
	if err != nil {
		t.Fatalf("second Accept: %v", err)
	}
	if _, writeErr := second.Write([]byte("beta")); writeErr != nil {
		t.Fatalf("second Write: %v", writeErr)
	}
	if closeErr := second.Close(); closeErr != nil {
		t.Fatalf("second Close: %v", closeErr)
	}

	got := string(rwc.writes())
	want := "alphabeta"
	if got != want {
		t.Fatalf("writes: got %q want %q", got, want)
	}
}

// TestOneShotListener_AcceptBlocksUntilCloseOrNextConn confirms that
// the second Accept blocks until the first conn is Closed.
func TestOneShotListener_AcceptBlocksUntilCloseOrNextConn(t *testing.T) {
	rwc := newFakeRWC()
	listener := NewOneShotListener(rwc)
	defer func() { _ = listener.Close() }()

	first, err := listener.Accept()
	if err != nil {
		t.Fatalf("first Accept: %v", err)
	}

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	resCh := make(chan acceptResult, 1)
	go func() {
		c, acceptErr := listener.Accept()
		resCh <- acceptResult{conn: c, err: acceptErr}
	}()

	select {
	case res := <-resCh:
		t.Fatalf("second Accept returned early: conn=%v err=%v", res.conn, res.err)
	case <-time.After(50 * time.Millisecond):
	}

	if closeErr := first.Close(); closeErr != nil {
		t.Fatalf("first Close: %v", closeErr)
	}

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("second Accept after close: %v", res.err)
		}
		_ = res.conn.Close()
	case <-time.After(time.Second):
		t.Fatalf("second Accept did not unblock after first Close")
	}
}

// TestOneShotListener_CloseUnblocksAccept ensures listener Close
// returns ErrClosed to any blocked Accept and closes the underlying fd.
func TestOneShotListener_CloseUnblocksAccept(t *testing.T) {
	rwc := newFakeRWC()
	listener := NewOneShotListener(rwc)

	first, err := listener.Accept()
	if err != nil {
		t.Fatalf("first Accept: %v", err)
	}

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	resCh := make(chan acceptResult, 1)
	go func() {
		c, acceptErr := listener.Accept()
		resCh <- acceptResult{conn: c, err: acceptErr}
	}()

	time.Sleep(20 * time.Millisecond)

	if closeErr := listener.Close(); closeErr != nil {
		t.Fatalf("listener Close: %v", closeErr)
	}

	select {
	case res := <-resCh:
		if !errors.Is(res.err, net.ErrClosed) {
			t.Fatalf("expected net.ErrClosed, got %v", res.err)
		}
	case <-time.After(time.Second):
		t.Fatalf("blocked Accept did not return after listener Close")
	}

	if !rwc.closed {
		t.Fatalf("listener Close must close underlying rwc")
	}

	if closeErr := first.Close(); closeErr != nil {
		t.Fatalf("first Close after listener Close: %v", closeErr)
	}
}

// TestSerialConn_ReadPropagatesEOF confirms that EOF from the fd is
// surfaced to the caller so gRPC tears down its transport.
func TestSerialConn_ReadPropagatesEOF(t *testing.T) {
	rwc := newFakeRWC()
	listener := NewOneShotListener(rwc)
	defer func() { _ = listener.Close() }()

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	rwc.pushRead([]byte("ping"))
	buf := make([]byte, 4)
	if n, readErr := conn.Read(buf); readErr != nil || n != 4 {
		t.Fatalf("Read: n=%d err=%v", n, readErr)
	}
	_ = rwc.Close()
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatalf("expected error after EOF, got nil")
	}
}
