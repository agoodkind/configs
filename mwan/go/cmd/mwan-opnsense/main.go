// mwan-opnsense is the recovery channel daemon that runs inside the
// OPNsense VM. It exposes a small gRPC surface for command exec and
// config.xml manipulation over a single virtio-serial-pci listener.
//
// There is no TLS and no application-level authentication. Access
// control is the unix socket permissions on the host side: only root
// on the Proxmox host can open /var/run/qemu-server/<vmid>.mwanrpc,
// and that already implies full power over the VM. The daemon trusts
// its single peer.
//
// Subcommands:
//
//	serve          start the daemon (long-running)
//	version        print build version and exit
//	status         print whether daemon is reachable on configured listener
//	is-enabled     exit 0 if rc.conf says enabled, 1 otherwise
package main

import (
	"fmt"
	"log/slog"
	"os"

	"goodkind.io/mwan/internal/version"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mwan-opnsense {serve|version|status|is-enabled} [flags]")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	sub := os.Args[1]
	args := os.Args[2:]
	slog.Info("mwan-opnsense boundary",
		"build", version.BuildVersionString(),
		"subcommand", sub)
	switch sub {
	case "version":
		fmt.Fprintln(os.Stdout, version.BuildVersionString())
	case "serve":
		runServe(args)
	case "status":
		runStatus(args)
	case "is-enabled":
		runIsEnabled(args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "mwan-opnsense: unknown subcommand %q\n", sub)
		usage()
	}
}
