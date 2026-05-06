package opnsenseclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
)

// RPC is the typed RPC surface returned by [Client.RPC]. It mirrors
// the unary methods of mwanv1.MWANOPNsenseServiceClient so existing
// call sites translate 1:1 onto MWN1 round-trips.
//
// Deploy is exposed via [RPC.Deploy] which returns a small streamer
// that batches Chunk messages into one MWN1 corr_id.
type RPC struct {
	c *Client
}

// RPC returns the typed RPC wrapper.
func (c *Client) RPC() *RPC { return &RPC{c: c} }

// callTyped performs one unary Call and casts the result to the
// declared response type. The cast failure is surfaced as a typed
// programming-bug error rather than a panic.
func callTyped[Resp proto.Message](ctx context.Context, c *Client,
	methodID uint16, req proto.Message,
) (Resp, error) {
	var zero Resp
	resp, err := c.Call(ctx, methodID, req)
	if err != nil {
		// The underlying Call already logged the error at WARN; we
		// just propagate it up to the typed caller.
		return zero, err
	}
	typed, ok := resp.(Resp)
	if !ok {
		c.log.WarnContext(ctx, "opnsenseclient: typed cast failed",
			slog.Int("method_id", int(methodID)), slog.String("got_type", fmt.Sprintf("%T", resp)))
		return zero, fmt.Errorf("%w: got %T for method %d", ErrUnexpectedResponseType, resp, methodID)
	}
	return typed, nil
}

// Version performs the Version RPC.
func (r *RPC) Version(ctx context.Context, req *mwanv1.VersionRequest) (*mwanv1.VersionResponse, error) {
	return callTyped[*mwanv1.VersionResponse](ctx, r.c, mwn1.MethodVersion, req)
}

// Exec performs the Exec RPC.
func (r *RPC) Exec(ctx context.Context, req *mwanv1.ExecRequest) (*mwanv1.ExecResponse, error) {
	return callTyped[*mwanv1.ExecResponse](ctx, r.c, mwn1.MethodExec, req)
}

// ReadConfigXML performs the ReadConfigXML RPC.
func (r *RPC) ReadConfigXML(ctx context.Context, req *mwanv1.ReadConfigXMLRequest) (*mwanv1.ReadConfigXMLResponse, error) {
	return callTyped[*mwanv1.ReadConfigXMLResponse](ctx, r.c, mwn1.MethodReadConfigXML, req)
}

// WriteConfigXML performs the WriteConfigXML RPC.
func (r *RPC) WriteConfigXML(ctx context.Context, req *mwanv1.WriteConfigXMLRequest) (*mwanv1.WriteConfigXMLResponse, error) {
	return callTyped[*mwanv1.WriteConfigXMLResponse](ctx, r.c, mwn1.MethodWriteConfigXML, req)
}

// BackupConfigXML performs the BackupConfigXML RPC.
func (r *RPC) BackupConfigXML(ctx context.Context, req *mwanv1.BackupConfigXMLRequest) (*mwanv1.BackupConfigXMLResponse, error) {
	return callTyped[*mwanv1.BackupConfigXMLResponse](ctx, r.c, mwn1.MethodBackupConfigXML, req)
}

// XPathGet performs the XPathGet RPC.
func (r *RPC) XPathGet(ctx context.Context, req *mwanv1.XPathGetRequest) (*mwanv1.XPathGetResponse, error) {
	return callTyped[*mwanv1.XPathGetResponse](ctx, r.c, mwn1.MethodXPathGet, req)
}

// XPathSet performs the XPathSet RPC.
func (r *RPC) XPathSet(ctx context.Context, req *mwanv1.XPathSetRequest) (*mwanv1.XPathSetResponse, error) {
	return callTyped[*mwanv1.XPathSetResponse](ctx, r.c, mwn1.MethodXPathSet, req)
}

// XPathDelete performs the XPathDelete RPC.
func (r *RPC) XPathDelete(ctx context.Context, req *mwanv1.XPathDeleteRequest) (*mwanv1.XPathDeleteResponse, error) {
	return callTyped[*mwanv1.XPathDeleteResponse](ctx, r.c, mwn1.MethodXPathDelete, req)
}

// StripGatewayV6 performs the StripGatewayV6 RPC.
func (r *RPC) StripGatewayV6(ctx context.Context, req *mwanv1.StripGatewayV6Request) (*mwanv1.StripGatewayV6Response, error) {
	return callTyped[*mwanv1.StripGatewayV6Response](ctx, r.c, mwn1.MethodStripGatewayV6, req)
}

// InjectGatewayV6 performs the InjectGatewayV6 RPC.
func (r *RPC) InjectGatewayV6(ctx context.Context, req *mwanv1.InjectGatewayV6Request) (*mwanv1.InjectGatewayV6Response, error) {
	return callTyped[*mwanv1.InjectGatewayV6Response](ctx, r.c, mwn1.MethodInjectGatewayV6, req)
}

// DeployStatus performs the DeployStatus RPC.
func (r *RPC) DeployStatus(ctx context.Context, req *mwanv1.DeployStatusRequest) (*mwanv1.DeployStatusResponse, error) {
	return callTyped[*mwanv1.DeployStatusResponse](ctx, r.c, mwn1.MethodDeployStatus, req)
}

// Revert performs the Revert RPC.
func (r *RPC) Revert(ctx context.Context, req *mwanv1.RevertRequest) (*mwanv1.RevertResponse, error) {
	return callTyped[*mwanv1.RevertResponse](ctx, r.c, mwn1.MethodRevert, req)
}

// DeployStream is the client-streaming wrapper for the Deploy RPC.
// Send each Chunk via [DeployStream.Send]; finish with
// [DeployStream.CloseAndRecv].
type DeployStream struct {
	send      func(proto.Message) error
	closeSend chan struct{}
	final     chan deployFinalResult
	closed    bool
}

type deployFinalResult struct {
	resp *mwanv1.DeployResponse
	err  error
}

// Send writes one Chunk frame to the server.
func (s *DeployStream) Send(chunk *mwanv1.Chunk) error {
	if s.send == nil {
		return errors.New("opnsenseclient: Deploy stream not started")
	}
	return s.send(chunk)
}

// CloseAndRecv signals end-of-stream to the server and returns the
// terminal DeployResponse. Must be called exactly once.
func (s *DeployStream) CloseAndRecv() (*mwanv1.DeployResponse, error) {
	if !s.closed {
		s.closed = true
		close(s.closeSend)
	}
	res, ok := <-s.final
	if !ok {
		return nil, errors.New("opnsenseclient: Deploy stream finalized without result")
	}
	return res.resp, res.err
}

// Deploy opens a Deploy stream. The implementation runs
// CallClientStream in a goroutine so callers can interleave Send
// invocations with their own producer logic, then CloseAndRecv to
// flush the terminator and read the response.
func (r *RPC) Deploy(ctx context.Context) (*DeployStream, error) {
	stream := &DeployStream{
		send:      nil,
		closeSend: make(chan struct{}),
		final:     make(chan deployFinalResult, 1),
		closed:    false,
	}
	sendReady := make(chan func(proto.Message) error, 1)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				r.c.log.ErrorContext(ctx, "opnsenseclient: Deploy goroutine panicked",
					slog.Any("err", fmt.Errorf("panic: %v", rec)))
				stream.final <- deployFinalResult{
					resp: nil,
					err:  fmt.Errorf("opnsenseclient: Deploy panic: %v", rec),
				}
				close(stream.final)
			}
		}()
		resp, err := r.c.CallClientStream(ctx, mwn1.MethodDeploy,
			func(send func(proto.Message) error) error {
				sendReady <- send
				close(sendReady)
				<-stream.closeSend
				return nil
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
