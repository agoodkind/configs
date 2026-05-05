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
	defaultDialTimeout    = 10 * time.Second
)

// runServe starts the bridge: dial the persistent upstream gRPC
// connection to the mwan-opnsense daemon inside the OPNsense VM, then
// run a gRPC server on a local unix socket that proxies all RPCs onto
// the persistent ClientConn.
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	upstream := fs.String("upstream", defaultUpstreamTarget,
		"upstream gRPC target (qemu virtio-serial chardev unix socket)")
	listenPath := fs.String("listen", defaultListenPath,
		"local unix socket path to listen on")
	dialTimeout := fs.Duration("dial-timeout", defaultDialTimeout,
		"timeout for the initial upstream handshake")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	if *upstream == "" {
		fmt.Fprintln(os.Stderr, "serve: -upstream required")
		return 2
	}
	if *listenPath == "" {
		fmt.Fprintln(os.Stderr, "serve: -listen required")
		return 2
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.InfoContext(ctx, "mwan-opnsense-host: dialing upstream",
		"upstream", *upstream,
		"dial_timeout", dialTimeout.String())
	upstreamClient, err := opnsenseclient.Dial(ctx, opnsenseclient.Config{
		Target:      *upstream,
		DialTimeout: *dialTimeout,
		Clock:       nil,
	})
	if err != nil {
		slog.ErrorContext(ctx, "mwan-opnsense-host: upstream dial failed",
			"upstream", *upstream,
			"err", err)
		return 1
	}
	defer func() {
		if closeErr := upstreamClient.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "mwan-opnsense-host: upstream close failed",
				"upstream", *upstream,
				"err", closeErr)
		}
	}()
	slog.InfoContext(ctx, "mwan-opnsense-host: upstream connected",
		"upstream", *upstream)

	listener, err := openLocalListener(ctx, *listenPath)
	if err != nil {
		slog.ErrorContext(ctx, "mwan-opnsense-host: listen failed",
			"listen", *listenPath,
			"err", err)
		return 1
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "mwan-opnsense-host: listener close failed",
				"listen", *listenPath,
				"err", closeErr)
		}
	}()
	slog.InfoContext(ctx, "mwan-opnsense-host: listening",
		"listen", *listenPath)

	gs := grpc.NewServer()
	mwanv1.RegisterMWANOPNsenseServiceServer(gs, &proxyServer{upstream: upstreamClient.RPC()})

	errCh := make(chan error, 1)
	go func() {
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
