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
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

const (
	// opnsenseHandshakeAttemptTimeout is the per-attempt budget for one
	// dial+handshake. Over virtio-serial each attempt is several
	// host->chardev->virtio->guest round trips: client preface, server
	// SETTINGS, client SETTINGS, ACKs, and the Version RPC. Each RTT
	// is 100-500ms; the full sequence needs ~5-10s under load. 10s is
	// the empirical floor under which the bridge fails to handshake.
	opnsenseHandshakeAttemptTimeout = 10 * time.Second
	opnsenseReconnectDelay          = 2 * time.Second
)

// Config is the per-connection configuration.
type Config struct {
	// Target is the gRPC target string. Must be unix:///path/to/socket.
	Target string
	// DialTimeout caps the time spent waiting on the initial
	// handshake. RPCs themselves use the per-call context.
	DialTimeout time.Duration
	// Clock supplies wall time for retry accounting.
	Clock Clock
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
	activeClock := clockOrReal(cfg.Clock)
	remainingTimeout := dialTimeout
	startedAt := activeClock.Now()
	var lastErr error
	attempts := 0
	for {
		attempts++
		attempt := attempts
		attemptTimeout := min(remainingTimeout, opnsenseHandshakeAttemptTimeout)
		slog.DebugContext(ctx, "opnsenseclient: handshake attempt starting",
			"target", cfg.Target,
			"attempt", attempt,
			"attempt_timeout_ms", attemptTimeout.Milliseconds(),
			"remaining_timeout_ms", remainingTimeout.Milliseconds())

		attemptStartedAt := activeClock.Now()
		client, err := dialOnce(ctx, cfg.Target, attemptTimeout)
		if err == nil {
			slog.DebugContext(ctx, "opnsenseclient: handshake succeeded",
				"target", cfg.Target,
				"attempt", attempt,
				"elapsed_ms", activeClock.Now().Sub(startedAt).Milliseconds())
			return client, nil
		}
		lastErr = err
		slog.WarnContext(ctx, "opnsenseclient: handshake attempt failed",
			"target", cfg.Target,
			"attempt", attempt,
			"attempt_timeout_ms", attemptTimeout.Milliseconds(),
			"attempt_elapsed_ms", activeClock.Now().Sub(attemptStartedAt).Milliseconds(),
			"err", err)

		remainingTimeout -= attemptTimeout
		if remainingTimeout <= opnsenseReconnectDelay {
			break
		}
		slog.DebugContext(ctx, "opnsenseclient: reconnect sleep starting",
			"target", cfg.Target,
			"attempt", attempt,
			"next_attempt", attempt+1,
			"reconnect_sleep_ms", opnsenseReconnectDelay.Milliseconds(),
			"remaining_timeout_ms", remainingTimeout.Milliseconds())
		timer := time.NewTimer(opnsenseReconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			slog.WarnContext(ctx, "opnsenseclient: handshake cancelled",
				"target", cfg.Target,
				"attempt", attempt,
				"elapsed_ms", activeClock.Now().Sub(startedAt).Milliseconds(),
				"err", ctx.Err())
			return nil, fmt.Errorf("opnsenseclient: handshake to %q: %w", cfg.Target, ctx.Err())
		case <-timer.C:
			remainingTimeout -= opnsenseReconnectDelay
		}
	}
	slog.ErrorContext(ctx, "opnsenseclient: handshake failed permanently",
		"target", cfg.Target,
		"attempts", attempts,
		"elapsed_ms", activeClock.Now().Sub(startedAt).Milliseconds(),
		"err", lastErr)
	return nil, fmt.Errorf(
		"opnsenseclient: handshake to %q failed after %d attempt(s): %w",
		cfg.Target,
		attempts,
		lastErr,
	)
}

func dialOnce(ctx context.Context, target string, dialTimeout time.Duration) (*Client, error) {
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if unixSocketPath, ok := unixTargetPath(target); ok {
		dialOptions = append(dialOptions,
			grpc.WithContextDialer(unixContextDialer(unixSocketPath)))
	}

	conn, err := grpc.NewClient(target, dialOptions...)
	if err != nil {
		slog.ErrorContext(ctx, "opnsenseclient: create client failed",
			"target", target,
			"err", err)
		return nil, fmt.Errorf("opnsenseclient: dial %q: %w", target, err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	cli := mwanv1.NewMWANOPNsenseServiceClient(conn)
	if _, err := cli.Version(probeCtx, &mwanv1.VersionRequest{}); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			slog.WarnContext(ctx, "opnsenseclient: close failed after handshake error",
				"target", target,
				"err", closeErr)
		}
		slog.WarnContext(ctx, "opnsenseclient: version handshake failed",
			"target", target,
			"err", err)
		return nil, fmt.Errorf("opnsenseclient: handshake to %q: %w", target, err)
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
