// Package opnsenseclient is the host-side gRPC client for the
// mwan-opnsense daemon running inside the OPNsense VM.
//
// The same package is used by both:
//   - operational tooling (`mwan opnsense-probe` subcommand) for ad
//     hoc dialing of the daemon
//   - the bridge daemon (cmd/mwan-opnsense-host) which forwards every
//     RPC from local probes onto a single persistent ClientConn
//
// Transport: only the OOB virtio-serial unix socket on the Proxmox
// host is supported. Target is always:
//
//	unix:///var/run/qemu-server/<vmid>.mwanrpc
//
// gRPC's resolver dispatches `unix:///` natively. There is no TLS
// and no application-level authentication; access control is the
// unix socket permissions (root-only on the Proxmox host).
//
// Reconnect strategy: this package leans on gRPC-go's built-in
// connection state machine (IDLE -> CONNECTING -> READY ->
// TRANSIENT_FAILURE) and its native exponential backoff configured
// via [grpc.WithConnectParams]. There is no application-level
// handshake-probe loop. Calls issued during TRANSIENT_FAILURE return
// codes.Unavailable when the call carries a deadline; callers that
// want to block until the channel is READY should use
// [Client.WaitForReady] or attach the [grpc.WaitForReady] call option.
package opnsenseclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// Deploy ships full daemon binaries (~17 MiB). Bump send and recv
// limits to 64 MiB so probe -> bridge -> daemon upload works.
const maxMsgBytes = 64 * 1024 * 1024

// Reconnect backoff parameters. Tuned for virtio-serial RTTs which
// are 100-500ms per round trip and can stall for several seconds
// under guest load. Matches grpc/doc/connection-backoff.md guidance.
const (
	reconnectBaseDelay  = 1 * time.Second
	reconnectMultiplier = 1.6
	reconnectJitter     = 0.2
	reconnectMaxDelay   = 30 * time.Second
	minConnectTimeout   = 10 * time.Second
)

// Config is the per-connection configuration.
type Config struct {
	// Target is the gRPC target string. Must be unix:///path/to/socket.
	Target string
	// DialTimeout caps the OPTIONAL initial-readiness wait performed
	// when [Client.WaitForReady] is called explicitly. It does NOT bound
	// [Dial] itself: gRPC's NewClient does not block on connect, so
	// [Dial] returns immediately and the channel transitions through
	// CONNECTING / TRANSIENT_FAILURE / READY in the background.
	//
	// Zero or negative means no waiting. Callers that want to defer the
	// readiness wait entirely should leave this zero and let RPCs block
	// per-call (with whatever deadline the call carries) via
	// [grpc.WaitForReady] semantics.
	DialTimeout time.Duration
}

// Client is a thin gRPC wrapper exposing the mwan-opnsense RPC
// surface. Construct via [Dial]; close with [Client.Close].
type Client struct {
	conn *grpc.ClientConn
	rpc  mwanv1.MWANOPNsenseServiceClient
}

// Dial constructs a gRPC ClientConn with auto-reconnect backoff and
// returns a [Client]. It does NOT block on initial connect: gRPC's
// NewClient returns a channel in IDLE state and the channel begins
// connecting on the first RPC (or eagerly when [grpc.ClientConn.Connect]
// is invoked, which Dial does internally so the state machine starts
// immediately).
//
// Subsequent upstream loss (daemon restart, virtio-serial chardev reset,
// transient errors) is handled by gRPC's built-in state machine. Calls
// during TRANSIENT_FAILURE return codes.Unavailable when the call
// carries a deadline; otherwise gRPC's per-call wait blocks until
// READY.
//
// Caller must Close.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Target == "" {
		return nil, errors.New("opnsenseclient: empty target")
	}

	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(maxMsgBytes),
			grpc.MaxCallRecvMsgSize(maxMsgBytes),
		),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  reconnectBaseDelay,
				Multiplier: reconnectMultiplier,
				Jitter:     reconnectJitter,
				MaxDelay:   reconnectMaxDelay,
			},
			MinConnectTimeout: minConnectTimeout,
		}),
	}
	if unixSocketPath, ok := unixTargetPath(cfg.Target); ok {
		dialOptions = append(dialOptions,
			grpc.WithContextDialer(unixContextDialer(unixSocketPath)))
	}

	conn, err := grpc.NewClient(cfg.Target, dialOptions...)
	if err != nil {
		slog.ErrorContext(ctx, "opnsenseclient: create client failed",
			"target", cfg.Target,
			"err", err)
		return nil, fmt.Errorf("opnsenseclient: dial %q: %w", cfg.Target, err)
	}

	// Kick the state machine out of IDLE so reconnect attempts begin
	// before the first RPC. This is what makes "the bridge starts up
	// before the daemon is ready" recover quickly: the channel is
	// already CONNECTING when probes arrive.
	conn.Connect()

	slog.DebugContext(ctx, "opnsenseclient: client created",
		"target", cfg.Target,
		"reconnect_base_delay_ms", reconnectBaseDelay.Milliseconds(),
		"reconnect_max_delay_ms", reconnectMaxDelay.Milliseconds(),
		"min_connect_timeout_ms", minConnectTimeout.Milliseconds())

	return &Client{
		conn: conn,
		rpc:  mwanv1.NewMWANOPNsenseServiceClient(conn),
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// RPC returns the underlying typed gRPC client. Callers are expected
// to use this for any of the service's RPCs.
func (c *Client) RPC() mwanv1.MWANOPNsenseServiceClient {
	return c.rpc
}

// WaitForReady blocks until the underlying channel reaches READY or
// the supplied context is done. It issues a no-op Version RPC tagged
// with [grpc.WaitForReady] which gRPC will hold open across
// CONNECTING / TRANSIENT_FAILURE transitions. Returns nil on success,
// the wrapped Version error on RPC failure, or ctx.Err() on
// cancellation/deadline.
//
// Callers that pass a finite deadline get the standard gRPC behaviour:
// codes.DeadlineExceeded if the channel never reaches READY in time.
//
// This is purely optional. The bridge does not need to call it; gRPC
// will block per-RPC for as long as the call's own context allows.
func (c *Client) WaitForReady(ctx context.Context) error {
	_, err := c.rpc.Version(ctx, &mwanv1.VersionRequest{}, grpc.WaitForReady(true))
	if err != nil {
		slog.WarnContext(ctx, "opnsenseclient: wait for ready failed",
			"err", err)
		return fmt.Errorf("opnsenseclient: wait for ready: %w", err)
	}
	return nil
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

func unixContextDialer(socketPath string) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}
}
