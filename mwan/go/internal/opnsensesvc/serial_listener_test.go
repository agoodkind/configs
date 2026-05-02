package opnsensesvc

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
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

func TestSerialListener_AcceptReadClose(t *testing.T) {
	opener := newFakeOpener("hello")
	l := NewSerialListener("/dev/test", opener)
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

func TestSerialListener_RejectsConcurrentAccept(t *testing.T) {
	opener := newFakeOpener("first")
	l := NewSerialListener("/dev/test", opener)
	defer func() { _ = l.Close() }()

	conn1, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn1.Close() }()

	_, err = l.Accept()
	if err == nil {
		t.Fatal("expected error on second concurrent Accept")
	}
	if !strings.Contains(err.Error(), "previous connection still open") {
		t.Fatalf("got %v", err)
	}
}

func TestSerialListener_ClosedRejects(t *testing.T) {
	opener := newFakeOpener("x")
	l := NewSerialListener("/dev/test", opener)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := l.Accept()
	if err == nil {
		t.Fatal("expected error after Close")
	}
}

func TestSerialListener_OpenerError(t *testing.T) {
	want := errors.New("device gone")
	l := NewSerialListener("/dev/test", func(_ string) (io.ReadWriteCloser, error) {
		return nil, want
	})
	defer func() { _ = l.Close() }()

	_, err := l.Accept()
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected wrapped %v, got %v", want, err)
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
	l := NewSerialListener("/dev/test", opener)
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
	// Calling Close twice must be safe (double-close should not panic
	// or change error).
	if err := conn.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close 2: %v", err)
	}
	// Once that conn is closed, the listener should accept a new one.
	conn2, err := l.Accept()
	if err != nil {
		t.Fatalf("Accept after close: %v", err)
	}
	_ = conn2.Close()
}
