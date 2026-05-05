package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsenseclient"
)

const (
	defaultUpstreamTarget = "unix:///var/run/qemu-server/101.mwanrpc"
	defaultListenPath     = "/var/run/mwan-opnsense.sock"
	// defaultUpstreamReadyTimeout is zero by default: the bridge does
	// not block at startup waiting for the upstream daemon. gRPC's
	// state machine reconnects in the background and per-call
	// deadlines surface upstream unavailability to probes as
	// codes.Unavailable.
	defaultUpstreamReadyTimeout = 0
	// proxyMaxBytes matches opnsensesvc.maxDeployBytes so the bridge
	// can forward full daemon-binary Deploy payloads (~17 MiB today,
	// 64 MiB ceiling).
	proxyMaxBytes = 64 * 1024 * 1024
)

// serveFlags is the parsed CLI surface for the serve subcommand.
type serveFlags struct {
	upstream             string
	listenPath           string
	upstreamReadyTimeout time.Duration
}

// parseServeFlags parses argv for the serve subcommand. Returns the
// flags plus a boolean indicating whether the caller should exit (and
// the suggested exit code) so runServe stays linear.
func parseServeFlags(args []string) (serveFlags, int, bool) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	upstream := fs.String("upstream", defaultUpstreamTarget,
		"upstream gRPC target (qemu virtio-serial chardev unix socket)")
	listenPath := fs.String("listen", defaultListenPath,
		"local unix socket path to listen on")
	upstreamReadyTimeout := fs.Duration("upstream-ready-timeout",
		defaultUpstreamReadyTimeout,
		"OPTIONAL: if > 0, block at startup until the upstream channel "+
			"reaches READY or this timeout elapses. The bridge will "+
			"keep serving even if this wait times out; it just means "+
			"early probes see codes.Unavailable until the channel "+
			"reconnects. Default 0 (no wait).")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return serveFlags{}, 2, true
	}
	if *upstream == "" {
		fmt.Fprintln(os.Stderr, "serve: -upstream required")
		return serveFlags{}, 2, true
	}
	if *listenPath == "" {
		fmt.Fprintln(os.Stderr, "serve: -listen required")
		return serveFlags{}, 2, true
	}
	return serveFlags{
		upstream:             *upstream,
		listenPath:           *listenPath,
		upstreamReadyTimeout: *upstreamReadyTimeout,
	}, 0, false
}

// runServe starts the bridge: build the persistent upstream gRPC
// connection to the mwan-opnsense daemon inside the OPNsense VM, then
// run a gRPC server on a local unix socket that proxies all RPCs onto
// the persistent ClientConn.
//
// Lifecycle: the bridge process exits only on signal (SIGINT/SIGTERM)
// or fatal local errors (cannot bind the listen socket). It does NOT
// exit when the upstream is unreachable: gRPC's auto-reconnect handles
// transient and prolonged upstream loss transparently.
func runServe(args []string) int {
	cfg, exitCode, shouldExit := parseServeFlags(args)
	if shouldExit {
		return exitCode
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.InfoContext(ctx, "mwan-opnsense-host: building upstream client",
		"upstream", cfg.upstream,
		"upstream_ready_timeout", cfg.upstreamReadyTimeout.String())
	upstreamClient, err := opnsenseclient.Dial(ctx, opnsenseclient.Config{
		Target:      cfg.upstream,
		DialTimeout: cfg.upstreamReadyTimeout,
	})
	if err != nil {
		// Only structural target/option failures land here. Network
		// unreachability does NOT produce an error from Dial because
		// grpc.NewClient is non-blocking. Treating this as fatal is
		// correct: it indicates a programmer or config bug.
		slog.ErrorContext(ctx, "mwan-opnsense-host: upstream client construction failed",
			"upstream", cfg.upstream,
			"err", err)
		return 1
	}
	defer func() {
		if closeErr := upstreamClient.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "mwan-opnsense-host: upstream close failed",
				"upstream", cfg.upstream,
				"err", closeErr)
		}
	}()
	slog.InfoContext(ctx, "mwan-opnsense-host: upstream client ready (connecting in background)",
		"upstream", cfg.upstream)

	if cfg.upstreamReadyTimeout > 0 {
		waitForUpstream(ctx, upstreamClient, cfg.upstream, cfg.upstreamReadyTimeout)
	}

	listener, err := openLocalListener(ctx, cfg.listenPath)
	if err != nil {
		slog.ErrorContext(ctx, "mwan-opnsense-host: listen failed",
			"listen", cfg.listenPath,
			"err", err)
		return 1
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "mwan-opnsense-host: listener close failed",
				"listen", cfg.listenPath,
				"err", closeErr)
		}
	}()
	slog.InfoContext(ctx, "mwan-opnsense-host: listening",
		"listen", cfg.listenPath)

	gs := grpc.NewServer(grpc.MaxRecvMsgSize(proxyMaxBytes))
	mwanv1.RegisterMWANOPNsenseServiceServer(gs, newProxyServer(upstreamClient.RPC(), slog.Default()))
	return runServeLoop(ctx, gs, listener)
}

// runServeLoop runs gs.Serve in a goroutine and blocks until either
// the serve goroutine returns (success or error) or the parent context
// is cancelled by SIGINT/SIGTERM. Returns the appropriate exit code.
func runServeLoop(ctx context.Context, gs *grpc.Server, listener net.Listener) int {
	errCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ctx, "mwan-opnsense-host: serve goroutine panicked",
					"err", fmt.Errorf("panic: %v", r))
				errCh <- fmt.Errorf("serve goroutine panic: %v", r)
			}
		}()
		serveErr := gs.Serve(listener)
		if serveErr != nil {
			errCh <- fmt.Errorf("grpc serve: %w", serveErr)
			return
		}
		errCh <- nil
	}()

	select {
	case serveErr := <-errCh:
		if serveErr != nil {
			slog.ErrorContext(ctx, "mwan-opnsense-host: serve terminated",
				"err", serveErr)
			return 1
		}
		slog.InfoContext(ctx, "mwan-opnsense-host: serve returned cleanly")
		return 0
	case <-ctx.Done():
		slog.InfoContext(ctx, "mwan-opnsense-host: shutdown signal, stopping gRPC")
		gs.GracefulStop()
		<-errCh
		return 0
	}
}

// waitForUpstream blocks until the upstream channel reaches READY or
// the timeout elapses. A timeout is logged at WARN but does NOT abort
// the bridge: gRPC will keep reconnecting in the background and probes
// arriving in the meantime will get codes.Unavailable until the
// channel comes up.
func waitForUpstream(parent context.Context, c *opnsenseclient.Client, target string, timeout time.Duration) {
	waitCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	startedAt := time.Now()
	if err := c.WaitForReady(waitCtx); err != nil {
		slog.WarnContext(parent, "mwan-opnsense-host: upstream not ready before timeout; serving anyway",
			"upstream", target,
			"timeout", timeout.String(),
			"elapsed_ms", time.Since(startedAt).Milliseconds(),
			"err", err)
		return
	}
	slog.InfoContext(parent, "mwan-opnsense-host: upstream READY",
		"upstream", target,
		"elapsed_ms", time.Since(startedAt).Milliseconds())
}

// openLocalListener prepares a unix-socket listener at path. Removes
// any stale socket file from a previous run before binding. Sets mode
// 0660 so non-root callers cannot connect by default.
func openLocalListener(ctx context.Context, path string) (net.Listener, error) {
	slog.DebugContext(ctx, "openLocalListener", "path", path)
	removeErr := os.Remove(path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket %s: %w", path, removeErr)
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", path, err)
	}
	chmodErr := os.Chmod(path, 0o660)
	if chmodErr != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chmod %s: %w", path, chmodErr)
	}
	return listener, nil
}
