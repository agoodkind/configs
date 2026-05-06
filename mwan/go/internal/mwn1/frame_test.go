package mwn1

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// newTestLogger returns a slog.Logger writing to b. Tests assert on b
// to verify resync log output.
func newTestLogger(b *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// makeFrame is a small helper that round-trips through writeFrame so
// tests do not have to hand-build wire bytes.
func makeFrame(t *testing.T, f frame) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := writeFrame(&buf, f, slog.Default()); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	return buf.Bytes()
}

func TestWriteRead_RoundTrip(t *testing.T) {
	want := frame{
		Flags:    FlagRequest | FlagFinal,
		MethodID: 7,
		CorrID:   0xdeadbeefcafebabe,
		Payload:  []byte("hello world"),
	}
	wire := makeFrame(t, want)
	got, err := readFrame(bytes.NewReader(wire), slog.Default())
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got.Flags != want.Flags || got.MethodID != want.MethodID ||
		got.CorrID != want.CorrID || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", got, want)
	}
}

func TestFlags_AllBitsRoundTrip(t *testing.T) {
	cases := []Flags{
		0,
		FlagRequest,
		FlagResponse,
		FlagStreaming,
		FlagFinal,
		FlagError,
		FlagFragment,
		FlagCancel,
		FlagAck,
		FlagRequest | FlagStreaming,
		FlagRequest | FlagFinal,
		FlagRequest | FlagStreaming | FlagFinal,
		FlagResponse | FlagError | FlagFinal,
		FlagRequest | FlagStreaming | FlagFragment,
		FlagRequest | FlagStreaming | FlagCancel,
		FlagRequest | FlagStreaming | FlagFinal | FlagError | FlagFragment,
	}
	for _, fl := range cases {
		f := frame{Flags: fl, MethodID: 1, CorrID: 1, Payload: []byte{0x01}}
		got, err := readFrame(bytes.NewReader(makeFrame(t, f)), slog.Default())
		if err != nil {
			t.Fatalf("flags=%08b: %v", fl, err)
		}
		if got.Flags != fl {
			t.Fatalf("flags=%08b: got %08b", fl, got.Flags)
		}
	}
}

func TestEmptyPayload(t *testing.T) {
	f := frame{Flags: FlagRequest | FlagFinal, MethodID: 2, CorrID: 5, Payload: nil}
	got, err := readFrame(bytes.NewReader(makeFrame(t, f)), slog.Default())
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if len(got.Payload) != 0 {
		t.Fatalf("want empty payload, got %d bytes", len(got.Payload))
	}
}

func TestMaxPayload(t *testing.T) {
	payload := bytes.Repeat([]byte{0xa5}, MaxPayload)
	f := frame{Flags: FlagRequest | FlagFinal, MethodID: 3, CorrID: 9, Payload: payload}
	wire := makeFrame(t, f)
	got, err := readFrame(bytes.NewReader(wire), slog.Default())
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestPayloadTooLarge_Write(t *testing.T) {
	payload := bytes.Repeat([]byte{0}, MaxPayload+1)
	f := frame{Flags: FlagRequest, MethodID: 1, CorrID: 1, Payload: payload}
	err := writeFrame(io.Discard, f, slog.Default())
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("want ErrPayloadTooLarge, got %v", err)
	}
}

func TestCRC_Corruption(t *testing.T) {
	f := frame{Flags: FlagRequest, MethodID: 1, CorrID: 1, Payload: []byte("payload-bytes")}
	wire := makeFrame(t, f)
	// Flip a payload bit: payload starts at MagicBytes+HdrAfterMagic = 19.
	wire[19] ^= 0x01
	_, err := readFrame(bytes.NewReader(wire), slog.Default())
	if !errors.Is(err, ErrCorrupted) {
		t.Fatalf("want ErrCorrupted, got %v", err)
	}
}

func TestMagicScan_PrefixGarbage(t *testing.T) {
	cases := []int{0, 1, 4, 100, 65000}
	for _, n := range cases {
		f := frame{Flags: FlagFinal, MethodID: 1, CorrID: 42, Payload: []byte("ok")}
		valid := makeFrame(t, f)
		junk := bytes.Repeat([]byte{'X'}, n)
		buf := append(append([]byte{}, junk...), valid...)
		var logBuf bytes.Buffer
		got, err := readFrame(bytes.NewReader(buf), newTestLogger(&logBuf))
		if err != nil {
			t.Fatalf("prefix=%d: %v", n, err)
		}
		if got.CorrID != 42 || !bytes.Equal(got.Payload, []byte("ok")) {
			t.Fatalf("prefix=%d: decode mismatch", n)
		}
		if n > 0 {
			if !strings.Contains(logBuf.String(), "resynced on magic") {
				t.Fatalf("prefix=%d: expected resync log, got %q", n, logBuf.String())
			}
		} else {
			if strings.Contains(logBuf.String(), "resynced on magic") {
				t.Fatalf("prefix=%d: did not expect resync log", n)
			}
		}
	}
}

func TestMagicScan_PartialMagicPrefix(t *testing.T) {
	// "MWN1" followed by junk should look like a frame start, fail
	// header decode (junk byte is treated as flags etc.), but the
	// next readFrame should still find the real frame after.
	//
	// Test variant: prefix "MWN" + 'X' (partial magic, then non-1)
	// followed by a valid frame. The state machine must reset and
	// keep scanning, eventually locking on the real magic.
	f := frame{Flags: FlagFinal, MethodID: 1, CorrID: 7, Payload: []byte("z")}
	valid := makeFrame(t, f)
	prefix := []byte("MWNXMWN")
	buf := append(append([]byte{}, prefix...), valid...)
	var logBuf bytes.Buffer
	got, err := readFrame(bytes.NewReader(buf), newTestLogger(&logBuf))
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got.CorrID != 7 {
		t.Fatalf("decode mismatch: %+v", got)
	}
	if !strings.Contains(logBuf.String(), "resynced on magic") {
		t.Fatalf("expected resync log, got %q", logBuf.String())
	}
}

func TestMagicScan_DropCountAccurate(t *testing.T) {
	f := frame{Flags: FlagFinal, MethodID: 1, CorrID: 1, Payload: []byte("p")}
	valid := makeFrame(t, f)
	junk := bytes.Repeat([]byte{'Q'}, 17)
	buf := append(append([]byte{}, junk...), valid...)
	var logBuf bytes.Buffer
	if _, err := readFrame(bytes.NewReader(buf), newTestLogger(&logBuf)); err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if !strings.Contains(logBuf.String(), "dropped_bytes=17") {
		t.Fatalf("want dropped_bytes=17 in log, got %q", logBuf.String())
	}
}

func TestEOF_MidFrame(t *testing.T) {
	// Truncate after magic but before full header.
	f := frame{Flags: FlagRequest, MethodID: 1, CorrID: 1, Payload: []byte("xyz")}
	wire := makeFrame(t, f)
	truncated := wire[:MagicBytes+5] // magic + 5 header bytes
	_, err := readFrame(bytes.NewReader(truncated), slog.Default())
	if err == nil {
		t.Fatalf("expected error on truncated frame")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF-class error, got %v", err)
	}
}

func TestEOF_BeforeMagic(t *testing.T) {
	_, err := readFrame(bytes.NewReader(nil), slog.Default())
	if err == nil {
		t.Fatalf("expected error on empty stream")
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestPayloadTooLarge_Read(t *testing.T) {
	// Hand-craft a frame whose advertised payload_len exceeds MaxPayload.
	// We do not need a valid CRC; the size check happens first.
	var buf bytes.Buffer
	buf.WriteString(Magic)
	buf.WriteByte(0)              // flags
	buf.Write([]byte{0x00, 0x01}) // method
	buf.Write(make([]byte, 8))    // corr_id
	payloadLen := uint32(MaxPayload + 1)
	buf.Write([]byte{
		byte(payloadLen >> 24),
		byte(payloadLen >> 16),
		byte(payloadLen >> 8),
		byte(payloadLen),
	})
	_, err := readFrame(bytes.NewReader(buf.Bytes()), slog.Default())
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("want ErrPayloadTooLarge, got %v", err)
	}
}
