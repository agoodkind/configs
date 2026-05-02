// Package opnsenseclient is the host-side gRPC client for the
// mwan-opnsense daemon running inside the OPNsense VM.
//
// The same package is used by both:
//   - operational tooling (`mwan opnsense-probe` subcommand) for ad
//     hoc dialing of the daemon
//   - any future caller that wants the typed RPC surface
//
// Transport: only the OOB virtio-serial unix socket on the Proxmox
// host is supported. Target is always:
//
//	unix:///var/run/qemu-server/<vmid>.mwanrpc
//
// gRPC's resolver dispatches `unix:///` natively. There is no TLS
// and no application-level authentication; access control is the
// unix socket permissions (root-only on the Proxmox host).
package opnsenseclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// Config is the per-connection configuration.
type Config struct {
	// Target is the gRPC target string. Must be unix:///path/to/socket.
	Target string
	// DialTimeout caps the time spent waiting on the initial
	// handshake. RPCs themselves use the per-call context.
	DialTimeout time.Duration
}

// Client is a thin gRPC wrapper exposing the mwan-opnsense RPC
// surface. Construct via Dial; close with Close.
type Client struct {
	conn *grpc.ClientConn
	rpc  mwanv1.MWANOPNsenseServiceClient
}

// Dial opens a gRPC client connection to the configured target.
// Caller must Close.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Target == "" {
		return nil, errors.New("opnsenseclient: empty target")
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}

	conn, err := grpc.NewClient(cfg.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.ErrorContext(ctx, "opnsenseclient: dial failed", "target", cfg.Target, "err", err)
		return nil, fmt.Errorf("opnsenseclient: dial %q: %w", cfg.Target, err)
	}

	// Force a handshake within dialTimeout so callers get a fast
	// failure instead of a deadline-exceeded on the first RPC.
	probeCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	cli := mwanv1.NewMWANOPNsenseServiceClient(conn)
	if _, err := cli.Version(probeCtx, &mwanv1.VersionRequest{}); err != nil {
		_ = conn.Close()
		slog.ErrorContext(ctx, "opnsenseclient: handshake failed", "target", cfg.Target, "err", err)
		return nil, fmt.Errorf("opnsenseclient: handshake to %q: %w", cfg.Target, err)
	}

	return &Client{conn: conn, rpc: cli}, nil
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
