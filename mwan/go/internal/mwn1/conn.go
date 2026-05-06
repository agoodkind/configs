package mwn1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

const (
	sendQueueDepth             = 4
	streamFramePayload         = 1024
	defaultReassemblyLimit     = 256 * 1024 * 1024
	reassemblyOverflowResponse = "mwn1: reassembly buffer exceeded"
)

// ErrClosed is returned after the connection has shut down.
var ErrClosed = errors.New("mwn1: connection closed")

// ErrReassemblyTooLarge indicates that fragments for one corr_id exceeded the cap.
var ErrReassemblyTooLarge = errors.New("mwn1: reassembly buffer exceeded")

// ErrAckWaiterExists indicates overlapping ACK waits for one corr_id.
var ErrAckWaiterExists = errors.New("mwn1: ack waiter already exists")

// ErrStreamCanceled indicates a stream was canceled before ACK.
var ErrStreamCanceled = errors.New("mwn1: stream canceled")

type messageHandler func(methodID uint16, corrID uint64, flags Flags, payload []byte)

type reassembly struct {
	methodID uint16
	flags    Flags
	payload  []byte
}

// Conn wraps an [io.ReadWriteCloser] as a message-oriented MWN1 transport.
type Conn struct {
	rw              io.ReadWriteCloser
	log             *slog.Logger
	sendCh          chan frame
	controlCh       chan frame
	done            chan struct{}
	reassemblyLimit int

	handlerMu    sync.RWMutex
	handler      messageHandler
	handlerReady chan struct{}
	handlerOnce  sync.Once

	reassemblyMu sync.Mutex
	reassemblies map[uint64]*reassembly

	ackMu      sync.Mutex
	ackWaiters map[uint64]chan error

	shutdownOnce sync.Once
	errMu        sync.Mutex
	err          error
}

// NewConn starts one reader goroutine and one writer goroutine for rw.
func NewConn(rwc io.ReadWriteCloser, log *slog.Logger) *Conn {
	return newConnWithReassemblyLimit(rwc, log, defaultReassemblyLimit)
}

func newConnWithReassemblyLimit(
	rwc io.ReadWriteCloser,
	log *slog.Logger,
	reassemblyLimit int,
) *Conn {
	if log == nil {
		log = slog.Default()
	}
	conn := &Conn{
		rw:              rwc,
		log:             log,
		sendCh:          make(chan frame, sendQueueDepth),
		controlCh:       make(chan frame, sendQueueDepth),
		done:            make(chan struct{}),
		reassemblyLimit: reassemblyLimit,
		handlerReady:    make(chan struct{}),
		reassemblies:    make(map[uint64]*reassembly),
		handlerMu:       sync.RWMutex{},
		handler:         nil,
		handlerOnce:     sync.Once{},
		reassemblyMu:    sync.Mutex{},
		ackMu:           sync.Mutex{},
		ackWaiters:      make(map[uint64]chan error),
		shutdownOnce:    sync.Once{},
		errMu:           sync.Mutex{},
		err:             nil,
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				conn.log.Error("mwn1: readLoop panic recovered", slog.Any("err", fmt.Errorf("panic: %v", r)))
			}
		}()
		conn.readLoop()
	}()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				conn.log.Error("mwn1: writeLoop panic recovered", slog.Any("err", fmt.Errorf("panic: %v", r)))
			}
		}()
		conn.writeLoop()
	}()
	return conn
}

// SendCancel sends a best-effort cancellation frame for corrID. Cancel frames
// use the control path so they bypass queued normal frames.
func (c *Conn) SendCancel(methodID uint16, corrID uint64) error {
	c.completeAck(corrID, ErrStreamCanceled)
	return c.sendControlFrame(frame{
		Flags:    FlagCancel,
		MethodID: methodID,
		CorrID:   corrID,
		Payload:  nil,
	})
}

// SendAck acknowledges that one streaming message for corrID has been
// accepted by this hop.
func (c *Conn) SendAck(methodID uint16, corrID uint64) error {
	return c.sendControlFrame(frame{
		Flags:    FlagAck,
		MethodID: methodID,
		CorrID:   corrID,
		Payload:  nil,
	})
}

// SendMessage sends one complete logical message. Payloads larger than
// MaxPayload are split into transport frames sharing corrID.
func (c *Conn) SendMessage(
	methodID uint16,
	corrID uint64,
	flags Flags,
	payload []byte,
) error {
	baseFlags := flags &^ FlagFragment
	if len(payload) == 0 {
		return c.sendFrame(frame{
			Flags:    baseFlags,
			MethodID: methodID,
			CorrID:   corrID,
			Payload:  nil,
		})
	}
	for offset := 0; offset < len(payload); offset += MaxPayload {
		end := min(offset+MaxPayload, len(payload))
		frameFlags := baseFlags
		if end != len(payload) {
			frameFlags |= FlagFragment
		}
		outboundPayload := payload[offset:end]
		outboundFrame := frame{
			Flags:    frameFlags,
			MethodID: methodID,
			CorrID:   corrID,
			Payload:  outboundPayload,
		}
		if err := c.sendFrame(outboundFrame); err != nil {
			return err
		}
	}
	return nil
}

// SendStreamMessage sends one complete streaming message and waits for
// the receiver to ACK that message for corrID.
func (c *Conn) SendStreamMessage(
	ctx context.Context,
	methodID uint16,
	corrID uint64,
	flags Flags,
	payload []byte,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	baseFlags := (flags | FlagStreaming) &^ FlagFragment
	if len(payload) == 0 {
		return c.sendStreamFrameAndWait(ctx, frame{
			Flags:    baseFlags,
			MethodID: methodID,
			CorrID:   corrID,
			Payload:  nil,
		}, len(payload))
	}
	for offset := 0; offset < len(payload); offset += streamFramePayload {
		end := min(offset+streamFramePayload, len(payload))
		frameFlags := baseFlags
		if end != len(payload) {
			frameFlags |= FlagFragment
		}
		if err := c.sendStreamFrameAndWait(ctx, frame{
			Flags:    frameFlags,
			MethodID: methodID,
			CorrID:   corrID,
			Payload:  payload[offset:end],
		}, len(payload)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) sendStreamFrameAndWait(ctx context.Context, outboundFrame frame, messagePayloadLen int) error {
	ackCh, err := c.registerAckWaiter(outboundFrame.CorrID)
	if err != nil {
		return err
	}
	defer c.unregisterAckWaiter(outboundFrame.CorrID, ackCh)

	if err := c.sendFrameContext(ctx, outboundFrame); err != nil {
		return err
	}
	select {
	case err := <-ackCh:
		return err
	case <-ctx.Done():
		c.log.Warn("mwn1: stream ack wait context done",
			slog.Int("method_id", int(outboundFrame.MethodID)),
			slog.Uint64("corr_id", outboundFrame.CorrID),
			slog.Uint64("flags", uint64(outboundFrame.Flags)),
			slog.Int("frame_payload_len", len(outboundFrame.Payload)),
			slog.Int("message_payload_len", messagePayloadLen),
			slog.String("err", ctx.Err().Error()))
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	}
}

// OnMessage installs the handler invoked once per complete logical message.
func (c *Conn) OnMessage(
	handler func(methodID uint16, corrID uint64, flags Flags, payload []byte),
) {
	c.handlerMu.Lock()
	c.handler = handler
	c.handlerMu.Unlock()
	c.handlerOnce.Do(func() {
		close(c.handlerReady)
	})
}

// Done returns a channel closed once for the lifetime of the connection.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

// Err returns the first error that shut down the connection.
func (c *Conn) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

// Close shuts down the connection and closes the underlying transport.
func (c *Conn) Close() error {
	c.shutdown(nil)
	return nil
}

func (c *Conn) sendFrame(outboundFrame frame) error {
	return c.sendQueuedFrame(c.sendCh, outboundFrame)
}

func (c *Conn) sendFrameContext(ctx context.Context, outboundFrame frame) error {
	return c.sendQueuedFrameContext(ctx, c.sendCh, outboundFrame)
}

func (c *Conn) sendControlFrame(outboundFrame frame) error {
	return c.sendQueuedFrame(c.controlCh, outboundFrame)
}

func (c *Conn) sendQueuedFrame(ch chan<- frame, outboundFrame frame) error {
	select {
	case <-c.done:
		return ErrClosed
	default:
	}
	select {
	case ch <- outboundFrame:
		return nil
	case <-c.done:
		return ErrClosed
	}
}

func (c *Conn) sendQueuedFrameContext(ctx context.Context, ch chan<- frame, outboundFrame frame) error {
	select {
	case <-c.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case ch <- outboundFrame:
		return nil
	case <-c.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Conn) sendMessageContext(
	ctx context.Context,
	methodID uint16,
	corrID uint64,
	flags Flags,
	payload []byte,
	maxPayload int,
) error {
	baseFlags := flags &^ FlagFragment
	if len(payload) == 0 {
		return c.sendFrameContext(ctx, frame{
			Flags:    baseFlags,
			MethodID: methodID,
			CorrID:   corrID,
			Payload:  nil,
		})
	}
	for offset := 0; offset < len(payload); offset += maxPayload {
		end := min(offset+maxPayload, len(payload))
		frameFlags := baseFlags
		if end != len(payload) {
			frameFlags |= FlagFragment
		}
		outboundFrame := frame{
			Flags:    frameFlags,
			MethodID: methodID,
			CorrID:   corrID,
			Payload:  payload[offset:end],
		}
		if err := c.sendFrameContext(ctx, outboundFrame); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) readLoop() {
	for {
		inboundFrame, err := readFrame(c.rw, c.log)
		if err != nil {
			c.shutdown(err)
			return
		}
		c.handleFrame(inboundFrame)
	}
}

func (c *Conn) writeLoop() {
	var pendingNormal *frame
	for {
		if pendingNormal == nil {
			select {
			case outboundFrame := <-c.controlCh:
				if err := writeFrame(c.rw, outboundFrame, c.log); err != nil {
					c.shutdown(err)
					return
				}
				continue
			default:
			}
			select {
			case outboundFrame := <-c.controlCh:
				if err := writeFrame(c.rw, outboundFrame, c.log); err != nil {
					c.shutdown(err)
					return
				}
				continue
			case outboundFrame := <-c.sendCh:
				pendingNormal = &outboundFrame
			case <-c.done:
				return
			}
		}

		select {
		case outboundFrame := <-c.controlCh:
			if err := writeFrame(c.rw, outboundFrame, c.log); err != nil {
				c.shutdown(err)
				return
			}
			continue
		default:
		}

		if err := writeFrame(c.rw, *pendingNormal, c.log); err != nil {
			c.shutdown(err)
			return
		}
		pendingNormal = nil
		select {
		case <-c.done:
			return
		default:
		}
	}
}

func (c *Conn) handleFrame(inboundFrame frame) {
	if inboundFrame.Flags&FlagAck != 0 {
		c.completeAck(inboundFrame.CorrID, nil)
		return
	}
	if inboundFrame.Flags&FlagCancel != 0 {
		c.reassemblyMu.Lock()
		delete(c.reassemblies, inboundFrame.CorrID)
		c.reassemblyMu.Unlock()
		c.completeAck(inboundFrame.CorrID, ErrStreamCanceled)
		c.deliver(inboundFrame.MethodID, inboundFrame.CorrID, inboundFrame.Flags, nil)
		return
	}

	c.reassemblyMu.Lock()
	_, isReassembling := c.reassemblies[inboundFrame.CorrID]
	c.reassemblyMu.Unlock()
	isFragmented := inboundFrame.Flags&FlagFragment != 0 || isReassembling
	if !isFragmented {
		c.deliver(inboundFrame.MethodID, inboundFrame.CorrID, inboundFrame.Flags, inboundFrame.Payload)
		return
	}
	completedFrame, complete, exceeded := c.appendFragment(inboundFrame)
	if exceeded {
		c.sendReassemblyError(inboundFrame)
		return
	}
	if inboundFrame.Flags&FlagStreaming != 0 && !complete {
		if err := c.SendAck(inboundFrame.MethodID, inboundFrame.CorrID); err != nil &&
			!errors.Is(err, ErrClosed) {
			c.log.Warn("mwn1: stream fragment ack failed",
				slog.Int("method_id", int(inboundFrame.MethodID)),
				slog.Uint64("corr_id", inboundFrame.CorrID),
				slog.String("err", err.Error()))
		}
	}
	if complete {
		c.deliver(
			completedFrame.MethodID,
			completedFrame.CorrID,
			completedFrame.Flags,
			completedFrame.Payload,
		)
	}
}

func (c *Conn) appendFragment(inboundFrame frame) (frame, bool, bool) {
	c.reassemblyMu.Lock()
	defer c.reassemblyMu.Unlock()

	entry, ok := c.reassemblies[inboundFrame.CorrID]
	if !ok {
		entry = &reassembly{
			methodID: inboundFrame.MethodID,
			flags:    inboundFrame.Flags &^ FlagFragment,
			payload:  make([]byte, 0, len(inboundFrame.Payload)),
		}
		c.reassemblies[inboundFrame.CorrID] = entry
	}
	entry.flags |= inboundFrame.Flags &^ FlagFragment
	nextSize := len(entry.payload) + len(inboundFrame.Payload)
	if nextSize > c.reassemblyLimit {
		delete(c.reassemblies, inboundFrame.CorrID)
		return frame{Flags: 0, MethodID: 0, CorrID: 0, Payload: nil}, false, true
	}
	entry.payload = append(entry.payload, inboundFrame.Payload...)
	if inboundFrame.Flags&FlagFragment != 0 {
		return frame{Flags: 0, MethodID: 0, CorrID: 0, Payload: nil}, false, false
	}
	delete(c.reassemblies, inboundFrame.CorrID)
	return frame{
		Flags:    entry.flags,
		MethodID: entry.methodID,
		CorrID:   inboundFrame.CorrID,
		Payload:  entry.payload,
	}, true, false
}

func (c *Conn) deliver(methodID uint16, corrID uint64, flags Flags, payload []byte) {
	select {
	case <-c.handlerReady:
	case <-c.done:
		return
	}
	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler != nil {
		handler(methodID, corrID, flags, payload)
	}
}

func (c *Conn) sendReassemblyError(inboundFrame frame) {
	err := c.SendMessage(
		inboundFrame.MethodID,
		inboundFrame.CorrID,
		(inboundFrame.Flags&^(FlagRequest|FlagStreaming|FlagFragment|FlagFinal))|
			FlagResponse|FlagError,
		[]byte(reassemblyOverflowResponse),
	)
	if err != nil && !errors.Is(err, ErrClosed) {
		c.log.Warn("mwn1: send reassembly error", slog.String("err", err.Error()))
	}
}

func (c *Conn) registerAckWaiter(corrID uint64) (chan error, error) {
	c.ackMu.Lock()
	defer c.ackMu.Unlock()
	if _, exists := c.ackWaiters[corrID]; exists {
		return nil, ErrAckWaiterExists
	}
	ch := make(chan error, 1)
	c.ackWaiters[corrID] = ch
	return ch, nil
}

func (c *Conn) unregisterAckWaiter(corrID uint64, ch chan error) {
	c.ackMu.Lock()
	if current := c.ackWaiters[corrID]; current == ch {
		delete(c.ackWaiters, corrID)
	}
	c.ackMu.Unlock()
}

func (c *Conn) completeAck(corrID uint64, err error) {
	c.ackMu.Lock()
	ch, ok := c.ackWaiters[corrID]
	if ok {
		delete(c.ackWaiters, corrID)
	}
	c.ackMu.Unlock()
	if !ok {
		return
	}
	ch <- err
}

func (c *Conn) completeAllAcks(err error) {
	c.ackMu.Lock()
	waiters := c.ackWaiters
	c.ackWaiters = make(map[uint64]chan error)
	c.ackMu.Unlock()
	for _, ch := range waiters {
		ch <- err
	}
}

func (c *Conn) shutdown(err error) {
	c.shutdownOnce.Do(func() {
		if err != nil {
			c.errMu.Lock()
			c.err = err
			c.errMu.Unlock()
		}
		if closeErr := c.rw.Close(); closeErr != nil && err == nil {
			c.errMu.Lock()
			c.err = fmt.Errorf("mwn1: close transport: %w", closeErr)
			c.errMu.Unlock()
		}
		c.completeAllAcks(ErrClosed)
		close(c.done)
	})
}
