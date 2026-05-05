package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc/peer"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// peerString renders a [peer.Peer] as a log-friendly address, or "" when
// the peer info is missing (e.g. non-gRPC invocation).
func peerString(p *peer.Peer) string {
	if p == nil || p.Addr == nil {
		return ""
	}
	return p.Addr.String()
}

// proxyServer implements mwanv1.MWANOPNsenseServiceServer by
// forwarding every RPC to the persistent upstream gRPC client.
//
// HTTP/2 stream multiplex on the upstream ClientConn handles
// concurrent RPCs from many local probes. We add no framing,
// no retry, no caching: the bridge is a transparent forwarder.
//
// Exception: the Deploy RPC additionally arms a post-deploy heartbeat
// goroutine that probes Version() with exponential backoff until the
// new daemon image answers. On first success it calls
// DeployStatus(MARK_HEALTHY). On 60s budget exhaustion it calls Revert.
type proxyServer struct {
	mwanv1.UnimplementedMWANOPNsenseServiceServer

	upstream mwanv1.MWANOPNsenseServiceClient
	log      *slog.Logger

	heartbeatMu      sync.Mutex
	heartbeatRunning bool

	// heartbeatConfig carries timing parameters; defaulted in the
	// arming path. Tests override via newProxyServerWithHeartbeat.
	heartbeatBudget   time.Duration
	heartbeatInitial  time.Duration
	heartbeatMaxDelay time.Duration
}

// newProxyServer builds the bridge proxy with production defaults.
func newProxyServer(upstream mwanv1.MWANOPNsenseServiceClient, log *slog.Logger) *proxyServer {
	if log == nil {
		log = slog.Default()
	}
	return &proxyServer{
		UnimplementedMWANOPNsenseServiceServer: mwanv1.UnimplementedMWANOPNsenseServiceServer{},
		upstream:                               upstream,
		log:                                    log,
		heartbeatMu:                            sync.Mutex{},
		heartbeatRunning:                       false,
		heartbeatBudget:                        60 * time.Second,
		heartbeatInitial:                       100 * time.Millisecond,
		heartbeatMaxDelay:                      5 * time.Second,
	}
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

// Deploy forwards the deploy RPC and, on a successful re-exec
// announcement, arms a post-deploy heartbeat probe in a background
// goroutine. Any subsequent Deploy invocation while a heartbeat is
// already running will skip arming a second one. The heartbeat itself
// finalizes the deploy by calling MarkHealthy or Revert.
func (p *proxyServer) Deploy(ctx context.Context, req *mwanv1.DeployRequest) (*mwanv1.DeployResponse, error) {
	pi, _ := peer.FromContext(ctx)
	pa := peerString(pi)
	p.log.InfoContext(ctx, "proxy Deploy: forwarding",
		"peer", pa,
		"bytes", len(req.GetBinary()),
		"sha256_hex", req.GetSha256Hex(),
		"version_str", req.GetVersionStr())

	resp, err := p.upstream.Deploy(ctx, req)
	if err != nil {
		p.log.ErrorContext(ctx, "proxy Deploy: upstream error", "peer", pa, "err", err)
		return nil, fmt.Errorf("proxy Deploy: %w", err)
	}
	if resp.GetReExecStarted() {
		p.armHeartbeat(ctx)
	}
	return resp, nil
}

// DeployStatus forwards the status RPC. The bridge's heartbeat
// goroutine also calls this on the upstream daemon directly to mark
// the deploy healthy. External callers may use it for read-only
// status queries.
func (p *proxyServer) DeployStatus(ctx context.Context, req *mwanv1.DeployStatusRequest) (*mwanv1.DeployStatusResponse, error) {
	pi, _ := peer.FromContext(ctx)
	resp, err := p.upstream.DeployStatus(ctx, req)
	if err != nil {
		p.log.ErrorContext(ctx, "proxy DeployStatus: upstream error", "peer", peerString(pi), "err", err)
		return nil, fmt.Errorf("proxy DeployStatus: %w", err)
	}
	return resp, nil
}

// Revert forwards the revert RPC. Like Deploy, a successful re-exec
// announcement arms the heartbeat probe so the bridge can confirm the
// reverted binary is healthy.
func (p *proxyServer) Revert(ctx context.Context, req *mwanv1.RevertRequest) (*mwanv1.RevertResponse, error) {
	pi, _ := peer.FromContext(ctx)
	pa := peerString(pi)
	p.log.InfoContext(ctx, "proxy Revert: forwarding", "peer", pa)
	resp, err := p.upstream.Revert(ctx, req)
	if err != nil {
		p.log.ErrorContext(ctx, "proxy Revert: upstream error", "peer", pa, "err", err)
		return nil, fmt.Errorf("proxy Revert: %w", err)
	}
	if resp.GetReExecStarted() {
		p.armHeartbeat(ctx)
	}
	return resp, nil
}

// armHeartbeat starts the post-deploy probe goroutine if one is not
// already running. Safe to call concurrently. The heartbeat outlives
// the originating RPC, so we detach the parent context's cancellation
// via [context.WithoutCancel] while preserving its values (logger,
// tracing). A fresh timeout bounds the entire heartbeat lifecycle.
func (p *proxyServer) armHeartbeat(parent context.Context) {
	p.heartbeatMu.Lock()
	if p.heartbeatRunning {
		p.heartbeatMu.Unlock()
		p.log.WarnContext(parent, "heartbeat already armed; skipping second arm")
		return
	}
	p.heartbeatRunning = true
	p.heartbeatMu.Unlock()

	hbCtx, hbCancel := context.WithTimeout(
		context.WithoutCancel(parent),
		p.heartbeatBudget+10*time.Second,
	)
	go func() {
		defer hbCancel()
		defer func() {
			if r := recover(); r != nil {
				p.log.ErrorContext(hbCtx, "heartbeat goroutine panicked", "err", fmt.Errorf("panic: %v", r))
			}
		}()
		p.runHeartbeat(hbCtx)
	}()
}

// runHeartbeat probes Version() with exponential backoff until the
// upstream answers, then marks the deploy healthy. On budget
// exhaustion, it triggers a revert. The supplied ctx bounds the entire
// heartbeat lifecycle and is propagated to every upstream call.
//
// If triggerRevert reports re_exec_started, runHeartbeat re-arms a
// fresh heartbeat AFTER releasing the running flag, so the new
// heartbeat is not rejected by the "already armed" gate.
func (p *proxyServer) runHeartbeat(ctx context.Context) {
	rearm := false
	defer func() {
		p.heartbeatMu.Lock()
		p.heartbeatRunning = false
		p.heartbeatMu.Unlock()
		if rearm {
			p.armHeartbeat(ctx)
		}
	}()

	deadline := time.Now().Add(p.heartbeatBudget)
	delay := p.heartbeatInitial
	attempts := 0
	p.log.InfoContext(ctx, "heartbeat: armed",
		"budget", p.heartbeatBudget.String(),
		"initial_delay", p.heartbeatInitial.String(),
		"max_delay", p.heartbeatMaxDelay.String())

	healthy := false
	for time.Now().Before(deadline) {
		attempts++
		select {
		case <-ctx.Done():
			p.log.WarnContext(ctx, "heartbeat: ctx cancelled", "attempts", attempts, "err", ctx.Err())
			return
		case <-time.After(delay):
		}

		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := p.upstream.Version(probeCtx, &mwanv1.VersionRequest{})
		cancel()
		if err == nil {
			healthy = true
			break
		}
		p.log.DebugContext(ctx, "heartbeat: probe failed",
			"attempts", attempts,
			"err", err,
			"next_delay", delay.String())

		delay *= 2
		if delay > p.heartbeatMaxDelay {
			delay = p.heartbeatMaxDelay
		}
	}

	if healthy {
		p.log.InfoContext(ctx, "heartbeat: upstream healthy",
			"attempts", attempts,
			"elapsed", time.Until(deadline).String())
		p.markDeployHealthy(ctx)
		return
	}

	p.log.ErrorContext(ctx, "heartbeat: budget exhausted; triggering revert",
		"attempts", attempts,
		"err", fmt.Errorf("heartbeat budget %s exhausted without healthy upstream response", p.heartbeatBudget))
	rearm = p.triggerRevert(ctx)
}

// markDeployHealthy calls DeployStatus(MARK_HEALTHY) on the upstream.
// The supplied ctx is the heartbeat's lifecycle context; we narrow it
// with a per-call timeout so a hung upstream doesn't pin the goroutine.
func (p *proxyServer) markDeployHealthy(ctx context.Context) {
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := p.upstream.DeployStatus(callCtx, &mwanv1.DeployStatusRequest{
		Mark: mwanv1.DeployStatusRequest_MARK_HEALTHY,
	})
	if err != nil {
		p.log.ErrorContext(ctx, "heartbeat: mark healthy failed", "err", err)
		return
	}
	p.log.InfoContext(ctx, "heartbeat: deploy marked healthy",
		"active_sha256", resp.GetActiveSha256(),
		"previous_sha256", resp.GetPreviousSha256(),
		"deployed_at", resp.GetDeployedAt())
}

// triggerRevert calls Revert on the upstream and returns whether the
// caller should re-arm a fresh heartbeat (true when the upstream
// reports a re-exec was started). The actual re-arm happens in
// runHeartbeat's deferred cleanup so the running flag is released
// before a new heartbeat is armed.
func (p *proxyServer) triggerRevert(ctx context.Context) bool {
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := p.upstream.Revert(callCtx, &mwanv1.RevertRequest{})
	if err != nil {
		p.log.ErrorContext(ctx, "heartbeat: revert call failed", "err", err)
		return false
	}
	p.log.WarnContext(ctx, "heartbeat: revert issued",
		"reverted_to_sha256", resp.GetRevertedToSha256(),
		"re_exec_started", resp.GetReExecStarted())
	return resp.GetReExecStarted()
}
