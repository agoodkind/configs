// Package opnsense provides the MWN1 client for the OPNsense OOB daemon.
package opnsense

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"goodkind.io/mwan/internal/mwn1"
)

type responseMessage struct {
	methodID uint16
	flags    mwn1.Flags
	payload  []byte
}

// Client speaks MWN1 to the mwan-opnsense daemon over the OOB unix socket.
type Client struct {
	target string
	log    *slog.Logger
	reg    *mwn1.Registry
	conn   *mwn1.Conn

	corrSeq atomic.Uint64

	mu      sync.Mutex
	pending map[uint64]chan responseMessage
	closed  bool
}

var (
	// ErrUnknownMethod means the daemon returned a method id missing from the registry.
	ErrUnknownMethod = errors.New("opnsense: unknown method id from daemon")
	// ErrUnexpectedResponseType means a typed wrapper received the wrong response proto.
	ErrUnexpectedResponseType = errors.New("opnsense: unexpected response type from daemon")
	// ErrClientClosed means the MWN1 transport closed before a response arrived.
	ErrClientClosed = errors.New("opnsense: client is closed")
)

// Dial opens target and starts the MWN1 response dispatcher.
func Dial(target string) (*Client, error) {
	if target == "" {
		err := errors.New("opnsense: empty target")
		slog.Error("opnsense: dial target empty", slog.Any("err", err))
		return nil, err
	}
	socketPath, ok := unixTargetPath(target)
	if !ok {
		err := fmt.Errorf("opnsense: only unix:// targets supported, got %q", target)
		slog.Error("opnsense: unsupported dial target",
			slog.String("target", target),
			slog.Any("err", err))
		return nil, err
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(context.Background(), "unix", socketPath)
	if err != nil {
		wrapped := fmt.Errorf("opnsense: dial %q: %w", target, err)
		slog.Error("opnsense: dial failed",
			slog.String("target", target),
			slog.Any("err", err))
		return nil, wrapped
	}

	reg, err := mwn1.NewMWANOPNsenseRegistry()
	if err != nil {
		_ = conn.Close()
		wrapped := fmt.Errorf("opnsense: build registry: %w", err)
		slog.Error("opnsense: registry build failed", slog.Any("err", err))
		return nil, wrapped
	}

	client := &Client{
		target:  target,
		log:     slog.Default(),
		reg:     reg,
		conn:    nil,
		corrSeq: atomic.Uint64{},
		mu:      sync.Mutex{},
		pending: make(map[uint64]chan responseMessage),
		closed:  false,
	}
	client.conn = mwn1.NewConn(conn, client.log)
	client.conn.OnMessage(client.dispatch)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				client.log.Error("opnsense: cleanup panic recovered",
					slog.Any("err", fmt.Errorf("panic: %v", r)))
			}
		}()
		client.cleanupOnDone()
	}()
	return client, nil
}

// Close tears down the underlying transport and unblocks pending calls.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	if err := c.conn.Close(); err != nil {
		return c.wrapError(context.Background(), "opnsense: close failed", err)
	}
	return nil
}

// Done closes when the underlying MWN1 connection terminates.
func (c *Client) Done() <-chan struct{} {
	return c.conn.Done()
}

// Err returns the first error that closed the underlying MWN1 connection.
func (c *Client) Err() error {
	if err := c.conn.Err(); err != nil {
		return c.wrapError(context.Background(), "opnsense: connection failed", err)
	}
	return nil
}

// Call performs one unary MWN1 request and waits for the matching CorrID response.
func (c *Client) Call(
	ctx context.Context,
	methodID uint16,
	req proto.Message,
) (proto.Message, error) {
	payload, _, err := mwn1.MarshalRequest(c.reg, methodID, req)
	if err != nil {
		return nil, c.wrapError(ctx, "opnsense: call marshal failed", err, slog.Int("method_id", int(methodID)))
	}

	corrID := c.corrSeq.Add(1)
	respCh, err := c.registerPending(corrID)
	if err != nil {
		return nil, err
	}
	defer c.unregisterPending(corrID)

	err = c.conn.SendMessage(methodID, corrID, mwn1.FlagRequest|mwn1.FlagFinal, payload)
	if err != nil {
		return nil, c.wrapError(ctx, "opnsense: call send failed", err,
			slog.Int("method_id", int(methodID)), slog.Uint64("corr_id", corrID))
	}

	select {
	case <-ctx.Done():
		return nil, c.wrapError(ctx, "opnsense: call context done", ctx.Err(), slog.Int("method_id", int(methodID)))
	case resp, ok := <-respCh:
		if !ok {
			return nil, c.connClosedErr(ctx, "call", methodID)
		}
		return c.decodeResponse(ctx, resp)
	}
}

// CallStream sends a client-streaming request and waits for one terminal response.
func (c *Client) CallStream(
	ctx context.Context,
	methodID uint16,
	produce func(send func(proto.Message) error) error,
) (proto.Message, error) {
	return c.callClientStream(ctx, methodID, produce)
}

func (c *Client) callClientStream(
	ctx context.Context,
	methodID uint16,
	produce func(send func(proto.Message) error) error,
) (proto.Message, error) {
	if _, ok := c.reg.MethodName(methodID); !ok {
		return nil, c.wrapError(ctx, "opnsense: unknown stream method", ErrUnknownMethod, slog.Int("method_id", int(methodID)))
	}

	corrID := c.corrSeq.Add(1)
	respCh, err := c.registerPending(corrID)
	if err != nil {
		return nil, err
	}
	defer c.unregisterPending(corrID)
	cancelOnce := sync.Once{}
	sendCancel := func() {
		cancelOnce.Do(func() {
			if cancelErr := c.conn.SendCancel(methodID, corrID); cancelErr != nil &&
				!errors.Is(cancelErr, mwn1.ErrClosed) {
				c.log.WarnContext(ctx, "opnsense: stream cancel send failed",
					slog.Int("method_id", int(methodID)),
					slog.Uint64("corr_id", corrID),
					slog.Any("err", cancelErr))
			}
		})
	}

	if produce != nil {
		produceDone := make(chan error, 1)
		go func() {
			produceDone <- produce(c.streamSendFn(ctx, methodID, corrID, sendCancel))
		}()
		select {
		case <-ctx.Done():
			sendCancel()
			return nil, c.wrapError(ctx, "opnsense: stream context done", ctx.Err(),
				slog.Int("method_id", int(methodID)))
		case produceErr := <-produceDone:
			if produceErr != nil {
				if ctx.Err() != nil {
					sendCancel()
				}
				return nil, c.wrapError(ctx, "opnsense: stream producer failed", produceErr,
					slog.Int("method_id", int(methodID)))
			}
		}
	}

	if err = ctx.Err(); err != nil {
		sendCancel()
		return nil, c.wrapError(ctx, "opnsense: stream context done before final send", err,
			slog.Int("method_id", int(methodID)))
	}

	err = c.conn.SendStreamMessage(
		ctx,
		methodID,
		corrID,
		mwn1.FlagRequest|mwn1.FlagStreaming|mwn1.FlagFinal,
		nil,
	)
	if err != nil {
		return nil, c.wrapError(ctx, "opnsense: stream final send failed", err,
			slog.Int("method_id", int(methodID)), slog.Uint64("corr_id", corrID))
	}

	select {
	case <-ctx.Done():
		sendCancel()
		return nil, c.wrapError(ctx, "opnsense: stream context done", ctx.Err(), slog.Int("method_id", int(methodID)))
	case resp, ok := <-respCh:
		if !ok {
			return nil, c.connClosedErr(ctx, "stream", methodID)
		}
		return c.decodeResponse(ctx, resp)
	}
}

func (c *Client) streamSendFn(
	ctx context.Context,
	methodID uint16,
	corrID uint64,
	sendCancel func(),
) func(proto.Message) error {
	return func(msg proto.Message) error {
		if err := ctx.Err(); err != nil {
			sendCancel()
			return c.wrapError(ctx, "opnsense: stream context done before chunk send", err,
				slog.Int("method_id", int(methodID)), slog.Uint64("corr_id", corrID))
		}
		payload, err := proto.Marshal(msg)
		if err != nil {
			return c.wrapError(ctx, "opnsense: stream chunk marshal failed", err, slog.Int("method_id", int(methodID)))
		}
		if err := ctx.Err(); err != nil {
			sendCancel()
			return c.wrapError(ctx, "opnsense: stream context done before chunk enqueue", err,
				slog.Int("method_id", int(methodID)), slog.Uint64("corr_id", corrID))
		}
		err = c.conn.SendStreamMessage(
			ctx,
			methodID,
			corrID,
			mwn1.FlagRequest|mwn1.FlagStreaming,
			payload,
		)
		if err != nil {
			return c.wrapError(ctx, "opnsense: stream chunk send failed", err,
				slog.Int("method_id", int(methodID)), slog.Uint64("corr_id", corrID))
		}
		return nil
	}
}

func (c *Client) registerPending(corrID uint64) (chan responseMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClientClosed
	}
	ch := make(chan responseMessage, 1)
	c.pending[corrID] = ch
	return ch, nil
}

func (c *Client) unregisterPending(corrID uint64) {
	c.mu.Lock()
	delete(c.pending, corrID)
	c.mu.Unlock()
}

func (c *Client) dispatch(methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) {
	c.mu.Lock()
	ch, ok := c.pending[corrID]
	c.mu.Unlock()
	if !ok {
		c.log.Warn("opnsense: dropping message with no waiter",
			slog.Uint64("corr_id", corrID),
			slog.Int("method_id", int(methodID)))
		return
	}

	msg := responseMessage{methodID: methodID, flags: flags, payload: payload}
	select {
	case ch <- msg:
	default:
		c.log.Error("opnsense: response channel full",
			slog.Any("err", errors.New("response channel full")),
			slog.Uint64("corr_id", corrID),
			slog.Int("method_id", int(methodID)))
	}
}

func (c *Client) cleanupOnDone() {
	<-c.conn.Done()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for corrID, ch := range c.pending {
		close(ch)
		delete(c.pending, corrID)
	}
}

func (c *Client) connClosedErr(ctx context.Context, kind string, methodID uint16) error {
	if err := c.conn.Err(); err != nil {
		return c.wrapError(ctx, "opnsense: connection closed during call", err,
			slog.String("kind", kind), slog.Int("method_id", int(methodID)))
	}
	return c.wrapError(ctx, "opnsense: client closed during call", ErrClientClosed,
		slog.String("kind", kind), slog.Int("method_id", int(methodID)))
}

func (c *Client) decodeResponse(ctx context.Context, resp responseMessage) (proto.Message, error) {
	if resp.flags&mwn1.FlagError != 0 {
		statusProto := &spb.Status{}
		if err := proto.Unmarshal(resp.payload, statusProto); err != nil {
			c.log.WarnContext(ctx, "opnsense: daemon error status decode failed",
				slog.Int("method_id", int(resp.methodID)),
				slog.Any("err", err))
			statusErr := status.Error(codes.Internal, string(resp.payload))
			return nil, statusErr
		}
		statusErr := status.ErrorProto(statusProto)
		c.log.WarnContext(ctx, "opnsense: daemon returned error status",
			slog.Int("method_id", int(resp.methodID)),
			slog.Int("code", int(statusProto.GetCode())),
			slog.String("message", statusProto.GetMessage()))
		return nil, fmt.Errorf("opnsense: daemon status method=%d: %w", resp.methodID, statusErr)
	}
	if _, ok := c.reg.MethodName(resp.methodID); !ok {
		return nil, c.wrapError(ctx, "opnsense: unknown response method", ErrUnknownMethod,
			slog.Int("method_id", int(resp.methodID)))
	}
	msg, err := mwn1.UnmarshalResponse(c.reg, resp.methodID, resp.payload)
	if err != nil {
		return nil, c.wrapError(ctx, "opnsense: response decode failed", err, slog.Int("method_id", int(resp.methodID)))
	}
	return msg, nil
}

func (c *Client) wrapError(ctx context.Context, message string, err error, attrs ...slog.Attr) error {
	_ = attrs
	c.log.ErrorContext(ctx, message, "err", err)
	return fmt.Errorf("%s: %w", message, err)
}

func unixTargetPath(target string) (string, bool) {
	const unixScheme = "unix://"
	if !strings.HasPrefix(target, unixScheme) {
		return "", false
	}
	path := strings.TrimPrefix(target, unixScheme)
	if path == "" {
		return "", false
	}
	return path, true
}
