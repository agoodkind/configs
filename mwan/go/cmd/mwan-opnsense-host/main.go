// mwan-opnsense-host is the Proxmox-host-side bridge daemon for the
// OPNsense recovery channel.
//
// Architecture:
//   - One persistent gRPC ClientConn to the qemu virtio-serial chardev
//     unix socket (/var/run/qemu-server/<vmid>.mwanrpc), held open for
//     the lifetime of the daemon. gRPC's built-in keepalive plus
//     auto-reconnect handles transient failures of the in-VM mwan-opnsense
//     daemon.
//   - Local unix socket listener (default /var/run/mwan-opnsense.sock)
//     speaking the same gRPC API. Each incoming RPC is forwarded to
//     the persistent upstream ClientConn via HTTP/2 stream multiplex.
//
// Why this exists: virtio-serial is a single byte stream; gRPC over it
// works for ONE long-lived gRPC connection. By holding that single
// upstream connection here and fanning out probe requests onto it, we
// satisfy "many short-lived gRPC clients" without any custom framing
// or multiplexer protocol. HTTP/2 streams ARE the multiplexer.
//
// Subcommands:
//
//	serve     start the bridge (long-running)
//	version   print build version and exit
package main

import (
	"fmt"
	"log/slog"
	"os"

	"goodkind.io/mwan/internal/version"
)

func usage() int {
	fmt.Fprintln(os.Stderr, "usage: mwan-opnsense-host {serve|version} [flags]")
	return 2
}

func main() {
	slog.Info("mwan-opnsense-host process start")
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		return usage()
	}
	sub := args[0]
	subArgs := args[1:]
	// Resolve subcommand against an allowlist BEFORE logging it, so the
	// log line cannot be poisoned by attacker-controlled input.
	switch sub {
	case "version":
		slog.Info("mwan-opnsense-host boundary",
			"build", version.BuildVersionString(),
			"subcommand", "version")
		fmt.Fprintln(os.Stdout, version.BuildVersionString())
		return 0
	case "serve":
		slog.Info("mwan-opnsense-host boundary",
			"build", version.BuildVersionString(),
			"subcommand", "serve")
		return runServe(subArgs)
	case "-h", "--help", "help":
		return usage()
	default:
		fmt.Fprintf(os.Stderr, "mwan-opnsense-host: unknown subcommand\n")
		return usage()
	}
}
