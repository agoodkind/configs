package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsenseclient"
)

// peerString renders a [peer.Peer] as a log-friendly address, or "" when
// the peer info is missing (e.g. non-gRPC invocation).
func peerString(p *peer.Peer) string {
	if p == nil || p.Addr == nil {
		return ""
	}
	return p.Addr.String()
}

// proxyServer implements mwanv1.MWANOPNsenseServiceServer by translating
// every gRPC RPC into one MWN1 round-trip on the persistent
// [opnsenseclient.Client].
//
// One probe -> one local gRPC handler -> one [Client.Call] -> one MWN1
// frame pair (request, response) sharing a CorrID. Concurrent probes
// translate into concurrent calls multiplexed on the single MWN1
// transport via monotonic CorrID assignment in the client.
//
// The post-deploy heartbeat / mark-healthy / revert flow has been
// stripped from this version; it becomes a probe-side concern, or a
// follow-up reintroduction once the MWN1 transport is stable on
// production. See the brief in MWAN-95 for the rationale.
type proxyServer struct {
	mwanv1.UnimplementedMWANOPNsenseServiceServer

	upstream *opnsenseclient.RPC
	log      *slog.Logger
}

// newProxyServer wires the bridge proxy to the persistent MWN1 client.
func newProxyServer(upstream *opnsenseclient.RPC, log *slog.Logger) *proxyServer {
	if log == nil {
		log = slog.Default()
	}
	return &proxyServer{
		UnimplementedMWANOPNsenseServiceServer: mwanv1.UnimplementedMWANOPNsenseServiceServer{},
		upstream:                               upstream,
		log:                                    log,
	}
}

// Version forwards the Version RPC.
func (p *proxyServer) Version(ctx context.Context, req *mwanv1.VersionRequest) (*mwanv1.VersionResponse, error) {
	resp, err := p.upstream.Version(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy Version: %w", err)
	}
	return resp, nil
}

// Exec forwards the Exec RPC.
func (p *proxyServer) Exec(ctx context.Context, req *mwanv1.ExecRequest) (*mwanv1.ExecResponse, error) {
	resp, err := p.upstream.Exec(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy Exec: %w", err)
	}
	return resp, nil
}

// ReadConfigXML forwards the ReadConfigXML RPC.
func (p *proxyServer) ReadConfigXML(ctx context.Context, req *mwanv1.ReadConfigXMLRequest) (*mwanv1.ReadConfigXMLResponse, error) {
	resp, err := p.upstream.ReadConfigXML(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy ReadConfigXML: %w", err)
	}
	return resp, nil
}

// WriteConfigXML forwards the WriteConfigXML RPC.
func (p *proxyServer) WriteConfigXML(ctx context.Context, req *mwanv1.WriteConfigXMLRequest) (*mwanv1.WriteConfigXMLResponse, error) {
	resp, err := p.upstream.WriteConfigXML(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy WriteConfigXML: %w", err)
	}
	return resp, nil
}

// BackupConfigXML forwards the BackupConfigXML RPC.
func (p *proxyServer) BackupConfigXML(ctx context.Context, req *mwanv1.BackupConfigXMLRequest) (*mwanv1.BackupConfigXMLResponse, error) {
	resp, err := p.upstream.BackupConfigXML(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy BackupConfigXML: %w", err)
	}
	return resp, nil
}

// XPathGet forwards the XPathGet RPC.
func (p *proxyServer) XPathGet(ctx context.Context, req *mwanv1.XPathGetRequest) (*mwanv1.XPathGetResponse, error) {
	resp, err := p.upstream.XPathGet(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy XPathGet: %w", err)
	}
	return resp, nil
}

// XPathSet forwards the XPathSet RPC.
func (p *proxyServer) XPathSet(ctx context.Context, req *mwanv1.XPathSetRequest) (*mwanv1.XPathSetResponse, error) {
	resp, err := p.upstream.XPathSet(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy XPathSet: %w", err)
	}
	return resp, nil
}

// XPathDelete forwards the XPathDelete RPC.
func (p *proxyServer) XPathDelete(ctx context.Context, req *mwanv1.XPathDeleteRequest) (*mwanv1.XPathDeleteResponse, error) {
	resp, err := p.upstream.XPathDelete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy XPathDelete: %w", err)
	}
	return resp, nil
}

// StripGatewayV6 forwards the StripGatewayV6 RPC.
func (p *proxyServer) StripGatewayV6(ctx context.Context, req *mwanv1.StripGatewayV6Request) (*mwanv1.StripGatewayV6Response, error) {
	resp, err := p.upstream.StripGatewayV6(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy StripGatewayV6: %w", err)
	}
	return resp, nil
}

// InjectGatewayV6 forwards the InjectGatewayV6 RPC.
func (p *proxyServer) InjectGatewayV6(ctx context.Context, req *mwanv1.InjectGatewayV6Request) (*mwanv1.InjectGatewayV6Response, error) {
	resp, err := p.upstream.InjectGatewayV6(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy InjectGatewayV6: %w", err)
	}
	return resp, nil
}

// DeployStatus forwards the DeployStatus RPC.
func (p *proxyServer) DeployStatus(ctx context.Context, req *mwanv1.DeployStatusRequest) (*mwanv1.DeployStatusResponse, error) {
	pi, _ := peer.FromContext(ctx)
	resp, err := p.upstream.DeployStatus(ctx, req)
	if err != nil {
		p.log.ErrorContext(ctx, "proxy DeployStatus: upstream error", "peer", peerString(pi), "err", err)
		return nil, fmt.Errorf("proxy DeployStatus: %w", err)
	}
	return resp, nil
}

// Revert forwards the Revert RPC.
func (p *proxyServer) Revert(ctx context.Context, req *mwanv1.RevertRequest) (*mwanv1.RevertResponse, error) {
	pi, _ := peer.FromContext(ctx)
	pa := peerString(pi)
	p.log.InfoContext(ctx, "proxy Revert: forwarding", "peer", pa)
	resp, err := p.upstream.Revert(ctx, req)
	if err != nil {
		p.log.ErrorContext(ctx, "proxy Revert: upstream error", "peer", pa, "err", err)
		return nil, fmt.Errorf("proxy Revert: %w", err)
	}
	return resp, nil
}

// Deploy bridges the streaming Deploy RPC: it forwards every Chunk from
// the downstream gRPC client to the upstream MWN1 stream and relays the
// final DeployResponse back. The relay loop is intentionally inline so
// each future client-streaming RPC can copy the same shape.
func (p *proxyServer) Deploy(srv grpc.ClientStreamingServer[mwanv1.Chunk, mwanv1.DeployResponse]) error {
	ctx := srv.Context()
	pi, _ := peer.FromContext(ctx)
	pa := peerString(pi)
	p.log.InfoContext(ctx, "proxy Deploy: stream begin", "peer", pa)

	stream, err := p.upstream.Deploy(ctx)
	if err != nil {
		p.log.ErrorContext(ctx, "proxy Deploy: upstream open failed", "peer", pa, "err", err)
		return fmt.Errorf("proxy Deploy: open upstream: %w", err)
	}

	relayed := 0
	for {
		msg, recvErr := srv.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			p.log.ErrorContext(ctx, "proxy Deploy: recv from caller failed",
				"peer", pa, "err", recvErr, "relayed", relayed)
			return fmt.Errorf("proxy Deploy: recv from caller: %w", recvErr)
		}
		if sendErr := stream.Send(msg); sendErr != nil {
			p.log.ErrorContext(ctx, "proxy Deploy: send to upstream failed",
				"peer", pa, "err", sendErr, "relayed", relayed)
			return fmt.Errorf("proxy Deploy: send to upstream: %w", sendErr)
		}
		relayed++
	}
	resp, closeErr := stream.CloseAndRecv()
	if closeErr != nil {
		p.log.ErrorContext(ctx, "proxy Deploy: close upstream failed",
			"peer", pa, "err", closeErr, "relayed", relayed)
		return fmt.Errorf("proxy Deploy: close upstream: %w", closeErr)
	}
	p.log.InfoContext(ctx, "proxy Deploy: relay complete",
		"peer", pa,
		"chunks_relayed", relayed,
		"staged_sha256", resp.GetStagedSha256(),
		"previous_path", resp.GetPreviousPath(),
		"re_exec_started", resp.GetReExecStarted())
	if sendErr := srv.SendAndClose(resp); sendErr != nil {
		p.log.ErrorContext(ctx, "proxy Deploy: send response failed", "peer", pa, "err", sendErr)
		return fmt.Errorf("proxy Deploy: send response: %w", sendErr)
	}
	return nil
}
