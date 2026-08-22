// Package cli implements the `mwan opnsense` subcommand family:
// the in-VM MWN1 daemon (serve, push, stage, restart, revert, gc), the
// host-side serial bridge, and the operator verbs (exec, config, file,
// upgrade, selftest, version) that drive the daemon over the channel.
//
// The package is shared by two entry points. The linux monolith
// (cmd/mwan) dispatches `mwan opnsense ...` to Run; the FreeBSD router
// binary (cmd/mwan-opnsense) carries only this family plus the
// symlink-name fast path into RunDaemonServe. Keeping the family here,
// portable and untagged, is what lets each entry point ship only the
// platform it serves without per-subcommand build-tag stub pairs.
package cli

import (
	"path/filepath"
	"strings"
)

// Run dispatches `mwan opnsense <verb> [args...]`. args excludes the
// leading "opnsense" token. Returns the process exit code.
func Run(args []string) int {
	return runOPNsense(args)
}

// RunDaemonServe starts the in-VM daemon serve loop directly, the fast
// path rc.d takes when the binary is invoked via its mwan-opnsense
// symlink name. Returns the process exit code.
func RunDaemonServe(args []string) int {
	return runOPNsenseDaemonServe(args)
}

// InvokedAsDaemon reports whether argv0 names the mwan-opnsense symlink
// (mwan-opnsense or mwan-opnsense.<suffix>), which routes straight into
// RunDaemonServe so rc.d keeps its existing ExecStart.
func InvokedAsDaemon(argv0 string) bool {
	binaryName := filepath.Base(argv0)
	return binaryName == "mwan-opnsense" || strings.HasPrefix(binaryName, "mwan-opnsense.")
}
