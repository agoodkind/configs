// Package mwn1 implements the MWN1 length-prefixed framing protocol
// used to carry mwan-opnsense RPC traffic over a Proxmox virtio-serial
// chardev. It is the replacement for gRPC over HTTP/2 over the same
// transport, which composes badly with virtio-serial's stream
// semantics.
//
// On-wire frame layout (big-endian, no padding):
//
//	offset  size  field
//	0       4     magic = "MWN1"
//	4       1     flags
//	5       2     method_id     (uint16)
//	7       8     corr_id       (uint64)
//	15      4     payload_len   (uint32, max 65535)
//	19      N     payload       (proto message bytes)
//	19+N    4     crc32c castagnoli over magic..payload
//
// Total fixed overhead is 23 bytes. Larger RPC payloads use multiple
// frames sharing one corr_id, terminated by FlagFinal.
package mwn1

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
)

// Wire constants. All offsets and sizes are in bytes.
const (
	// Magic is the four-byte frame preamble. Receivers scan for this
	// pattern byte-by-byte to recover from mid-stream corruption.
	Magic = "MWN1"

	// MagicBytes is the on-wire length of the magic preamble.
	MagicBytes = 4

	// HdrAfterMagic is the size of the fixed header that follows the
	// magic: flags(1) + method_id(2) + corr_id(8) + payload_len(4).
	HdrAfterMagic = 1 + 2 + 8 + 4

	// TailLen is the size of the trailing CRC32C field.
	TailLen = 4

	// MaxPayload is the per-frame payload byte cap. Payloads larger
	// than this must be split across multiple frames sharing one
	// corr_id (FlagStreaming) and terminated by FlagFinal.
	MaxPayload = 65535

	// FrameOverhead is the total non-payload byte cost of one frame.
	FrameOverhead = MagicBytes + HdrAfterMagic + TailLen
)

// Flags is the bitfield carried in the flags byte of every frame.
type Flags uint8

// Flag bits. Bits 4-7 are reserved and must be transmitted as zero.
const (
	// FlagRequest marks a frame as a request from client to server.
	// Cleared frames are responses.
	FlagRequest Flags = 1 << 0

	// FlagStreaming marks a frame as part of a multi-frame stream
	// sharing one corr_id. Single-frame requests and responses do
	// not set this bit.
	FlagStreaming Flags = 1 << 1

	// FlagFinal marks the last frame of a corr_id. A single-frame
	// request sets FlagRequest|FlagFinal; a streaming request sets
	// FlagFinal only on its terminal frame.
	FlagFinal Flags = 1 << 2

	// FlagError marks a response whose payload is a serialized
	// error status rather than the declared response message.
	FlagError Flags = 1 << 3
)

// Frame is one decoded MWN1 message. Concurrent reads or writes of
// the same Frame value are not supported.
type Frame struct {
	Flags    Flags
	MethodID uint16
	CorrID   uint64
	Payload  []byte
}

// castagnoli is the CRC32C Castagnoli table; package-level so we do
// not allocate a fresh table per frame.
var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// ErrCorrupted indicates that a frame's CRC32C did not match its
// header and payload bytes. The reader has already consumed the
// frame from the stream; resync resumes on the next byte.
var ErrCorrupted = errors.New("mwn1: frame CRC mismatch")

// ErrPayloadTooLarge indicates that a payload exceeded MaxPayload.
// Returned by WriteFrame for outbound frames and by ReadFrame when
// the on-wire payload_len header advertises more than MaxPayload.
var ErrPayloadTooLarge = errors.New("mwn1: payload exceeds MaxPayload")

// ReadFrame scans r for the magic preamble, then reads the rest of
// the frame and verifies its CRC32C. On magic mismatch it slides
// one byte forward and tries again, logging at WARN with the count
// of dropped bytes once it locks on. Returns ErrCorrupted on CRC
// failure, ErrPayloadTooLarge when the advertised payload_len
// exceeds MaxPayload, or the wrapped underlying read error when
// the stream is closed mid-frame.
func ReadFrame(r io.Reader, log *slog.Logger) (Frame, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := scanMagic(r, log); err != nil {
		return Frame{}, err
	}
	hdr := make([]byte, HdrAfterMagic)
	if _, err := io.ReadFull(r, hdr); err != nil {
		log.Warn("mwn1: read header", slog.String("err", err.Error()))
		return Frame{}, fmt.Errorf("mwn1: read header: %w", err)
	}
	flags := Flags(hdr[0])
	methodID := binary.BigEndian.Uint16(hdr[1:3])
	corrID := binary.BigEndian.Uint64(hdr[3:11])
	payloadLen := binary.BigEndian.Uint32(hdr[11:15])
	if payloadLen > MaxPayload {
		log.Warn("mwn1: payload header exceeds max",
			slog.Uint64("advertised", uint64(payloadLen)),
			slog.Uint64("max", uint64(MaxPayload)))
		return Frame{}, fmt.Errorf("%w: advertised %d", ErrPayloadTooLarge, payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		log.Warn("mwn1: read payload", slog.String("err", err.Error()))
		return Frame{}, fmt.Errorf("mwn1: read payload: %w", err)
	}
	var crcBuf [TailLen]byte
	if _, err := io.ReadFull(r, crcBuf[:]); err != nil {
		log.Warn("mwn1: read crc", slog.String("err", err.Error()))
		return Frame{}, fmt.Errorf("mwn1: read crc: %w", err)
	}
	got := binary.BigEndian.Uint32(crcBuf[:])
	want := computeCRC(hdr, payload)
	if got != want {
		log.Warn("mwn1: crc mismatch",
			slog.String("got", fmt.Sprintf("%08x", got)),
			slog.String("want", fmt.Sprintf("%08x", want)))
		return Frame{}, fmt.Errorf("%w: got=%08x want=%08x", ErrCorrupted, got, want)
	}
	return Frame{
		Flags:    flags,
		MethodID: methodID,
		CorrID:   corrID,
		Payload:  payload,
	}, nil
}

// WriteFrame serializes f and writes it to w in a single Write call.
// Caller is responsible for serializing concurrent writes; Frame
// itself does not lock. Returns ErrPayloadTooLarge if the payload
// exceeds MaxPayload.
//
// log is used to emit a single WARN line on payload-size or transport
// write failure so the wrapped error returned to the caller has a
// matching observable log entry. nil is allowed and falls back to
// [slog.Default].
func WriteFrame(w io.Writer, f Frame, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if len(f.Payload) > MaxPayload {
		log.Warn("mwn1: write payload exceeds max",
			slog.Int("have", len(f.Payload)),
			slog.Int("max", MaxPayload))
		return fmt.Errorf("%w: have %d", ErrPayloadTooLarge, len(f.Payload))
	}
	hdr := make([]byte, HdrAfterMagic)
	hdr[0] = byte(f.Flags)
	binary.BigEndian.PutUint16(hdr[1:3], f.MethodID)
	binary.BigEndian.PutUint64(hdr[3:11], f.CorrID)
	binary.BigEndian.PutUint32(hdr[11:15], payloadLenU32(len(f.Payload)))
	crc := computeCRC(hdr, f.Payload)
	buf := make([]byte, 0, FrameOverhead+len(f.Payload))
	buf = append(buf, Magic...)
	buf = append(buf, hdr...)
	buf = append(buf, f.Payload...)
	var crcBuf [TailLen]byte
	binary.BigEndian.PutUint32(crcBuf[:], crc)
	buf = append(buf, crcBuf[:]...)
	if _, err := w.Write(buf); err != nil {
		log.Warn("mwn1: write frame", slog.String("err", err.Error()))
		return fmt.Errorf("mwn1: write frame: %w", err)
	}
	return nil
}

// payloadLenU32 narrows an int payload length to uint32 after asserting
// it fits within MaxPayload. WriteFrame already returns an error when
// the payload exceeds MaxPayload, so by the time this runs the value
// is guaranteed in range. Centralizing the conversion keeps gosec G115
// happy without scattering inline annotations.
func payloadLenU32(n int) uint32 {
	if n < 0 || n > MaxPayload {
		return MaxPayload
	}
	return uint32(n)
}

// computeCRC runs Castagnoli CRC32C over magic||hdr||payload. Kept
// in one place so reader and writer stay byte-for-byte identical.
func computeCRC(hdr, payload []byte) uint32 {
	hasher := crc32.New(castagnoli)
	_, _ = hasher.Write([]byte(Magic))
	_, _ = hasher.Write(hdr)
	_, _ = hasher.Write(payload)
	return hasher.Sum32()
}

// scanMagic advances r byte-by-byte until it locks on the four-byte
// Magic preamble. On success, the magic has been consumed from r and
// the next read will return the start of the header. If the lock
// took more than 4 bytes (i.e. junk preceded a valid frame), it logs
// the dropped count at WARN.
func scanMagic(r io.Reader, log *slog.Logger) error {
	const m0, m1, m2, m3 = 'M', 'W', 'N', '1'
	var b [1]byte
	state := 0
	scanned := 0
	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			log.Warn("mwn1: scan magic", slog.String("err", err.Error()))
			return fmt.Errorf("mwn1: scan magic: %w", err)
		}
		scanned++
		switch state {
		case 0:
			if b[0] == m0 {
				state = 1
			}
		case 1:
			if b[0] == m1 {
				state = 2
				continue
			}
			state = 0
			if b[0] == m0 {
				state = 1
			}
		case 2:
			if b[0] == m2 {
				state = 3
				continue
			}
			state = 0
			if b[0] == m0 {
				state = 1
			}
		case 3:
			if b[0] == m3 {
				if scanned > MagicBytes {
					log.Warn("mwn1: resynced on magic",
						slog.Int("dropped_bytes", scanned-MagicBytes))
				}
				return nil
			}
			state = 0
			if b[0] == m0 {
				state = 1
			}
		}
	}
}
