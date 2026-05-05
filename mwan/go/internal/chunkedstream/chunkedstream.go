// Package chunkedstream wraps the streaming Chunk envelope defined in
// proto/mwan/v1/chunked.proto so that callers do not have to reinvent
// header/data/trailer ordering, sha256 checksumming, or buffer reuse
// for every RPC that needs to carry a payload larger than gRPC's
// per-message frame budget.
//
// The package is intentionally gRPC-free. The caller wires Send into
// stream.Send and feeds Writer.Write from stream.Recv on the receiver
// side. Concurrent Sends or Writes on the same instance are not
// supported; create one Reader/Writer per stream.
package chunkedstream

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// DefaultChunkBytes is the per-data-chunk byte budget used when the
// caller passes 0 to Send. 64 KiB sits comfortably above gRPC's
// default 16 KiB SETTINGS_MAX_FRAME_SIZE while still exercising
// streaming behavior on the wire.
const DefaultChunkBytes = 64 * 1024

// ErrProtocol marks a failure caused by malformed envelope ordering
// (e.g. data after trailer, double header). Wrap with [errors.Is] to
// detect.
var ErrProtocol = errors.New("chunkedstream: protocol error")

// hasher is the minimal subset of [hash.Hash] used by Writer; declared
// inline so swap-in test hashers do not need to satisfy the entire
// stdlib [hash.Hash] surface.
type hasher interface {
	io.Writer
	Sum(b []byte) []byte
}

// Writer accumulates a chunked stream into an [io.Writer] while
// running a sha256 of the data bytes. It is single-goroutine: do not
// invoke Write concurrently.
type Writer struct {
	dst         io.Writer
	hash        hasher
	header      *mwanv1.ChunkHeader
	bytesIn     int64
	headerSeen  bool
	done        bool
	checksumOK  bool
	computedHex string
	trailerHex  string
}

// NewWriter returns a Writer that copies data chunks into dst.
func NewWriter(dst io.Writer) *Writer {
	return &Writer{
		dst:         dst,
		hash:        sha256.New(),
		header:      nil,
		bytesIn:     0,
		headerSeen:  false,
		done:        false,
		checksumOK:  false,
		computedHex: "",
		trailerHex:  "",
	}
}

// Write consumes one [mwanv1.Chunk] from the stream. It enforces
// the header/data/trailer ordering and returns [ErrProtocol] on
// violations. Data chunks are written to the destination and folded
// into the running sha256.
func (w *Writer) Write(c *mwanv1.Chunk) error {
	if c == nil {
		return errProtocolf("nil chunk")
	}
	if w.done {
		return errProtocolf("chunk after trailer")
	}
	switch body := c.GetBody().(type) {
	case *mwanv1.Chunk_Header:
		if w.headerSeen {
			return errProtocolf("duplicate header")
		}
		w.headerSeen = true
		w.header = body.Header
		return nil
	case *mwanv1.Chunk_Data:
		if !w.headerSeen {
			return errProtocolf("data before header")
		}
		return w.writeData(body.Data)
	case *mwanv1.Chunk_Trailer:
		if !w.headerSeen {
			return errProtocolf("trailer before header")
		}
		w.done = true
		want := body.Trailer.GetSha256Hex()
		got := hex.EncodeToString(w.hash.Sum(nil))
		w.computedHex = got
		w.trailerHex = want
		w.checksumOK = strings.EqualFold(want, got)
		return nil
	default:
		return errProtocolf("unknown chunk body")
	}
}

// writeData copies one data chunk to dst and folds it into the
// running sha256. Errors are logged before being wrapped so the
// staticcheck-extra slog rule is satisfied.
func (w *Writer) writeData(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if _, err := w.dst.Write(data); err != nil {
		slog.Error("chunkedstream: write dst failed", "err", err, "bytes", len(data))
		return fmt.Errorf("chunkedstream: write dst: %w", err)
	}
	if _, err := w.hash.Write(data); err != nil {
		slog.Error("chunkedstream: hash data failed", "err", err, "bytes", len(data))
		return fmt.Errorf("chunkedstream: hash data: %w", err)
	}
	w.bytesIn += int64(len(data))
	return nil
}

// errProtocolf returns an [ErrProtocol]-wrapping error annotated with
// the given message and slogs at warn level so the staticcheck-extra
// "no wrapped error without slog" rule is satisfied.
func errProtocolf(reason string) error {
	wrapped := fmt.Errorf("%w: %s", ErrProtocol, reason)
	slog.Warn("chunkedstream: protocol violation", "err", wrapped, "reason", reason)
	return wrapped
}

// Header returns the header observed at the start of the stream, or
// nil if no header has been received yet.
func (w *Writer) Header() *mwanv1.ChunkHeader { return w.header }

// BytesWritten returns the total number of data bytes accepted so far.
func (w *Writer) BytesWritten() int64 { return w.bytesIn }

// Done reports whether the stream has been terminated by a trailer.
func (w *Writer) Done() bool { return w.done }

// ChecksumOK reports whether the trailer's sha256 matched the running
// digest of the data bytes. Only meaningful when [Writer.Done] is true.
func (w *Writer) ChecksumOK() bool { return w.checksumOK }

// Sha256Hex returns the hex-encoded sha256 of the data bytes computed
// during streaming. Equal to the trailer's value when ChecksumOK is
// true; equal to the locally computed digest otherwise. Empty string
// when Done is false.
func (w *Writer) Sha256Hex() string { return w.computedHex }

// TrailerSha256Hex returns the sha256 hex carried in the trailer, or
// empty string when no trailer has been received. Useful for logging
// the mismatch on a checksum failure.
func (w *Writer) TrailerSha256Hex() string { return w.trailerHex }

// Send streams r into chunks and dispatches header, data, trailer via
// sendFn. chunkBytes <= 0 selects [DefaultChunkBytes]. The header
// argument may be nil; in that case an empty header chunk is sent.
// The returned sha256 hex string and total byte count match what the
// receiver should compute.
func Send(
	r io.Reader,
	header *mwanv1.ChunkHeader,
	chunkBytes int,
	sendFn func(*mwanv1.Chunk) error,
) (string, int64, error) {
	if r == nil {
		return "", 0, errors.New("chunkedstream: nil reader")
	}
	if sendFn == nil {
		return "", 0, errors.New("chunkedstream: nil sendFn")
	}
	if chunkBytes <= 0 {
		chunkBytes = DefaultChunkBytes
	}

	headerMsg := header
	if headerMsg == nil {
		headerMsg = &mwanv1.ChunkHeader{
			ContentType: "",
			Label:       "",
			TotalSize:   0,
			Attrs:       nil,
		}
	}
	if err := sendFn(&mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: headerMsg}}); err != nil {
		slog.Error("chunkedstream: send header failed", "err", err)
		return "", 0, fmt.Errorf("chunkedstream: send header: %w", err)
	}

	buf := make([]byte, chunkBytes)

	hash := sha256.New()
	var totalBytes int64

	for {
		n, readErr := io.ReadFull(r, buf)
		if n > 0 {
			data := buf[:n]
			if _, err := hash.Write(data); err != nil {
				slog.Error("chunkedstream: hash data failed", "err", err, "bytes", n)
				return "", 0, fmt.Errorf("chunkedstream: hash data: %w", err)
			}
			payload := make([]byte, n)
			copy(payload, data)
			chunk := &mwanv1.Chunk{Body: &mwanv1.Chunk_Data{Data: payload}}
			if err := sendFn(chunk); err != nil {
				slog.Error("chunkedstream: send data failed", "err", err, "bytes", n)
				return "", 0, fmt.Errorf("chunkedstream: send data: %w", err)
			}
			totalBytes += int64(n)
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
		if readErr != nil {
			slog.Error("chunkedstream: read source failed", "err", readErr, "bytes_so_far", totalBytes)
			return "", 0, fmt.Errorf("chunkedstream: read source: %w", readErr)
		}
	}

	sumHex := hex.EncodeToString(hash.Sum(nil))
	trailer := &mwanv1.Chunk{Body: &mwanv1.Chunk_Trailer{Trailer: &mwanv1.ChunkTrailer{
		Sha256Hex: sumHex,
		TotalSize: totalBytes,
	}}}
	if err := sendFn(trailer); err != nil {
		slog.Error("chunkedstream: send trailer failed", "err", err, "total_bytes", totalBytes)
		return "", 0, fmt.Errorf("chunkedstream: send trailer: %w", err)
	}
	return sumHex, totalBytes, nil
}
