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

	"google.golang.org/grpc"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsenseclient"
)

const (
	defaultUpstreamTarget = "unix:///var/run/qemu-server/101.mwanrpc"
	defaultListenPath     = "/var/run/mwan-opnsense.sock"
	// proxyMaxBytes matches opnsensesvc.maxDeployBytes so the bridge
	// can forward full daemon-binary Deploy payloads (~17 MiB today,
	// 64 MiB ceiling). MWN1 carries each Chunk in a single frame whose
	// payload is bounded by mwn1.MaxPayload (64 KiB); chunkedstream
	// already splits Deploy bodies into smaller chunks.
	proxyMaxBytes = 64 * 1024 * 1024
)

// serveFlags is the parsed CLI surface for the serve subcommand.
type serveFlags struct {
	upstream   string
	listenPath string
}

// parseServeFlags parses argv for the serve subcommand. Returns the
// flags plus a boolean indicating whether the caller should exit (and
// the suggested exit code) so runServe stays linear.
func parseServeFlags(args []string) (serveFlags, int, bool) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	upstream := fs.String("upstream", defaultUpstreamTarget,
		"upstream MWN1 target (qemu virtio-serial chardev unix socket)")
	listenPath := fs.String("listen", defaultListenPath,
		"local unix socket path to listen on")
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
		upstream:   *upstream,
		listenPath: *listenPath,
	}, 0, false
}

// runServe starts the bridge: dial the persistent MWN1 connection to
// the mwan-opnsense daemon inside the OPNsense VM, then run a gRPC
// server on a local unix socket that translates every probe RPC into
// one MWN1 round-trip on the persistent transport.
//
// Lifecycle: the bridge process exits on SIGINT/SIGTERM, on local
// fatal errors (cannot bind the listen socket), or on initial dial
// failure of the upstream chardev. The MWN1 client does not
// transparently reconnect; systemd Restart=always brings the bridge
// back when the upstream returns. An in-process retry loop is a
// follow-up improvement.
func runServe(args []string) int {
	cfg, exitCode, shouldExit := parseServeFlags(args)
	if shouldExit {
		return exitCode
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.InfoContext(ctx, "mwan-opnsense-host: dialing upstream",
		"upstream", cfg.upstream)
	upstreamClient, err := opnsenseclient.Dial(ctx, opnsenseclient.Config{
		Target: cfg.upstream,
		Log:    slog.Default(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "mwan-opnsense-host: upstream dial failed",
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
	slog.InfoContext(ctx, "mwan-opnsense-host: upstream dialed",
		"upstream", cfg.upstream)

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
	mwanv1.RegisterMWANOPNsenseServiceServer(gs,
		newProxyServer(upstreamClient.RPC(), slog.Default()))
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
