package chunkedstream

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"testing"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// captureSend returns a sendFn that appends every chunk to chunks.
func captureSend(chunks *[]*mwanv1.Chunk) func(*mwanv1.Chunk) error {
	return func(c *mwanv1.Chunk) error {
		*chunks = append(*chunks, c)
		return nil
	}
}

// drainTo replays chunks into a fresh Writer backed by dst.
func drainTo(t *testing.T, dst io.Writer, chunks []*mwanv1.Chunk) *Writer {
	t.Helper()
	w := NewWriter(dst)
	for i, c := range chunks {
		if err := w.Write(c); err != nil {
			t.Fatalf("Write chunk %d: %v", i, err)
		}
	}
	return w
}

func TestSendThenWriter_HappyPath_RoundTripsBytes(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdef"), 10_000) // 60_000 bytes
	src := bytes.NewReader(payload)

	hdr := &mwanv1.ChunkHeader{
		ContentType: "application/octet-stream",
		Label:       "v1.2.3",
		TotalSize:   int64(len(payload)),
		Attrs:       map[string]string{"version_str": "v1.2.3"},
	}
	var chunks []*mwanv1.Chunk
	sumHex, total, err := Send(src, hdr, 0, captureSend(&chunks))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if total != int64(len(payload)) {
		t.Fatalf("total=%d want %d", total, len(payload))
	}
	want := sha256.Sum256(payload)
	if sumHex != hex.EncodeToString(want[:]) {
		t.Fatalf("sumHex mismatch: got %s want %s", sumHex, hex.EncodeToString(want[:]))
	}

	var dst bytes.Buffer
	w := drainTo(t, &dst, chunks)
	if !w.Done() {
		t.Fatal("Writer not Done after trailer")
	}
	if !w.ChecksumOK() {
		t.Fatal("ChecksumOK=false on happy path")
	}
	if w.BytesWritten() != int64(len(payload)) {
		t.Fatalf("BytesWritten=%d want %d", w.BytesWritten(), len(payload))
	}
	if !bytes.Equal(dst.Bytes(), payload) {
		t.Fatal("payload byte mismatch")
	}
	if w.Header().GetLabel() != "v1.2.3" {
		t.Fatalf("Header label=%q", w.Header().GetLabel())
	}
	if w.Header().GetAttrs()["version_str"] != "v1.2.3" {
		t.Fatalf("attr version_str=%q", w.Header().GetAttrs()["version_str"])
	}
	if w.Sha256Hex() != hex.EncodeToString(want[:]) {
		t.Fatalf("Writer.Sha256Hex mismatch: got %s", w.Sha256Hex())
	}
}

func TestSend_ChunkBoundaries(t *testing.T) {
	const chunkBytes = 1024
	cases := []struct {
		name string
		size int
	}{
		{"exact_one_chunk", chunkBytes},
		{"exact_multi_chunk", chunkBytes * 4},
		{"off_by_one_under", chunkBytes*3 - 1},
		{"off_by_one_over", chunkBytes*3 + 1},
		{"single_byte", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := make([]byte, tc.size)
			if _, err := rand.Read(payload); err != nil {
				t.Fatalf("rand: %v", err)
			}
			var chunks []*mwanv1.Chunk
			_, total, err := Send(bytes.NewReader(payload), nil, chunkBytes, captureSend(&chunks))
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if total != int64(tc.size) {
				t.Fatalf("total=%d want %d", total, tc.size)
			}
			var dst bytes.Buffer
			w := drainTo(t, &dst, chunks)
			if !w.Done() || !w.ChecksumOK() {
				t.Fatalf("done=%v ok=%v", w.Done(), w.ChecksumOK())
			}
			if !bytes.Equal(dst.Bytes(), payload) {
				t.Fatalf("payload mismatch size=%d", tc.size)
			}
		})
	}
}

func TestSend_EmptyPayload(t *testing.T) {
	var chunks []*mwanv1.Chunk
	sumHex, total, err := Send(bytes.NewReader(nil), nil, 0, captureSend(&chunks))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if total != 0 {
		t.Fatalf("total=%d want 0", total)
	}
	emptyHash := sha256.Sum256(nil)
	if sumHex != hex.EncodeToString(emptyHash[:]) {
		t.Fatalf("sum mismatch on empty: %s", sumHex)
	}
	if len(chunks) != 2 {
		t.Fatalf("len(chunks)=%d want 2", len(chunks))
	}
	if _, ok := chunks[0].GetBody().(*mwanv1.Chunk_Header); !ok {
		t.Fatal("first chunk not header")
	}
	if _, ok := chunks[1].GetBody().(*mwanv1.Chunk_Trailer); !ok {
		t.Fatal("second chunk not trailer")
	}
	var dst bytes.Buffer
	w := drainTo(t, &dst, chunks)
	if !w.Done() || !w.ChecksumOK() || w.BytesWritten() != 0 {
		t.Fatalf("empty: done=%v ok=%v wrote=%d", w.Done(), w.ChecksumOK(), w.BytesWritten())
	}
}

func TestWriter_HeaderOnly_NotDone(t *testing.T) {
	w := NewWriter(io.Discard)
	if err := w.Write(&mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{}}}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if w.Done() {
		t.Fatal("Done=true after header only")
	}
	if w.ChecksumOK() {
		t.Fatal("ChecksumOK should be false until trailer arrives")
	}
}

func TestWriter_TrailerImmediately_ZeroBytePayload(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	if err := w.Write(&mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{}}}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	emptyHash := sha256.Sum256(nil)
	trailer := &mwanv1.Chunk{Body: &mwanv1.Chunk_Trailer{Trailer: &mwanv1.ChunkTrailer{
		Sha256Hex: hex.EncodeToString(emptyHash[:]),
		TotalSize: 0,
	}}}
	if err := w.Write(trailer); err != nil {
		t.Fatalf("write trailer: %v", err)
	}
	if !w.Done() || !w.ChecksumOK() || w.BytesWritten() != 0 || dst.Len() != 0 {
		t.Fatalf("zero payload: done=%v ok=%v wrote=%d dst=%d",
			w.Done(), w.ChecksumOK(), w.BytesWritten(), dst.Len())
	}
}

func TestWriter_TrailerSHAMismatch_ChecksumNotOK(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	if err := w.Write(&mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{}}}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	payload := []byte("hello world")
	if err := w.Write(&mwanv1.Chunk{Body: &mwanv1.Chunk_Data{Data: payload}}); err != nil {
		t.Fatalf("write data: %v", err)
	}
	bogus := &mwanv1.Chunk{Body: &mwanv1.Chunk_Trailer{Trailer: &mwanv1.ChunkTrailer{
		Sha256Hex: "00000000",
		TotalSize: int64(len(payload)),
	}}}
	if err := w.Write(bogus); err != nil {
		t.Fatalf("write bogus trailer: %v", err)
	}
	if !w.Done() {
		t.Fatal("Done should be true after trailer regardless of mismatch")
	}
	if w.ChecksumOK() {
		t.Fatal("ChecksumOK should be false on mismatch")
	}
	if !bytes.Equal(dst.Bytes(), payload) {
		t.Fatal("data should still be accumulated even on bad trailer")
	}
	if w.TrailerSha256Hex() != "00000000" {
		t.Fatalf("TrailerSha256Hex=%q want 00000000", w.TrailerSha256Hex())
	}
}

func TestWriter_DoubleHeader_Rejected(t *testing.T) {
	w := NewWriter(io.Discard)
	hdr := &mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{}}}
	if err := w.Write(hdr); err != nil {
		t.Fatalf("first header: %v", err)
	}
	if err := w.Write(hdr); !errors.Is(err, ErrProtocol) {
		t.Fatalf("second header: got %v want ErrProtocol", err)
	}
}

func TestWriter_DataAfterTrailer_Rejected(t *testing.T) {
	w := NewWriter(io.Discard)
	if err := w.Write(&mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: &mwanv1.ChunkHeader{}}}); err != nil {
		t.Fatalf("header: %v", err)
	}
	emptyHash := sha256.Sum256(nil)
	trailer := &mwanv1.Chunk{Body: &mwanv1.Chunk_Trailer{Trailer: &mwanv1.ChunkTrailer{
		Sha256Hex: hex.EncodeToString(emptyHash[:]),
	}}}
	if err := w.Write(trailer); err != nil {
		t.Fatalf("trailer: %v", err)
	}
	stray := &mwanv1.Chunk{Body: &mwanv1.Chunk_Data{Data: []byte("late")}}
	if err := w.Write(stray); !errors.Is(err, ErrProtocol) {
		t.Fatalf("late data: got %v want ErrProtocol", err)
	}
}

func TestWriter_DataBeforeHeader_Rejected(t *testing.T) {
	w := NewWriter(io.Discard)
	stray := &mwanv1.Chunk{Body: &mwanv1.Chunk_Data{Data: []byte("early")}}
	if err := w.Write(stray); !errors.Is(err, ErrProtocol) {
		t.Fatalf("early data: got %v want ErrProtocol", err)
	}
}

func TestWriter_TrailerBeforeHeader_Rejected(t *testing.T) {
	w := NewWriter(io.Discard)
	tr := &mwanv1.Chunk{Body: &mwanv1.Chunk_Trailer{Trailer: &mwanv1.ChunkTrailer{}}}
	if err := w.Write(tr); !errors.Is(err, ErrProtocol) {
		t.Fatalf("early trailer: got %v want ErrProtocol", err)
	}
}

func TestWriter_NilChunk_Rejected(t *testing.T) {
	w := NewWriter(io.Discard)
	if err := w.Write(nil); !errors.Is(err, ErrProtocol) {
		t.Fatalf("nil chunk: got %v", err)
	}
}

func TestSend_LargeChunkBytes(t *testing.T) {
	payload := bytes.Repeat([]byte{0x42}, 200_000)
	var chunks []*mwanv1.Chunk
	if _, _, err := Send(bytes.NewReader(payload), nil, 65536, captureSend(&chunks)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var dst bytes.Buffer
	w := drainTo(t, &dst, chunks)
	if !w.Done() || !w.ChecksumOK() || !bytes.Equal(dst.Bytes(), payload) {
		t.Fatalf("large chunk path failed: done=%v ok=%v eq=%v",
			w.Done(), w.ChecksumOK(), bytes.Equal(dst.Bytes(), payload))
	}
}

func TestSend_TinyChunkBytes(t *testing.T) {
	payload := []byte("abcde")
	var chunks []*mwanv1.Chunk
	if _, _, err := Send(bytes.NewReader(payload), nil, 1, captureSend(&chunks)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	dataCount := 0
	for _, c := range chunks {
		if _, ok := c.GetBody().(*mwanv1.Chunk_Data); ok {
			dataCount++
		}
	}
	if dataCount != len(payload) {
		t.Fatalf("dataCount=%d want %d", dataCount, len(payload))
	}
	var dst bytes.Buffer
	_ = drainTo(t, &dst, chunks)
	if !bytes.Equal(dst.Bytes(), payload) {
		t.Fatal("tiny-chunk path mismatch")
	}
}

// errReader returns errBoom after returning some bytes.
type errReader struct {
	bytesLeft int
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.bytesLeft <= 0 {
		return 0, errBoom
	}
	n := len(p)
	if n > e.bytesLeft {
		n = e.bytesLeft
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	e.bytesLeft -= n
	return n, nil
}

var errBoom = errors.New("boom")

func TestSend_ReaderError_Propagated(t *testing.T) {
	r := &errReader{bytesLeft: 100}
	var chunks []*mwanv1.Chunk
	_, _, err := Send(r, nil, 32, captureSend(&chunks))
	if err == nil {
		t.Fatal("expected error from Send")
	}
	if !errors.Is(err, errBoom) {
		t.Fatalf("err=%v want errBoom wrapped", err)
	}
	for _, c := range chunks {
		if _, ok := c.GetBody().(*mwanv1.Chunk_Trailer); ok {
			t.Fatal("trailer should not be emitted after read error")
		}
	}
}

func TestSend_SendFnError_Propagated(t *testing.T) {
	failOn := 1
	calls := 0
	sendFn := func(_ *mwanv1.Chunk) error {
		defer func() { calls++ }()
		if calls == failOn {
			return errBoom
		}
		return nil
	}
	_, _, err := Send(bytes.NewReader([]byte("payload")), nil, 0, sendFn)
	if !errors.Is(err, errBoom) {
		t.Fatalf("err=%v want errBoom", err)
	}
}

func TestSend_NilArgs(t *testing.T) {
	if _, _, err := Send(nil, nil, 0, func(*mwanv1.Chunk) error { return nil }); err == nil {
		t.Fatal("expected error on nil reader")
	}
	if _, _, err := Send(bytes.NewReader(nil), nil, 0, nil); err == nil {
		t.Fatal("expected error on nil sendFn")
	}
}

func TestPool_ReusedAcrossSends(t *testing.T) {
	for i := 0; i < 100; i++ {
		payload := []byte(fmt.Sprintf("iter=%d", i))
		var chunks []*mwanv1.Chunk
		if _, _, err := Send(bytes.NewReader(payload), nil, 0, captureSend(&chunks)); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
}
