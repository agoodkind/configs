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
	"strings"
	"syscall"
)

const (
	defaultUpstreamTarget = "unix:///var/run/qemu-server/101.mwanrpc"
	defaultListenPath     = "/var/run/mwan-opnsense.sock"
)

// opnsenseHostServeFlags is the parsed CLI surface for the serve subcommand.
type opnsenseHostServeFlags struct {
	upstream   string
	listenPath string
}

// parseOPNsenseHostServeFlags parses argv for the serve subcommand. Returns the
// flags plus a boolean indicating whether the caller should exit (and
// the suggested exit code) so runOPNsenseHostServe stays linear.
func parseOPNsenseHostServeFlags(args []string) (opnsenseHostServeFlags, int, bool) {
	fs := flag.NewFlagSet("opnsense-host serve", flag.ContinueOnError)
	upstream := fs.String("upstream", defaultUpstreamTarget,
		"upstream MWN1 target (qemu virtio-serial chardev unix socket)")
	listenPath := fs.String("listen", defaultListenPath,
		"local unix socket path to listen on")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return opnsenseHostServeFlags{}, 2, true
	}
	if *upstream == "" {
		fmt.Fprintln(os.Stderr, "serve: -upstream required")
		return opnsenseHostServeFlags{}, 2, true
	}
	if *listenPath == "" {
		fmt.Fprintln(os.Stderr, "serve: -listen required")
		return opnsenseHostServeFlags{}, 2, true
	}
	return opnsenseHostServeFlags{
		upstream:   *upstream,
		listenPath: *listenPath,
	}, 0, false
}

func runOPNsenseHostServe(args []string) int {
	cfg, exitCode, shouldExit := parseOPNsenseHostServeFlags(args)
	if shouldExit {
		return exitCode
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.InfoContext(ctx, "mwan-opnsense-host serve boundary",
		"upstream", cfg.upstream,
		"listen", cfg.listenPath)

	slog.InfoContext(ctx, "mwan-opnsense-host: dialing upstream",
		"upstream", cfg.upstream)
	upstreamConn, err := dialMWN1Target(ctx, cfg.upstream)
	if err != nil {
		slog.ErrorContext(ctx, "mwan-opnsense-host: upstream dial failed",
			"upstream", cfg.upstream,
			"err", err)
		return 1
	}
	defer upstreamConn.Close()
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

	bridge := newFanInBridge(upstreamConn, listener, slog.Default())
	if err := bridge.serve(ctx); err != nil {
		slog.ErrorContext(ctx, "mwan-opnsense-host: bridge terminated",
			"err", err)
		return 1
	}
	return 0
}

// openLocalListener prepares a unix-socket listener at path. Removes
// any stale socket file from a previous run before binding. Sets mode
// 0660 so non-root callers cannot connect by default.
func openLocalListener(ctx context.Context, path string) (net.Listener, error) {
	slog.DebugContext(ctx, "openLocalListener", "path", path)
	removeErr := os.Remove(path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		slog.ErrorContext(ctx, "mwan-opnsense-host: remove stale socket failed",
			"path", path, "err", removeErr)
		return nil, fmt.Errorf("remove stale socket %s: %w", path, removeErr)
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "unix", path)
	if err != nil {
		slog.ErrorContext(ctx, "mwan-opnsense-host: listen unix failed",
			"path", path, "err", err)
		return nil, fmt.Errorf("listen unix %s: %w", path, err)
	}
	chmodErr := os.Chmod(path, 0o660)
	if chmodErr != nil {
		_ = listener.Close()
		slog.ErrorContext(ctx, "mwan-opnsense-host: chmod socket failed",
			"path", path, "err", chmodErr)
		return nil, fmt.Errorf("chmod %s: %w", path, chmodErr)
	}
	return listener, nil
}

func dialMWN1Target(ctx context.Context, target string) (net.Conn, error) {
	path, ok := strings.CutPrefix(target, "unix://")
	if !ok {
		err := fmt.Errorf("unsupported upstream target %q: only unix:// is supported", target)
		slog.ErrorContext(ctx, "mwan-opnsense-host: unsupported upstream target",
			"target", target, "err", err)
		return nil, err
	}
	if path == "" {
		err := errors.New("empty unix upstream path")
		slog.ErrorContext(ctx, "mwan-opnsense-host: empty upstream path", "err", err)
		return nil, err
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		slog.ErrorContext(ctx, "mwan-opnsense-host: dial unix upstream failed",
			"path", path, "err", err)
		return nil, fmt.Errorf("dial unix upstream %s: %w", path, err)
	}
	return conn, nil
}
