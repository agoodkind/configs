// Package opnsenseclient is the vault-side gRPC client for the
// mwan-opnsense daemon running inside the OPNsense VM.
//
// The same package is used by both:
//   - operational tooling (`mwan opnsense-probe` subcommand) for ad
//     hoc dialing of the daemon
//   - cutover2 (and any future caller) for replacing the historical
//     ssh+sudo+yq path with a typed RPC surface
//
// Two transports are supported via gRPC's native target schemes:
//
//	tcp://[3d06:bad:b01:fe::2]:9443         the LAN-side mTLS endpoint
//	unix:///var/run/qemu-server/<vmid>.mwanrpc   the OOB virtio-serial socket
//
// gRPC's resolver dispatches `unix:///` natively, so no custom
// dialer is needed.
package opnsenseclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// Config is the per-connection configuration.
type Config struct {
	// Target is the gRPC target string.
	//   tcp://host:port
	//   unix:///path/to/socket
	Target string
	// CertPath, KeyPath, CAPath point at PEM material on disk.
	CertPath, KeyPath, CAPath string
	// Authority overrides the :authority pseudo-header. Useful with
	// unix sockets (where there is no host) or when the server cert
	// SAN does not match the dial target.
	Authority string
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
	creds, err := loadCreds(cfg.CertPath, cfg.KeyPath, cfg.CAPath)
	if err != nil {
		return nil, err
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}

	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	if cfg.Authority != "" {
		dialOpts = append(dialOpts, grpc.WithAuthority(cfg.Authority))
	}

	// gRPC's resolver knows "unix:///..." natively. "tcp://host:port"
	// is convenient for callers but not a real gRPC scheme; strip it.
	target, _ := strings.CutPrefix(cfg.Target, "tcp://")

	conn, err := grpc.NewClient(target, dialOpts...)
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

func loadCreds(certPath, keyPath, caPath string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		slog.Error("opnsenseclient: keypair load failed", "cert", certPath, "key", keyPath, "err", err)
		return nil, fmt.Errorf("opnsenseclient: keypair: %w", err)
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		slog.Error("opnsenseclient: ca read failed", "ca", caPath, "err", err)
		return nil, fmt.Errorf("opnsenseclient: ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("opnsenseclient: ca PEM had no usable certs")
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}), nil
}
