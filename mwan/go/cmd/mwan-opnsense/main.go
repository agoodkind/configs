// mwan-opnsense is the recovery channel daemon that runs inside the
// OPNsense VM. It exposes a small mTLS-gated gRPC surface for command
// exec and config.xml manipulation. Two listeners: virtio-serial-pci
// (OOB) and TCP-on-LAN.
//
// Subcommands:
//
//	serve          start the daemon (long-running)
//	version        print build version and exit
//	status         print whether daemon is reachable on configured listeners
//	is-enabled     exit 0 if rc.conf says enabled, 1 otherwise
//	ca-init        generate a fresh CA for issuing client/server certs
//	issue          sign a leaf cert for a named caller (server or vault)
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"goodkind.io/mwan/internal/version"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mwan-opnsense {serve|version|status|is-enabled|ca-init|issue} [flags]")
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
	case "ca-init":
		runCAInit(args)
	case "issue":
		runIssue(args)
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

func runCAInit(args []string) {
	fs := flag.NewFlagSet("ca-init", flag.ExitOnError)
	outDir := fs.String("out-dir", "", "directory to write ca.crt and ca.key (required)")
	cn := fs.String("cn", "mwan-opnsense-ca", "CA common name")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *outDir == "" {
		fmt.Fprintln(os.Stderr, "ca-init: -out-dir required")
		os.Exit(2)
	}
	if err := caInit(*outDir, *cn); err != nil {
		fmt.Fprintln(os.Stderr, "ca-init:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "wrote %s/ca.crt and %s/ca.key\n", *outDir, *outDir)
}

func runIssue(args []string) {
	fs := flag.NewFlagSet("issue", flag.ExitOnError)
	caDir := fs.String("ca-dir", "", "directory containing ca.crt and ca.key (required)")
	cn := fs.String("cn", "", "leaf common name (required)")
	out := fs.String("out", "", "output bundle prefix; produces <out>.crt and <out>.key (required)")
	dnsList := fs.String("dns", "", "comma-separated DNS SANs")
	ipList := fs.String("ip", "", "comma-separated IP SANs")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *caDir == "" || *cn == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "issue: -ca-dir, -cn, -out all required")
		os.Exit(2)
	}
	if err := issueBundle(*caDir, *cn, *out, splitCSV(*dnsList), splitCSV(*ipList)); err != nil {
		fmt.Fprintln(os.Stderr, "issue:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "wrote %s.crt and %s.key\n", *out, *out)
}
