//go:build linux

package netif

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// SysctlRunner reads and writes /proc/sys keys via direct file I/O. Used
// by slaac_health to toggle disable_ipv6 and by diagnostics modules to
// observe interface tunables. No /sbin/sysctl shellout.
//
// Capabilities required for write: CAP_SYS_ADMIN, or systemd
// ProtectKernelTunables=false / ReadWritePaths=/proc/sys/net/ipv6/conf
// in the unit. Read is always allowed.
type SysctlRunner interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	DryRun() bool
}

// ProcSysctlRunner is the production SysctlRunner. Set respects dryRun:
// when true, writes are logged at INFO and skipped, while reads always
// execute.
type ProcSysctlRunner struct {
	log    *slog.Logger
	dryRun bool
}

// NewProcSysctlRunner constructs a runner. log must be non-nil.
func NewProcSysctlRunner(log *slog.Logger, dryRun bool) *ProcSysctlRunner {
	if log == nil {
		panic("netif.NewProcSysctlRunner: log is required")
	}
	r := &ProcSysctlRunner{
		log:    log.With("component", "sysctl"),
		dryRun: dryRun,
	}
	r.log.Debug("sysctl: constructed", "dry_run", dryRun)
	return r
}

// Get reads the sysctl value at key (e.g. "net.ipv6.conf.eth0.disable_ipv6").
// Returns the trimmed string contents.
func (r *ProcSysctlRunner) Get(ctx context.Context, key string) (string, error) {
	_ = ctx // file read is fast; we don't honour ctx here for simplicity
	path := keyToPath(key)
	start := time.Now()
	data, err := os.ReadFile(path)
	dur := time.Since(start)
	val := strings.TrimRight(string(data), "\n\t ")
	r.log.Debug("sysctl: read",
		"key", key, "path", path, "value", val,
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		return "", fmt.Errorf("sysctl read %s: %w", key, err)
	}
	return val, nil
}

// Set writes value to the sysctl. In dry-run mode, logs and returns nil.
// Returns wrapped error if the write fails (most commonly EACCES when the
// process lacks the systemd capability or ProtectKernelTunables blocks it).
func (r *ProcSysctlRunner) Set(ctx context.Context, key, value string) error {
	_ = ctx
	path := keyToPath(key)
	if r.dryRun {
		r.log.Info("sysctl: dry-run skipping write",
			"key", key, "path", path, "value", value)
		return nil
	}
	start := time.Now()
	err := os.WriteFile(path, []byte(value), 0o644)
	dur := time.Since(start)
	r.log.Debug("sysctl: write",
		"key", key, "path", path, "value", value,
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		// Surface EACCES specifically; it's the most common operator
		// misconfiguration and the diagnostic message helps unblock.
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf(
				"sysctl write %s: permission denied (need CAP_SYS_ADMIN or "+
					"ReadWritePaths=/proc/sys/net/ipv6/conf in systemd unit): %w",
				key, err,
			)
		}
		return fmt.Errorf("sysctl write %s: %w", key, err)
	}
	return nil
}

// DryRun reports whether mutating sysctl operations are skipped.
func (r *ProcSysctlRunner) DryRun() bool { return r.dryRun }

// keyToPath translates a sysctl dotted key into the corresponding /proc/sys
// path. "." becomes "/" except for components that contain a literal dot
// in the interface name (e.g. "vrrp.51", "enatt0.3242"). The kernel's
// /proc/sys hierarchy escapes those by reversing dot-vs-slash convention
// only at the interface name token. We follow the same convention as the
// `sysctl(8)` userspace tool: the FIRST occurrence of a dot in an
// interface name is preserved as a dot; subsequent dots remain. In
// practice the simpler rule is: if a key segment looks like a NIC name
// (contains a dot AND its surrounding context is "conf.<NAME>.<key>"),
// preserve dots in NAME.
//
// Implementation: pattern-match the well-known "conf.<NAME>.<leaf>" form;
// outside that form, treat all dots as path separators. This handles
// every key the daemon needs to write today.
func keyToPath(key string) string {
	const procSys = "/proc/sys/"

	// Special-case interface tunables: net.ipv{4,6}.conf.<NAME>.<leaf>.
	// The NAME may itself contain dots (e.g. "vrrp.51", "enatt0.3242").
	for _, prefix := range []string{"net.ipv4.conf.", "net.ipv6.conf."} {
		if strings.HasPrefix(key, prefix) {
			rest := strings.TrimPrefix(key, prefix)
			// rest is "<NAME>.<leaf>" where leaf is a single token without
			// dots in the kernel's tree. Split on the LAST dot.
			lastDot := strings.LastIndex(rest, ".")
			if lastDot < 0 {
				return procSys + strings.ReplaceAll(key, ".", "/")
			}
			name := rest[:lastDot]
			leaf := rest[lastDot+1:]
			head := strings.ReplaceAll(strings.TrimSuffix(prefix, "."), ".", "/")
			return procSys + head + "/" + name + "/" + leaf
		}
	}
	// Default: every dot becomes a slash.
	return procSys + strings.ReplaceAll(key, ".", "/")
}
