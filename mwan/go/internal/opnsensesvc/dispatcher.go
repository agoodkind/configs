package opnsensesvc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
)

// streamPushTimeout caps how long the dispatcher reader will block
// pushing a streaming chunk into a per-CorrID channel. The buffer is
// 16 chunks deep so a handler that keeps draining never hits this; it
// only fires if the handler panicked or wedged, in which case dropping
// the frame and logging is preferable to wedging the whole reader.
const streamPushTimeout = 30 * time.Second

// Dispatcher reads MWN1 frames from an underlying virtio-serial device
// (wrapped in a [mwn1.Conn]), routes each request to the matching
// [Server] handler, and writes the response frame back through the
// same Conn.
//
// Unary requests run in worker goroutines drawn from a fixed pool so a
// slow handler cannot block the reader. Streaming requests (currently
// only Deploy) push frames into a per-CorrID channel that the handler
// reads from on a dedicated goroutine; the channel is closed when the
// FlagFinal frame arrives.
type Dispatcher struct {
	registry *mwn1.Registry
	server   *Server
	log      *slog.Logger
	workers  int

	// onFrameError is invoked when a handler dispatch path fails before a
	// response frame can be assembled, e.g. a request payload that fails
	// to unmarshal. Used by tests to assert error counts.
	onFrameError func(error)

	// streamMu guards streams. Streams maps CorrID to the in-progress
	// chunk channel for an active client-streaming RPC. The handler
	// goroutine reads chunks until the channel is closed (FlagFinal).
	streamMu        sync.Mutex
	streams         map[uint64]*streamBuffer
	canceledStreams map[uint64]struct{}

	// streamWG tracks active streaming-handler goroutines so Serve can
	// drain them on shutdown.
	streamWG sync.WaitGroup

	// frameCh feeds the worker pool. Buffered so the reader goroutine
	// does not block when all workers are busy on slow handlers.
	frameCh chan dispatchJob

	conn *mwn1.Conn
}

// DispatcherConfig configures a Dispatcher. Registry defaults to
// [mwn1.NewMWANOPNsenseRegistry] when nil. Workers defaults to
// [runtime.NumCPU] when zero. Log defaults to [slog.Default] when nil.
type DispatcherConfig struct {
	Registry     *mwn1.Registry
	Server       *Server
	Workers      int
	Log          *slog.Logger
	OnFrameError func(error)
}

// streamBuffer carries the per-CorrID chunk channel for an active
// client-streaming RPC. The handler goroutine reads from ch until it is
// closed (FlagFinal). cancel aborts the handler when the dispatcher
// shuts down.
type streamBuffer struct {
	ch        chan *mwanv1.Chunk
	cancel    context.CancelFunc
	closeOnce sync.Once
}

// dispatchJob carries a fully-decoded unary request to a worker
// goroutine. Streaming RPCs do not flow through frameCh; they run on
// dedicated per-CorrID goroutines spawned in handleStreamFrame.
type dispatchJob struct {
	methodID uint16
	corrID   uint64
	req      proto.Message
}

// NewDispatcher constructs a Dispatcher. It does not open or attach to
// any transport; call [Dispatcher.Serve] to begin processing frames.
// Returns an error if the default registry cannot be built (which would
// indicate a programming bug in [mwn1.RegisterMWANOPNsenseService]).
func NewDispatcher(cfg DispatcherConfig) (*Dispatcher, error) {
	registry := cfg.Registry
	if registry == nil {
		built, regErr := mwn1.NewMWANOPNsenseRegistry()
		if regErr != nil {
			log := cfg.Log
			if log == nil {
				log = slog.Default()
			}
			log.Error("dispatcher: build default registry failed", "err", regErr)
			return nil, fmt.Errorf("dispatcher: build default registry: %w", regErr)
		}
		registry = built
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{
		registry:        registry,
		server:          cfg.Server,
		log:             log,
		workers:         workers,
		onFrameError:    cfg.OnFrameError,
		streamMu:        sync.Mutex{},
		streams:         make(map[uint64]*streamBuffer),
		canceledStreams: make(map[uint64]struct{}),
		streamWG:        sync.WaitGroup{},
		frameCh:         make(chan dispatchJob, workers*2),
		conn:            nil,
	}, nil
}

// Serve attaches the dispatcher to rw and runs until ctx is cancelled
// or the underlying [mwn1.Conn] errors. Returns nil on clean shutdown
// (ctx cancel) and the underlying error otherwise.
func (d *Dispatcher) Serve(ctx context.Context, rw io.ReadWriteCloser) error {
	if d.server == nil {
		return errors.New("dispatcher: Server required")
	}
	if rw == nil {
		return errors.New("dispatcher: rw required")
	}

	d.conn = mwn1.NewConn(rw, d.log)
	d.conn.OnMessage(d.onMessage)

	// Worker pool. Each worker pulls jobs off frameCh and runs to
	// completion. Workers exit when frameCh is closed.
	var wg sync.WaitGroup
	wg.Add(d.workers)
	for range d.workers {
		go func() {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					d.log.ErrorContext(ctx, "dispatcher: worker panic recovered",
						"err", fmt.Errorf("recovered panic: %v", recovered))
				}
			}()
			for job := range d.frameCh {
				d.handleJob(ctx, job)
			}
		}()
	}

	var serveErr error
	cancelled := false
	select {
	case <-ctx.Done():
		cancelled = true
		d.log.InfoContext(ctx, "dispatcher: context cancelled, closing conn")
	case <-d.conn.Done():
		serveErr = d.conn.Err()
		if serveErr != nil && !errors.Is(serveErr, io.EOF) {
			d.log.ErrorContext(ctx, "dispatcher: conn read loop ended", "err", serveErr)
		}
	}

	if closeErr := d.conn.Close(); closeErr != nil {
		d.log.WarnContext(ctx, "dispatcher: conn close failed", "err", closeErr)
	}
	close(d.frameCh)
	wg.Wait()

	// Cancel any in-flight streaming handlers and wait for their goroutines
	// to exit so Serve does not return while handlers still hold open
	// channels or temp files.
	d.streamMu.Lock()
	for corrID, buf := range d.streams {
		buf.cancel()
		buf.close()
		delete(d.streams, corrID)
	}
	d.streamMu.Unlock()
	d.streamWG.Wait()

	if cancelled || errors.Is(serveErr, io.EOF) {
		return nil
	}
	if serveErr == nil {
		return nil
	}
	d.log.ErrorContext(ctx, "dispatcher: serve terminating with error", "err", serveErr)
	return fmt.Errorf("dispatcher: conn err: %w", serveErr)
}

// onMessage is the complete-message callback wired into [mwn1.Conn].
// It runs on the Conn reader goroutine and must not block.
func (d *Dispatcher) onMessage(methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) {
	if flags&mwn1.FlagCancel != 0 {
		d.cancelStream(corrID)
		return
	}

	if flags&mwn1.FlagRequest == 0 {
		// Spurious response frame on the daemon side. Log and drop; the
		// daemon never initiates requests.
		d.log.Warn("dispatcher: dropped non-request frame",
			"method_id", methodID,
			"corr_id", corrID,
			"flags", uint8(flags))
		return
	}

	if flags&mwn1.FlagStreaming != 0 {
		d.handleStreamMessage(methodID, corrID, flags, payload)
		return
	}

	req, err := mwn1.UnmarshalRequest(d.registry, methodID, payload)
	if err != nil {
		d.reportError(err)
		d.sendError(methodID, corrID, err)
		return
	}
	d.frameCh <- dispatchJob{
		methodID: methodID,
		corrID:   corrID,
		req:      req,
	}
}

// handleStreamMessage routes a streaming-request message into the
// per-CorrID chunk channel. The first frame for a new CorrID spawns a
// handler goroutine that consumes the channel; subsequent frames push
// chunks in. An empty FlagFinal message closes the channel, which lets
// the handler observe end-of-stream and produce the response. Currently
// only Deploy uses streaming; other method ids in a streaming frame are
// rejected.
func (d *Dispatcher) handleStreamMessage(
	methodID uint16,
	corrID uint64,
	flags mwn1.Flags,
	payload []byte,
) {
	if d.isCanceled(corrID) {
		return
	}

	if methodID != mwn1.MethodDeploy {
		err := fmt.Errorf("dispatcher: streaming not supported for method id %d", methodID)
		d.reportError(err)
		d.sendError(methodID, corrID, err)
		return
	}

	if flags&mwn1.FlagFinal != 0 && len(payload) == 0 {
		if d.closeStream(methodID, corrID) {
			d.ackStreamMessage(methodID, corrID)
		}
		return
	}

	msg, err := mwn1.UnmarshalRequest(d.registry, methodID, payload)
	if err != nil {
		d.log.Warn("dispatcher: stream unmarshal failed",
			"method_id", methodID,
			"corr_id", corrID,
			"payload_len", len(payload),
			"err", err)
		d.reportError(err)
		d.sendError(methodID, corrID, err)
		return
	}
	chunk, ok := msg.(*mwanv1.Chunk)
	if !ok {
		err := fmt.Errorf("dispatcher: Deploy frame payload not a Chunk: %T", msg)
		d.reportError(err)
		d.sendError(methodID, corrID, err)
		return
	}

	final := flags&mwn1.FlagFinal != 0

	d.streamMu.Lock()
	buf, exists := d.streams[corrID]
	if !exists {
		// First frame for this CorrID. Spin up the handler goroutine
		// before publishing the channel so subsequent frames find a live
		// receiver.
		ctx, cancel := context.WithCancel(context.Background())
		buf = &streamBuffer{
			ch:     make(chan *mwanv1.Chunk, 16),
			cancel: cancel,
		}
		d.streams[corrID] = buf

		ch := buf.ch
		d.streamWG.Go(func() {
			defer cancel()
			defer func() {
				if recovered := recover(); recovered != nil {
					d.log.Error("dispatcher: stream handler panic recovered",
						"err", fmt.Errorf("recovered panic: %v", recovered),
						"method_id", methodID, "corr_id", corrID)
					d.sendError(methodID, corrID, fmt.Errorf("dispatcher: stream handler panic: %v", recovered))
				}
			}()
			resp, handlerErr := d.server.Deploy(ctx, ch)
			if handlerErr != nil {
				if ctx.Err() != nil {
					return
				}
				d.sendError(methodID, corrID, handlerErr)
				return
			}
			if ctx.Err() != nil {
				return
			}
			if sendErr := d.sendResponse(methodID, corrID, resp); sendErr != nil {
				d.log.Error("dispatcher: send response failed",
					"err", sendErr, "method_id", methodID, "corr_id", corrID)
			}
		})
	}
	if final {
		delete(d.streams, corrID)
	}
	ch := buf.ch
	d.streamMu.Unlock()

	// Push chunk; if the handler already exited the channel may be at
	// capacity and a slow handler would block the reader. Apply the
	// dispatcher ctx via a non-blocking guard around send: if the
	// per-stream context is cancelled mid-push, drop with a log so the
	// reader keeps moving.
	if ok := sendStreamChunk(ch, chunk); !ok {
		d.log.Warn("dispatcher stream closed before chunk push",
			"method_id", methodID, "corr_id", corrID)
		return
	}
	if !d.ackStreamMessage(methodID, corrID) {
		return
	}

	if final {
		buf.close()
	}
}

func sendStreamChunk(ch chan<- *mwanv1.Chunk, chunk *mwanv1.Chunk) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case ch <- chunk:
		return true
	case <-time.After(streamPushTimeout):
		return false
	}
}

func (d *Dispatcher) closeStream(methodID uint16, corrID uint64) bool {
	d.streamMu.Lock()
	if _, canceled := d.canceledStreams[corrID]; canceled {
		d.streamMu.Unlock()
		return false
	}
	buf, exists := d.streams[corrID]
	if exists {
		delete(d.streams, corrID)
	}
	d.streamMu.Unlock()
	if !exists {
		err := fmt.Errorf("dispatcher: final streaming frame for unknown corr_id %d", corrID)
		d.reportError(err)
		d.sendError(methodID, corrID, err)
		return false
	}
	buf.close()
	return true
}

func (d *Dispatcher) ackStreamMessage(methodID uint16, corrID uint64) bool {
	if err := d.conn.SendAck(methodID, corrID); err != nil {
		if !errors.Is(err, mwn1.ErrClosed) {
			d.log.Warn("dispatcher: stream ACK failed",
				"err", err, "method_id", methodID, "corr_id", corrID)
		}
		return false
	}
	return true
}

func (d *Dispatcher) cancelStream(corrID uint64) {
	d.streamMu.Lock()
	buf, exists := d.streams[corrID]
	if exists {
		delete(d.streams, corrID)
		buf.cancel()
		buf.close()
	}
	d.canceledStreams[corrID] = struct{}{}
	d.streamMu.Unlock()
}

func (d *Dispatcher) isCanceled(corrID uint64) bool {
	d.streamMu.Lock()
	defer d.streamMu.Unlock()
	_, canceled := d.canceledStreams[corrID]
	return canceled
}

func (b *streamBuffer) close() {
	b.closeOnce.Do(func() {
		close(b.ch)
	})
}

// handleJob dispatches one assembled request to its handler and writes
// the response frame. Errors are converted into FlagError frames.
func (d *Dispatcher) handleJob(ctx context.Context, job dispatchJob) {
	resp, err := d.invoke(ctx, job)
	if err != nil {
		d.sendError(job.methodID, job.corrID, err)
		return
	}
	if err := d.sendResponse(job.methodID, job.corrID, resp); err != nil {
		d.log.ErrorContext(ctx, "dispatcher: send response failed",
			"err", err, "method_id", job.methodID, "corr_id", job.corrID)
	}
}

// invoke runs the unary handler matching job.methodID and returns its
// response message. Streaming RPCs (Deploy) do not flow through invoke;
// they are handled in handleStreamFrame on a dedicated goroutine.
func (d *Dispatcher) invoke(ctx context.Context, job dispatchJob) (proto.Message, error) {
	handler, ok := d.unaryHandler(job.methodID)
	if !ok {
		return nil, fmt.Errorf("unknown method id %d", job.methodID)
	}
	return handler(ctx, job.req)
}

// unaryHandler returns a typed-erased handler function for methodID, or
// false if methodID is not a unary RPC. The handler internally type-
// asserts the request to the expected concrete type.
func (d *Dispatcher) unaryHandler(methodID uint16) (func(context.Context, proto.Message) (proto.Message, error), bool) {
	switch methodID {
	case mwn1.MethodVersion:
		return wrapHandler(d.server.Version), true
	case mwn1.MethodExec:
		return wrapHandler(d.server.Exec), true
	case mwn1.MethodReadConfigXML:
		return wrapHandler(d.server.ReadConfigXML), true
	case mwn1.MethodWriteConfigXML:
		return wrapHandler(d.server.WriteConfigXML), true
	case mwn1.MethodBackupConfigXML:
		return wrapHandler(d.server.BackupConfigXML), true
	case mwn1.MethodXPathGet:
		return wrapHandler(d.server.XPathGet), true
	case mwn1.MethodXPathSet:
		return wrapHandler(d.server.XPathSet), true
	case mwn1.MethodXPathDelete:
		return wrapHandler(d.server.XPathDelete), true
	case mwn1.MethodStripGatewayV6:
		return wrapHandler(d.server.StripGatewayV6), true
	case mwn1.MethodInjectGatewayV6:
		return wrapHandler(d.server.InjectGatewayV6), true
	case mwn1.MethodDeployStatus:
		return wrapHandler(d.server.DeployStatus), true
	case mwn1.MethodRevert:
		return wrapHandler(d.server.Revert), true
	default:
		return nil, false
	}
}

// wrapHandler adapts a typed handler function into the type-erased
// signature used by the dispatcher table. The returned function casts
// the incoming proto.Message to Req via [castOrErr] and returns an
// error frame if the cast fails.
func wrapHandler[Req proto.Message, Resp proto.Message](
	handler func(context.Context, Req) (Resp, error),
) func(context.Context, proto.Message) (proto.Message, error) {
	return func(ctx context.Context, raw proto.Message) (proto.Message, error) {
		req, err := castOrErr[Req](raw)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// sendResponse marshals resp and sends a single FlagFinal response
// frame.
func (d *Dispatcher) sendResponse(methodID uint16, corrID uint64, resp proto.Message) error {
	payload, _, err := mwn1.MarshalResponse(d.registry, methodID, resp)
	if err != nil {
		// Marshal failed: fall back to an error frame so the caller
		// is not left waiting forever.
		return d.sendError(methodID, corrID, err)
	}
	if err := d.conn.SendMessage(methodID, corrID, mwn1.FlagResponse, payload); err != nil {
		d.log.Error("dispatcher: send response failed",
			"err", err, "method_id", methodID, "corr_id", corrID)
		return fmt.Errorf("dispatcher: send response: %w", err)
	}
	return nil
}

// sendError serializes err as a [google.rpc.Status] proto and sends a
// FlagError|FlagFinal response frame. If the payload itself fails to
// marshal (which would be a programming bug), an empty FlagError frame
// is sent so the caller can at least observe the error bit.
func (d *Dispatcher) sendError(methodID uint16, corrID uint64, handlerErr error) error {
	payload := marshalStatus(handlerErr)
	flags := mwn1.FlagResponse | mwn1.FlagError
	if err := d.conn.SendMessage(methodID, corrID, flags, payload); err != nil {
		d.log.Warn("dispatcher: send error frame failed",
			"err", err,
			"method_id", methodID,
			"corr_id", corrID,
			"handler_err", handlerErr)
		return fmt.Errorf("dispatcher: send error frame: %w", err)
	}
	return nil
}

// marshalStatus encodes err as a serialized google.rpc.Status. The
// status code is fixed to 13 (INTERNAL); the original error message is
// preserved verbatim. The dispatcher does not currently surface
// finer-grained codes because the daemon-side handlers do not carry
// status code metadata.
func marshalStatus(err error) []byte {
	s := &status.Status{
		Code:    13, // google.rpc.Code.INTERNAL
		Message: err.Error(),
	}
	out, marshalErr := proto.Marshal(s)
	if marshalErr != nil {
		// Should never happen for a populated Status. Return a
		// best-effort plain-text payload so the bit is still observable.
		return []byte(err.Error())
	}
	return out
}

// reportError forwards err to the test-only callback when set.
func (d *Dispatcher) reportError(err error) {
	if d.onFrameError != nil {
		d.onFrameError(err)
	}
}

// castOrErr asserts msg to T. On mismatch it returns a zero value and
// an error, which the dispatcher then surfaces back to the client as
// a FlagError frame. Mismatches indicate a registry/handler wiring bug.
func castOrErr[T proto.Message](msg proto.Message) (T, error) {
	out, ok := msg.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("dispatcher: type mismatch: got %T want %T", msg, zero)
	}
	return out, nil
}
