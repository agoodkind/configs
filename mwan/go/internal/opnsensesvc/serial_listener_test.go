package opnsensesvc

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// fakeRWC backs a SerialListener in tests with an in-memory pipe.
type fakeRWC struct {
	*io.PipeReader
	*io.PipeWriter

	mu     sync.Mutex
	closed bool
}

func (f *fakeRWC) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	_ = f.PipeReader.Close()
	_ = f.PipeWriter.Close()
	return nil
}

type deadlineFakeRWC struct {
	mu                sync.Mutex
	readDeadlineCount int
}

func (f *deadlineFakeRWC) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (f *deadlineFakeRWC) Write(p []byte) (int, error) {
	return len(p), nil
}

func (f *deadlineFakeRWC) Close() error {
	return nil
}

func (f *deadlineFakeRWC) SetDeadline(time.Time) error {
	return nil
}

func (f *deadlineFakeRWC) SetReadDeadline(time.Time) error {
	f.mu.Lock()
	f.readDeadlineCount++
	f.mu.Unlock()
	return nil
}

func (f *deadlineFakeRWC) SetWriteDeadline(time.Time) error {
	return nil
}

func (f *deadlineFakeRWC) deadlineCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readDeadlineCount
}

type timeoutFakeRWC struct{}

func (f *timeoutFakeRWC) Read(_ []byte) (int, error) {
	return 0, os.ErrDeadlineExceeded
}

func (f *timeoutFakeRWC) Write(p []byte) (int, error) {
	return len(p), nil
}

func (f *timeoutFakeRWC) Close() error {
	return nil
}

func (f *timeoutFakeRWC) SetReadDeadline(time.Time) error {
	return nil
}

func (f *timeoutFakeRWC) SetWriteDeadline(time.Time) error {
	return nil
}

type closeCountingRWC struct {
	mu         sync.Mutex
	closeCount int
}

func (f *closeCountingRWC) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (f *closeCountingRWC) Write(p []byte) (int, error) {
	return len(p), nil
}

func (f *closeCountingRWC) Close() error {
	f.mu.Lock()
	f.closeCount++
	f.mu.Unlock()
	return nil
}

func (f *closeCountingRWC) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCount
}

func newFakeOpener(payload string) func(string) (io.ReadWriteCloser, error) {
	return func(_ string) (io.ReadWriteCloser, error) {
		pr, pw := io.Pipe()
		go func() {
			_, _ = pw.Write([]byte(payload))
			_ = pw.Close()
		}()
		return &fakeRWC{PipeReader: pr, PipeWriter: pw}, nil
	}
}

// newTestListener constructs a listener for tests.
func newTestListener(t *testing.T, devPath string, openFn func(string) (io.ReadWriteCloser, error)) *SerialListener {
	t.Helper()
	l, err := NewSerialListener(devPath, openFn)
	if err != nil {
		t.Fatalf("NewSerialListener: %v", err)
	}
	return l
}

// TestSerialListener_NewOpensImmediately confirms the device is opened
// at construction (the persistent-device model), not deferred to Accept.
func TestSerialListener_NewOpensImmediately(t *testing.T) {
	opens := 0
	opener := func(_ string) (io.ReadWriteCloser, error) {
		opens++
		return &closeCountingRWC{}, nil
	}
	l := newTestListener(t, "/dev/test", opener)
	defer func() { _ = l.Close() }()

	if opens != 1 {
		t.Fatalf("opens at construction = %d, want 1", opens)
	}
}

// TestSerialListener_NewOpenerError returns the openFn error, no
// listener is constructed.
func TestSerialListener_NewOpenerError(t *testing.T) {
	want := errors.New("device gone")
	l, err := NewSerialListener("/dev/test", func(_ string) (io.ReadWriteCloser, error) {
		return nil, want
	})
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected wrapped %v, got %v (l=%v)", want, err, l)
	}
	if l != nil {
		t.Fatal("expected nil listener on open failure")
	}
}

// TestSerialListener_NewRejectsNilOpener guards against accidental nil.
func TestSerialListener_NewRejectsNilOpener(t *testing.T) {
	if _, err := NewSerialListener("/dev/test", nil); err == nil {
		t.Fatal("expected error for nil openFn")
	}
}

// TestSerialListener_AcceptReadClose confirms basic read flow over
// a single accepted Conn.
func TestSerialListener_AcceptReadClose(t *testing.T) {
	opener := newFakeOpener("hello")
	l := newTestListener(t, "/dev/test", opener)
	defer func() { _ = l.Close() }()

	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	buf := make([]byte, 5)
	n, err := io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("read: %v (n=%d)", err, n)
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q", buf)
	}
}

// TestSerialListener_AcceptBlocksUntilFirstCloses verifies the
// session-gating: only one Conn at a time is "active" per the
// listener's internal state, even though they share the underlying
// device.
func TestSerialListener_AcceptBlocksUntilFirstCloses(t *testing.T) {
	opener := newFakeOpener("first")
	l := newTestListener(t, "/dev/test", opener)
	defer func() { _ = l.Close() }()

	conn1, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan acceptResult, 1)
	go func() {
		c, e := l.Accept()
		ch <- acceptResult{c, e}
	}()

	select {
	case r := <-ch:
		t.Fatalf("Accept returned without first close: conn=%v err=%v", r.conn, r.err)
	case <-time.After(50 * time.Millisecond):
	}

	_ = conn1.Close()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("second Accept after close: %v", r.err)
		}
		_ = r.conn.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("second Accept did not unblock after first conn closed")
	}
}

// TestSerialListener_ClosedRejects after explicit Close, Accept fails.
func TestSerialListener_ClosedRejects(t *testing.T) {
	opener := newFakeOpener("x")
	l := newTestListener(t, "/dev/test", opener)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := l.Accept()
	if err == nil {
		t.Fatal("expected error after Close")
	}
}

// TestSerialListener_CloseClosesDeviceOnce: listener Close closes the
// underlying device exactly once.
func TestSerialListener_CloseClosesDeviceOnce(t *testing.T) {
	fake := &closeCountingRWC{}
	l := newTestListener(t, "/dev/test", func(_ string) (io.ReadWriteCloser, error) {
		return fake, nil
	})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if got := fake.count(); got != 1 {
		t.Fatalf("close count = %d, want 1", got)
	}
	// Idempotent.
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if got := fake.count(); got != 1 {
		t.Fatalf("close count after second Close = %d, want 1", got)
	}
}

// TestSerialListener_ConnCloseDoesNotCloseDevice is the load-bearing
// invariant of the persistent-device model. Closing a Conn must not
// close the underlying device.
func TestSerialListener_ConnCloseDoesNotCloseDevice(t *testing.T) {
	fake := &closeCountingRWC{}
	l := newTestListener(t, "/dev/test", func(_ string) (io.ReadWriteCloser, error) {
		return fake, nil
	})
	defer func() { _ = l.Close() }()

	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("conn close: %v", err)
	}
	if got := fake.count(); got != 0 {
		t.Fatalf("device close count after conn.Close = %d, want 0", got)
	}
}

// TestSerialListener_RepeatedAcceptReusesSameDevice: multiple
// Accept/Close cycles share the same fd. The fd is opened once.
func TestSerialListener_RepeatedAcceptReusesSameDevice(t *testing.T) {
	opens := 0
	fake := &closeCountingRWC{}
	l := newTestListener(t, "/dev/test", func(_ string) (io.ReadWriteCloser, error) {
		opens++
		return fake, nil
	})
	defer func() { _ = l.Close() }()

	for i := 0; i < 5; i++ {
		conn, err := l.Accept()
		if err != nil {
			t.Fatalf("Accept %d: %v", i, err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
	}
	if opens != 1 {
		t.Fatalf("opens after 5 Accept/Close cycles = %d, want 1", opens)
	}
	if got := fake.count(); got != 0 {
		t.Fatalf("device close count after 5 conn.Close = %d, want 0", got)
	}
}

func TestSerialAddr(t *testing.T) {
	a := serialAddr("/dev/ttyV0.0")
	if a.Network() != "virtio-serial" {
		t.Fatal("network mismatch")
	}
	if a.String() != "/dev/ttyV0.0" {
		t.Fatal("string mismatch")
	}
}

func TestSerialConn_AddrAndClose(t *testing.T) {
	opener := newFakeOpener("y")
	l := newTestListener(t, "/dev/test", opener)
	defer func() { _ = l.Close() }()

	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if conn.LocalAddr().String() != "/dev/test" {
		t.Fatal("LocalAddr")
	}
	if conn.RemoteAddr().String() != "/dev/test" {
		t.Fatal("RemoteAddr")
	}
	// Double-close must be safe and idempotent.
	if err := conn.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close 2: %v", err)
	}
	// Listener can hand out a fresh Conn after the previous one ended.
	conn2, err := l.Accept()
	if err != nil {
		t.Fatalf("Accept after close: %v", err)
	}
	_ = conn2.Close()
}

func TestSerialConn_ReadSetsIdleDeadline(t *testing.T) {
	fake := &deadlineFakeRWC{}
	conn := &serialConn{
		rwc:         fake,
		idleTimeout: time.Second,
		nowFn:       time.Now,
	}

	buffer := make([]byte, 1)
	_, _ = conn.Read(buffer)
	if got := fake.deadlineCount(); got != 1 {
		t.Fatalf("read deadline count = %d, want 1", got)
	}
}

func TestSerialConn_ReadTimeoutReturnsEOF(t *testing.T) {
	fake := &timeoutFakeRWC{}
	conn := &serialConn{
		rwc:         fake,
		idleTimeout: time.Second,
		nowFn:       time.Now,
	}

	buffer := make([]byte, 1)
	n, err := conn.Read(buffer)
	if n != 0 {
		t.Fatalf("n = %d", n)
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want EOF", err)
	}
}
