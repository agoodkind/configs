// Command mwan-opnsense is the FreeBSD router entry point. It ships as
// the release's freebsd `mwan` artifact and carries only the opnsense
// subcommand family; the linux monolith (cmd/mwan) carries every other
// role. Splitting the entry points keeps the router binary's build
// graph to what the router runs, so neither platform's gates see the
// other's code.
//
// The CLI surface matches the monolith's opnsense subtree exactly:
// invoked via the mwan-opnsense symlink the binary goes straight into
// the daemon serve loop (rc.d's ExecStart contract), and invoked as
// `mwan opnsense <verb>` it dispatches the same verbs.
package main

import (
	"fmt"
	"log/slog"
	"os"

	opnsensecli "goodkind.io/mwan/internal/opnsense/cli"
	"goodkind.io/mwan/internal/version"
)

func main() {
	if opnsensecli.InvokedAsDaemon(os.Args[0]) {
		os.Exit(opnsensecli.RunDaemonServe(os.Args[1:]))
	}
	if len(os.Args) < 2 || os.Args[1] != "opnsense" {
		fmt.Fprintln(os.Stderr, "usage: mwan opnsense <verb> [args]")
		os.Exit(1)
	}

	// Boundary log: same grep-anchor the monolith writes, linking the
	// binary on disk to the session it executed in.
	slog.Info("mwan boundary",
		"build", version.BuildVersionString(),
		"subcommand", "opnsense")

	os.Exit(opnsensecli.Run(os.Args[2:]))
}
