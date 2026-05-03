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

func usage() int {
	fmt.Fprintln(os.Stderr, "usage: mwan-opnsense {serve|version|status|is-enabled} [flags]")
	return 2
}

func main() {
	if os.Getenv("MWAN_OPNSENSE_DAEMON_CHILD") == "1" {
		os.Unsetenv("MWAN_OPNSENSE_DAEMON_CHILD")
		os.Exit(run(os.Args[1:]))
	}
	slog.Info("mwan-opnsense process start")
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		return usage()
	}
	sub := args[0]
	subArgs := args[1:]
	slog.Info("mwan-opnsense boundary",
		"build", version.BuildVersionString(),
		"subcommand", sub)
	switch sub {
	case "version":
		fmt.Fprintln(os.Stdout, version.BuildVersionString())
		return 0
	case "serve":
		return runServe(subArgs)
	case "status":
		return runStatus(subArgs)
	case "is-enabled":
		return runIsEnabled(subArgs)
	case "-h", "--help", "help":
		return usage()
	default:
		fmt.Fprintf(os.Stderr, "mwan-opnsense: unknown subcommand %q\n", sub)
		return usage()
	}
}
