package main

import (
	"context"
	"fmt"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// proxyServer implements mwanv1.MWANOPNsenseServiceServer by
// forwarding every RPC to the persistent upstream gRPC client.
//
// HTTP/2 stream multiplex on the upstream ClientConn handles
// concurrent RPCs from many local probes. We add no framing,
// no retry, no caching: the bridge is a transparent forwarder.
type proxyServer struct {
	mwanv1.UnimplementedMWANOPNsenseServiceServer

	upstream mwanv1.MWANOPNsenseServiceClient
}

func (p *proxyServer) Version(ctx context.Context, req *mwanv1.VersionRequest) (*mwanv1.VersionResponse, error) {
	resp, err := p.upstream.Version(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy Version: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) Exec(ctx context.Context, req *mwanv1.ExecRequest) (*mwanv1.ExecResponse, error) {
	resp, err := p.upstream.Exec(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy Exec: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) ReadConfigXML(ctx context.Context, req *mwanv1.ReadConfigXMLRequest) (*mwanv1.ReadConfigXMLResponse, error) {
	resp, err := p.upstream.ReadConfigXML(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy ReadConfigXML: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) WriteConfigXML(ctx context.Context, req *mwanv1.WriteConfigXMLRequest) (*mwanv1.WriteConfigXMLResponse, error) {
	resp, err := p.upstream.WriteConfigXML(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy WriteConfigXML: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) BackupConfigXML(ctx context.Context, req *mwanv1.BackupConfigXMLRequest) (*mwanv1.BackupConfigXMLResponse, error) {
	resp, err := p.upstream.BackupConfigXML(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy BackupConfigXML: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) XPathGet(ctx context.Context, req *mwanv1.XPathGetRequest) (*mwanv1.XPathGetResponse, error) {
	resp, err := p.upstream.XPathGet(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy XPathGet: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) XPathSet(ctx context.Context, req *mwanv1.XPathSetRequest) (*mwanv1.XPathSetResponse, error) {
	resp, err := p.upstream.XPathSet(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy XPathSet: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) XPathDelete(ctx context.Context, req *mwanv1.XPathDeleteRequest) (*mwanv1.XPathDeleteResponse, error) {
	resp, err := p.upstream.XPathDelete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy XPathDelete: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) StripGatewayV6(ctx context.Context, req *mwanv1.StripGatewayV6Request) (*mwanv1.StripGatewayV6Response, error) {
	resp, err := p.upstream.StripGatewayV6(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy StripGatewayV6: %w", err)
	}
	return resp, nil
}

func (p *proxyServer) InjectGatewayV6(ctx context.Context, req *mwanv1.InjectGatewayV6Request) (*mwanv1.InjectGatewayV6Response, error) {
	resp, err := p.upstream.InjectGatewayV6(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("proxy InjectGatewayV6: %w", err)
	}
	return resp, nil
}
