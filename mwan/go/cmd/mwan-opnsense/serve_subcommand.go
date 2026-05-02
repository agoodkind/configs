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

// runServe starts the gRPC daemon with the virtio-serial-pci listener.
// There is exactly one listener and exactly one peer. Auth is unix
// socket permissions on the host side (root-only), so the daemon does
// not authenticate at the application layer.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	serialPath := fs.String("serial", "/dev/ttyV0.1", "virtio-serial device path")
	configPath := fs.String("config-xml", opnsensesvc.ConfigPath, "OPNsense config.xml path")
	backupDir := fs.String("backup-dir", opnsensesvc.BackupDir, "directory for snapshot files")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if *serialPath == "" {
		fmt.Fprintln(os.Stderr, "serve: -serial path required")
		os.Exit(2)
	}

	srv := opnsensesvc.NewServer(slog.Default(), *configPath, *backupDir)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	// We do not `defer cancel()` because we explicitly call it before
	// any os.Exit; gocritic flags exit-after-defer otherwise.

	opts := opnsensesvc.ServeOpts{
		SerialPath: *serialPath,
		OpenSerial: opnsensesvc.OpenVirtioSerial,
		Server:     srv,
		Log:        slog.Default(),
	}

	slog.Info("mwan-opnsense: serving", "serial_path", *serialPath)

	err := opnsensesvc.Serve(ctx, opts)
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
