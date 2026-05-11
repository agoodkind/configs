package opnsense

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
)

// RPC exposes typed methods for the mwan-opnsense service.
type RPC struct {
	c *Client
}

// RPC returns the typed service wrapper for this client.
func (c *Client) RPC() *RPC {
	return &RPC{c: c}
}

func callTyped[Resp proto.Message](
	ctx context.Context,
	c *Client,
	methodID uint16,
	req proto.Message,
) (Resp, error) {
	var zero Resp
	resp, err := c.Call(ctx, methodID, req)
	if err != nil {
		return zero, err
	}
	typed, ok := resp.(Resp)
	if !ok {
		err := fmt.Errorf("%w: got %T for method %d", ErrUnexpectedResponseType, resp, methodID)
		c.log.ErrorContext(ctx, "opnsense: unexpected typed response",
			slog.Int("method_id", int(methodID)),
			slog.Any("err", err))
		return zero, err
	}
	return typed, nil
}

// Version calls the Version RPC.
func (r *RPC) Version(ctx context.Context, req *mwanv1.VersionRequest) (*mwanv1.VersionResponse, error) {
	return callTyped[*mwanv1.VersionResponse](ctx, r.c, mwn1.MethodVersion, req)
}

// Exec calls the Exec RPC.
func (r *RPC) Exec(ctx context.Context, req *mwanv1.ExecRequest) (*mwanv1.ExecResponse, error) {
	return callTyped[*mwanv1.ExecResponse](ctx, r.c, mwn1.MethodExec, req)
}

// ReadConfigXML calls the ReadConfigXML RPC.
func (r *RPC) ReadConfigXML(ctx context.Context, req *mwanv1.ReadConfigXMLRequest) (*mwanv1.ReadConfigXMLResponse, error) {
	return callTyped[*mwanv1.ReadConfigXMLResponse](ctx, r.c, mwn1.MethodReadConfigXML, req)
}

// WriteConfigXML calls the WriteConfigXML RPC.
func (r *RPC) WriteConfigXML(ctx context.Context, req *mwanv1.WriteConfigXMLRequest) (*mwanv1.WriteConfigXMLResponse, error) {
	return callTyped[*mwanv1.WriteConfigXMLResponse](ctx, r.c, mwn1.MethodWriteConfigXML, req)
}

// BackupConfigXML calls the BackupConfigXML RPC.
func (r *RPC) BackupConfigXML(ctx context.Context, req *mwanv1.BackupConfigXMLRequest) (*mwanv1.BackupConfigXMLResponse, error) {
	return callTyped[*mwanv1.BackupConfigXMLResponse](ctx, r.c, mwn1.MethodBackupConfigXML, req)
}

// XPathGet calls the XPathGet RPC.
func (r *RPC) XPathGet(ctx context.Context, req *mwanv1.XPathGetRequest) (*mwanv1.XPathGetResponse, error) {
	return callTyped[*mwanv1.XPathGetResponse](ctx, r.c, mwn1.MethodXPathGet, req)
}

// XPathSet calls the XPathSet RPC.
func (r *RPC) XPathSet(ctx context.Context, req *mwanv1.XPathSetRequest) (*mwanv1.XPathSetResponse, error) {
	return callTyped[*mwanv1.XPathSetResponse](ctx, r.c, mwn1.MethodXPathSet, req)
}

// XPathDelete calls the XPathDelete RPC.
func (r *RPC) XPathDelete(ctx context.Context, req *mwanv1.XPathDeleteRequest) (*mwanv1.XPathDeleteResponse, error) {
	return callTyped[*mwanv1.XPathDeleteResponse](ctx, r.c, mwn1.MethodXPathDelete, req)
}

// StripGatewayV6 calls the StripGatewayV6 RPC.
func (r *RPC) StripGatewayV6(ctx context.Context, req *mwanv1.StripGatewayV6Request) (*mwanv1.StripGatewayV6Response, error) {
	return callTyped[*mwanv1.StripGatewayV6Response](ctx, r.c, mwn1.MethodStripGatewayV6, req)
}

// InjectGatewayV6 calls the InjectGatewayV6 RPC.
func (r *RPC) InjectGatewayV6(ctx context.Context, req *mwanv1.InjectGatewayV6Request) (*mwanv1.InjectGatewayV6Response, error) {
	return callTyped[*mwanv1.InjectGatewayV6Response](ctx, r.c, mwn1.MethodInjectGatewayV6, req)
}

// DeployStatus calls the DeployStatus RPC.
func (r *RPC) DeployStatus(ctx context.Context, req *mwanv1.DeployStatusRequest) (*mwanv1.DeployStatusResponse, error) {
	return callTyped[*mwanv1.DeployStatusResponse](ctx, r.c, mwn1.MethodDeployStatus, req)
}

// Revert calls the Revert RPC.
func (r *RPC) Revert(ctx context.Context, req *mwanv1.RevertRequest) (*mwanv1.RevertResponse, error) {
	return callTyped[*mwanv1.RevertResponse](ctx, r.c, mwn1.MethodRevert, req)
}

// Reset calls the Reset RPC. The daemon handles this inline on the
// reader goroutine and never queues it through the worker pool, so
// the call succeeds even when the worker pool is saturated.
func (r *RPC) Reset(ctx context.Context, req *mwanv1.ResetRequest) (*mwanv1.ResetResponse, error) {
	return callTyped[*mwanv1.ResetResponse](ctx, r.c, mwn1.MethodReset, req)
}

// DeployStream is the client side of the Deploy streaming RPC.
type DeployStream struct {
	send      func(proto.Message) error
	closeSend chan struct{}
	final     chan deployFinalResult
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	completed bool
}

type deployFinalResult struct {
	resp *mwanv1.DeployResponse
	err  error
}

// Send writes one application-level deploy chunk to the stream.
func (s *DeployStream) Send(chunk *mwanv1.Chunk) error {
	if s.send == nil {
		return errors.New("opnsense: Deploy stream not started")
	}
	s.mu.Lock()
	closed := s.closed
	completed := s.completed
	s.mu.Unlock()
	if closed || completed {
		return errors.New("opnsense: Deploy stream closed")
	}
	return s.send(chunk)
}

// CloseAndRecv closes the send side and waits for the Deploy response.
func (s *DeployStream) CloseAndRecv() (*mwanv1.DeployResponse, error) {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.closeSend)
	}
	s.mu.Unlock()
	res, ok := <-s.final
	s.mu.Lock()
	s.completed = true
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("opnsense: Deploy stream finalized without result")
	}
	return res.resp, res.err
}

// Cancel cancels an open Deploy stream without sending the final frame.
func (s *DeployStream) Cancel() {
	s.mu.Lock()
	if s.completed {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	s.closed = true
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Deploy opens the Deploy client-streaming RPC.
func (r *RPC) Deploy(ctx context.Context) (*DeployStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	stream := &DeployStream{
		send:      nil,
		closeSend: make(chan struct{}),
		final:     make(chan deployFinalResult, 1),
		cancel:    cancel,
		mu:        sync.Mutex{},
		closed:    false,
		completed: false,
	}
	sendReady := make(chan func(proto.Message) error, 1)

	go func() {
		defer func() {
			if recoverErr := recover(); recoverErr != nil {
				err := fmt.Errorf("panic: %v", recoverErr)
				r.c.log.ErrorContext(streamCtx, "opnsense: deploy stream panic recovered", slog.Any("err", err))
				stream.final <- deployFinalResult{resp: nil, err: err}
				close(stream.final)
			}
		}()
		resp, err := r.c.CallStream(streamCtx, mwn1.MethodDeploy,
			func(send func(proto.Message) error) error {
				sendReady <- send
				close(sendReady)
				select {
				case <-stream.closeSend:
					return nil
				case <-streamCtx.Done():
					return streamCtx.Err()
				}
			})
		typed, _ := resp.(*mwanv1.DeployResponse)
		stream.final <- deployFinalResult{resp: typed, err: err}
		close(stream.final)
	}()

	send, ok := <-sendReady
	if !ok {
		res := <-stream.final
		return nil, res.err
	}
	stream.send = send
	return stream, nil
}
