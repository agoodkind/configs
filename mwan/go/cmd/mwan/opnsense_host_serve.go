package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/yamux"
)

// runOPNsenseHostServe runs the host-side yamux bridge. It owns one
// persistent connection to the qemu virtio-serial unix socket, wraps
// it in a yamux client session, and forwards each accepted local
// connection to a fresh yamux substream. Each substream gives the
// remote gRPC server a clean HTTP/2 connection lifecycle on top of
// the persistent byte stream. yamux keep-alive is disabled because
// the virtio-serial line has no out-of-band channel.
func runOPNsenseHostServe(args []string) int {
	fs := flag.NewFlagSet("opnsense-host serve", flag.ContinueOnError)
	upstream := fs.String("upstream", "", "upstream unix socket (e.g. unix:///var/run/qemu-server/102.mwanrpc)")
	listen := fs.String("listen", "/var/run/mwan-opnsense.sock", "local unix socket path for CLI clients")
	reconnect := fs.Duration("reconnect", 2*time.Second, "delay between upstream reconnect attempts")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	upstreamPath, ok := unixPath(*upstream)
	if !ok {
		fmt.Fprintln(os.Stderr, "opnsense-host serve: -upstream must be unix:///abs/path")
		return 2
	}
	if !strings.HasPrefix(*listen, "/") {
		fmt.Fprintln(os.Stderr, "opnsense-host serve: -listen must be an absolute path")
		return 2
	}

	log := slog.Default()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := os.Remove(*listen); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "opnsense-host serve: clear stale socket: %v\n", err)
		return 1
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "unix", *listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "opnsense-host serve: listen %s: %v\n", *listen, err)
		return 1
	}
	if err := os.Chmod(*listen, 0o600); err != nil {
		_ = listener.Close()
		fmt.Fprintf(os.Stderr, "opnsense-host serve: chmod %s: %v\n", *listen, err)
		return 1
	}
	defer func() { _ = listener.Close() }()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.ErrorContext(ctx, "opnsense-host: stop watcher panic",
					"panic", r, "err", fmt.Errorf("panic: %v", r))
			}
		}()
		<-ctx.Done()
		_ = listener.Close()
	}()

	log.InfoContext(ctx, "opnsense-host: serving", "upstream", upstreamPath, "listen", *listen)
	if err := bridgeLoop(ctx, log, listener, upstreamPath, *reconnect); err != nil {
		log.ErrorContext(ctx, "opnsense-host: bridge terminated", "err", err)
		return 1
	}
	return 0
}

// bridgeLoop maintains one yamux session at a time and forwards every
// accepted local connection to a fresh substream over the current
// session. When the session dies (upstream close, yamux EOF) the next
// accepted local connection triggers a reconnect.
func bridgeLoop(ctx context.Context, log *slog.Logger, listener net.Listener, upstreamPath string, backoff time.Duration) error {
	state := &sessionState{
		mu:           sync.Mutex{},
		session:      nil,
		upstreamPath: upstreamPath,
		log:          log,
		backoff:      backoff,
	}
	defer state.closeSession(ctx)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			wrapped := fmt.Errorf("accept: %w", err)
			log.ErrorContext(ctx, "opnsense-host: accept failed", "err", wrapped)
			return wrapped
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.ErrorContext(ctx, "opnsense-host: client handler panic",
						"panic", r, "err", fmt.Errorf("panic: %v", r))
				}
			}()
			state.handleClient(ctx, conn)
		}()
	}
}

type sessionState struct {
	mu           sync.Mutex
	session      *yamux.Session
	upstreamPath string
	log          *slog.Logger
	backoff      time.Duration
}

// openStream returns a yamux substream on the current session. If the
// session is nil or already dead, dial upstream, build a new session,
// and open a stream on that one.
func (s *sessionState) openStream(ctx context.Context) (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("openStream: %w", err)
		}
		if s.session == nil || s.session.IsClosed() {
			if err := s.dialLocked(ctx); err != nil {
				s.log.WarnContext(ctx, "opnsense-host: upstream dial failed", "err", err, "attempt", attempt)
				if !sleepCtxOK(ctx, s.backoff) {
					return nil, fmt.Errorf("openStream backoff: %w", ctx.Err())
				}
				continue
			}
		}
		stream, err := s.session.OpenStream()
		if err == nil {
			return stream, nil
		}
		s.log.WarnContext(ctx, "opnsense-host: open stream failed; reconnecting", "err", err)
		_ = s.session.Close()
		s.session = nil
	}
}

func (s *sessionState) dialLocked(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", s.upstreamPath)
	if err != nil {
		wrapped := fmt.Errorf("dial upstream %s: %w", s.upstreamPath, err)
		s.log.WarnContext(ctx, "opnsense-host: dial upstream failed", "err", wrapped)
		return wrapped
	}
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = false
	cfg.LogOutput = io.Discard
	cfg.MaxStreamWindowSize = 16 * 1024 * 1024
	session, err := yamux.Client(conn, cfg)
	if err != nil {
		_ = conn.Close()
		wrapped := fmt.Errorf("yamux client: %w", err)
		s.log.WarnContext(ctx, "opnsense-host: yamux client failed", "err", wrapped)
		return wrapped
	}
	s.session = session
	s.log.InfoContext(ctx, "opnsense-host: upstream session established")
	return nil
}

func (s *sessionState) closeSession(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		if err := s.session.Close(); err != nil {
			s.log.WarnContext(ctx, "opnsense-host: close session failed", "err", err)
		}
		s.session = nil
	}
}

func (s *sessionState) handleClient(ctx context.Context, client net.Conn) {
	defer func() { _ = client.Close() }()
	stream, err := s.openStream(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "opnsense-host: open stream for client failed", "err", err)
		return
	}
	defer func() { _ = stream.Close() }()

	errCh := make(chan error, 2)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.ErrorContext(ctx, "opnsense-host: copy uplink panic",
					"panic", r, "err", fmt.Errorf("panic: %v", r))
			}
		}()
		_, copyErr := io.Copy(stream, client)
		errCh <- copyErr
	}()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.ErrorContext(ctx, "opnsense-host: copy downlink panic",
					"panic", r, "err", fmt.Errorf("panic: %v", r))
			}
		}()
		_, copyErr := io.Copy(client, stream)
		errCh <- copyErr
	}()
	<-errCh
}

// sleepCtxOK waits for d or returns false if ctx is cancelled first.
// The caller checks the return and re-derives ctx.Err() at its own
// wrap site so this helper does not leak a bare interface error.
func sleepCtxOK(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func unixPath(target string) (string, bool) {
	const scheme = "unix://"
	if !strings.HasPrefix(target, scheme) {
		return "", false
	}
	path := strings.TrimPrefix(target, scheme)
	if !strings.HasPrefix(path, "/") {
		return "", false
	}
	return path, true
}
