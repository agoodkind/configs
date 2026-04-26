//go:build linux

// Package netif provides Go-native (and, transitionally, /sbin/ip-shellout)
// implementations of low-level Linux network state operations: address and
// route reconciliation, policy-rule reconciliation, kernel event monitoring,
// DHCPv4 client, Router Solicitation, and connectivity probes.
//
// It is intentionally a leaf package: it does not depend on any other mwan
// internal package (besides version), and exposes a small, testable surface
// (IPRunner, Monitor, DHCPClient) that the higher-level ifmgr daemon and its
// modules consume. Splitting netif out of internal/oob lets multiple roles
// (vault-oob, lxc-failover-backup, future NPT/policy-routing modules) share
// the same primitives without duplicating shellout glue.
//
// The package is Linux-only. Operations either use vishvananda/netlink,
// mdlayher/ndp, golang.org/x/net/icmp+ipv6, or shell out to /sbin/ip and
// rdisc6 during the transition. All implementations log every boundary at
// slog.LevelDebug.
package netif

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// IPRunner abstracts execution of `ip` commands so the daemon can be unit
// tested without touching the real kernel. The interface is intentionally
// narrow: the only inputs are an argv slice and a timeout. Implementations
// are responsible for logging and dry-run handling.
type IPRunner interface {
	// Run executes `ip <args...>` with the given timeout. Returns combined
	// stdout+stderr on success or wrapped error including stderr on failure.
	// Implementations MUST log every invocation at slog.LevelDebug with
	// argv, exit code, output, and duration.
	Run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error)
}

// ExecIPRunner is the production IPRunner. It shells out to /sbin/ip via
// exec.CommandContext. Construct via NewExecIPRunner.
type ExecIPRunner struct {
	log    *slog.Logger
	dryRun bool
}

// NewExecIPRunner builds the production IPRunner. log must be non-nil.
// When dryRun is true, mutating commands (rule add, addr add, route add,
// etc.) are logged at LevelInfo and skipped; read-only commands still run.
func NewExecIPRunner(log *slog.Logger, dryRun bool) *ExecIPRunner {
	return &ExecIPRunner{log: log.With("component", "ip"), dryRun: dryRun}
}

// Run implements IPRunner.
func (r *ExecIPRunner) Run(
	ctx context.Context, timeout time.Duration, args ...string,
) ([]byte, error) {
	if r.dryRun && isMutating(args) {
		r.log.Info("dry-run: skipping mutating ip command",
			"argv", append([]string{"ip"}, args...),
		)
		return nil, nil
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(cctx, "ip", args...)
	out, err := cmd.CombinedOutput()
	dur := time.Since(start)

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	// Always log at DEBUG. On error, also bubble stderr in the wrapped error.
	r.log.Debug("ip command",
		"argv", append([]string{"ip"}, args...),
		"exit", exitCode,
		"duration_ms", dur.Milliseconds(),
		"output", strings.TrimRight(string(out), "\n"),
		"err", err,
	)

	if err != nil {
		return out, fmt.Errorf("ip %s: %w (exit=%d, output=%s)",
			strings.Join(args, " "), err, exitCode,
			strings.TrimSpace(string(out)),
		)
	}
	return out, nil
}

// isMutating returns true if the ip subcommand modifies kernel state.
// Used by dry-run mode to skip applies while still allowing inspection.
// Read-only forms ("show", "list", "get", "monitor") return false.
func isMutating(args []string) bool {
	if len(args) < 2 {
		return false
	}
	// Skip leading family flags like "-6", "-4", "-d", "-br".
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		i++
	}
	if i+1 >= len(args) {
		return false
	}
	verb := args[i+1]
	switch verb {
	case "add", "del", "delete", "replace", "change", "append", "flush",
		"set", "up", "down":
		return true
	}
	return false
}
