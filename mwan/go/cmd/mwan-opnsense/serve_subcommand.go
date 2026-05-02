package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"goodkind.io/mwan/internal/opnsensesvc"
)

// runServe starts the gRPC daemon with both the TCP listener (LAN
// diagnostic) and the virtio-serial-pci listener (OOB recovery
// channel). Either may be disabled by setting its flag to empty.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	tcpAddr := fs.String("tcp", "[::]:9443", "mTLS TCP listen address; empty disables")
	serialPath := fs.String("serial", "/dev/ttyV0.1", "virtio-serial device path; empty disables")
	certPath := fs.String("cert", "/usr/local/etc/mwan-opnsense/server.crt", "server cert PEM")
	keyPath := fs.String("key", "/usr/local/etc/mwan-opnsense/server.key", "server key PEM")
	caPath := fs.String("ca", "/usr/local/etc/mwan-opnsense/ca.crt", "client CA PEM")
	pinsPath := fs.String("pins", "/usr/local/etc/mwan-opnsense/allowed_clients.txt", "SPKI pins file (empty disables pin check)")
	configPath := fs.String("config-xml", opnsensesvc.ConfigPath, "OPNsense config.xml path")
	backupDir := fs.String("backup-dir", opnsensesvc.BackupDir, "directory for snapshot files")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if *tcpAddr == "" && *serialPath == "" {
		fmt.Fprintln(os.Stderr, "serve: at least one of -tcp or -serial must be set")
		os.Exit(2)
	}

	creds, err := opnsensesvc.LoadServerCreds(*certPath, *keyPath, *caPath, *pinsPath)
	if err != nil {
		slog.Error("serve: load creds failed", "err", err)
		os.Exit(1)
	}

	srv := opnsensesvc.NewServer(slog.Default(), *configPath, *backupDir)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	// We do not `defer cancel()` because we explicitly call it before
	// any os.Exit; gocritic flags exit-after-defer otherwise.

	opts := opnsensesvc.ServeOpts{
		TCPAddr:    *tcpAddr,
		SerialPath: *serialPath,
		OpenSerial: opnsensesvc.OpenVirtioSerial,
		Creds:      creds,
		Server:     srv,
		Log:        slog.Default(),
	}

	slog.Info("mwan-opnsense: serving",
		"tcp_addr", *tcpAddr,
		"serial_path", *serialPath)

	err = opnsensesvc.Serve(ctx, opts)
	cancel()
	if err != nil {
		slog.Error("serve: terminated", "err", err)
		os.Exit(1)
	}
	slog.Info("mwan-opnsense: stopped")
}

// runStatus reports whether the rc.d service is currently running.
// It is the daemon-side counterpart, NOT a remote-probe tool. For
// dialing the daemon from a client, use `mwan opnsense-probe` from
// the vault-side mwan binary which carries the
// internal/opnsenseclient package.
func runStatus(_ []string) {
	fmt.Fprintln(os.Stderr, "mwan-opnsense status: not implemented yet (use rc.d 'service mwan_opnsense status')")
	os.Exit(1)
}

func runIsEnabled(_ []string) {
	// Mirror cloudflared-configd shape: exit 0 if enabled, 1 otherwise.
	// Stub returns 1 for now; rc.conf parser comes in S7.
	os.Exit(1)
}
