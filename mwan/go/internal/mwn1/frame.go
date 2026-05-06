// Package mwn1 implements the MWN1 message transport used to carry
// mwan-opnsense RPC traffic over Proxmox virtio-serial chardevs.
//
// On-wire frame layout (big-endian, no padding):
//
//	offset  size  field
//	0       4     magic = "MWN1"
//	4       1     flags
//	5       2     method_id     (uint16)
//	7       8     corr_id       (uint64)
//	15      4     payload_len   (uint32, capped at MaxPayload)
//	19      N     payload       (proto message bytes or fragment)
//	19+N    4     crc32c castagnoli over magic..payload
//
// Total fixed overhead is 23 bytes. Frames are an implementation
// detail; Conn exposes complete messages and transparently fragments
// payloads larger than MaxPayload.
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

	// MaxPayload is the per-frame payload byte cap. Payloads larger than
	// this are split across frames sharing one corr_id.
	MaxPayload = 64 * 1024

	// FrameOverhead is the total non-payload byte cost of one frame.
	FrameOverhead = MagicBytes + HdrAfterMagic + TailLen
)

// Flags is the bitfield carried in the flags byte of every frame.
type Flags uint8

// Flag bits.
const (
	// FlagRequest marks a request message from client to server.
	FlagRequest Flags = 1 << 0

	// FlagStreaming marks a streamed logical message sequence.
	FlagStreaming Flags = 1 << 1

	// FlagResponse marks a response message from server to client.
	FlagResponse Flags = 1 << 2

	// FlagFinal marks the final frame of one complete message.
	FlagFinal Flags = 1 << 3

	// FlagError marks a response whose payload is an error status rather
	// than the declared response message.
	FlagError Flags = 1 << 4

	// FlagFragment marks a non-final transport fragment.
	FlagFragment Flags = 1 << 5

	// FlagCancel marks a best-effort cancellation for corr_id.
	FlagCancel Flags = 1 << 6

	// FlagAck acknowledges one accepted streaming message for corr_id.
	FlagAck Flags = 1 << 7
)

// frame is one decoded MWN1 frame. Frames stay package-internal; callers
// use Conn.SendMessage and Conn.OnMessage with complete payloads.
type frame struct {
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
// Returned by writeFrame for outbound frames and by readFrame when
// the on-wire payload_len header advertises more than MaxPayload.
var ErrPayloadTooLarge = errors.New("mwn1: payload exceeds MaxPayload")

// readFrame scans r for the magic preamble, then reads the rest of
// the frame and verifies its CRC32C. On magic mismatch it slides
// one byte forward and tries again, logging at WARN with the count
// of dropped bytes once it locks on. Returns ErrCorrupted on CRC
// failure, ErrPayloadTooLarge when the advertised payload_len
// exceeds MaxPayload, or the wrapped underlying read error when
// the stream is closed mid-frame.
func readFrame(r io.Reader, log *slog.Logger) (frame, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := scanMagic(r, log); err != nil {
		return frame{}, err
	}
	hdr := make([]byte, HdrAfterMagic)
	if _, err := io.ReadFull(r, hdr); err != nil {
		log.Warn("mwn1: read header", slog.String("err", err.Error()))
		return frame{}, fmt.Errorf("mwn1: read header: %w", err)
	}
	flags := Flags(hdr[0])
	methodID := binary.BigEndian.Uint16(hdr[1:3])
	corrID := binary.BigEndian.Uint64(hdr[3:11])
	payloadLen := binary.BigEndian.Uint32(hdr[11:15])
	if payloadLen > MaxPayload {
		log.Warn("mwn1: payload header exceeds max",
			slog.Uint64("advertised", uint64(payloadLen)),
			slog.Uint64("max", uint64(MaxPayload)))
		return frame{}, fmt.Errorf("%w: advertised %d", ErrPayloadTooLarge, payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		log.Warn("mwn1: read payload", slog.String("err", err.Error()))
		return frame{}, fmt.Errorf("mwn1: read payload: %w", err)
	}
	var crcBuf [TailLen]byte
	if _, err := io.ReadFull(r, crcBuf[:]); err != nil {
		log.Warn("mwn1: read crc", slog.String("err", err.Error()))
		return frame{}, fmt.Errorf("mwn1: read crc: %w", err)
	}
	got := binary.BigEndian.Uint32(crcBuf[:])
	want := computeCRC(hdr, payload)
	if got != want {
		log.Warn("mwn1: crc mismatch",
			slog.String("got", fmt.Sprintf("%08x", got)),
			slog.String("want", fmt.Sprintf("%08x", want)))
		return frame{}, fmt.Errorf("%w: got=%08x want=%08x", ErrCorrupted, got, want)
	}
	return frame{
		Flags:    flags,
		MethodID: methodID,
		CorrID:   corrID,
		Payload:  payload,
	}, nil
}

// writeFrame serializes f and writes it to w in a single Write call.
// Caller is responsible for serializing concurrent writes; frame
// itself does not lock. Returns ErrPayloadTooLarge if the payload
// exceeds MaxPayload.
//
// log is used to emit a single WARN line on payload-size or transport
// write failure so the wrapped error returned to the caller has a
// matching observable log entry. nil is allowed and falls back to
// [slog.Default].
func writeFrame(w io.Writer, f frame, log *slog.Logger) error {
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
// it fits within MaxPayload. writeFrame already returns an error when
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
