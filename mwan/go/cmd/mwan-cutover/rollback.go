package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// cmdRollback forcefully restores pre-cutover state. Every step is best-effort
// and continues on failure. The goal is to get fe::1 back on the physical
// interface no matter what state keepalived, the vrrp interface, or the LXC is in.
func cmdRollback(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	host := cfg.MwanMgmtAddr
	iface := cfg.MwanIntIface
	to := cfg.SSHTimeoutSec

	// Idempotency check: if the original address is already on the physical
	// interface and keepalived is not running, there's nothing to rollback.
	addr := strings.Split(cfg.CurrentRealIPv6, "/")[0]
	out, err := sshExec(ctx, host, fmt.Sprintf("ip -6 addr show dev %s scope global", iface), to)
	if err == nil && strings.Contains(out.Stdout, addr) {
		r, _ := sshExec(ctx, host, "systemctl is-active keepalived", to)
		if r.Stdout == "inactive" || strings.Contains(r.Stdout, "inactive") {
			log.Info("rollback: already in pre-cutover state, nothing to do")
			return nil
		}
	}

	log.Info("rollback: FORCING revert to pre-cutover state")

	_ = sendEmail(cfg, fmt.Sprintf("%s ROLLBACK INITIATED", subjectPfx),
		"Rolling back HA cutover. Killing keepalived, restoring original addresses.")

	// Step 1: Kill keepalived on LXC (try stop, then kill, then disable)
	log.Info("rollback: killing keepalived on LXC", "lxc", cfg.FailoverLXCID)
	_, _ = localExec(ctx, "pct", []string{"exec", cfg.FailoverLXCID, "--",
		"systemctl", "stop", "keepalived"}, to)
	_, _ = localExec(ctx, "pct", []string{"exec", cfg.FailoverLXCID, "--",
		"killall", "-9", "keepalived"}, to)
	_, _ = localExec(ctx, "pct", []string{"exec", cfg.FailoverLXCID, "--",
		"systemctl", "disable", "keepalived"}, to)

	// Step 2: Kill keepalived on VM (try stop, then kill, then disable)
	log.Info("rollback: killing keepalived on VM")
	_, _ = sshExec(ctx, host, "systemctl stop keepalived", to)
	_, _ = sshExec(ctx, host, "killall -9 keepalived", to)
	_, _ = sshExec(ctx, host, "systemctl disable keepalived", to)

	// Step 3: Delete the vrrp macvlan interface entirely
	log.Info("rollback: removing vrrp interface")
	_, _ = sshExec(ctx, host, "ip link del vrrp.51 2>/dev/null", to)

	// Step 4: Flush ALL global addresses from the internal interface
	log.Info("rollback: flushing all addresses from interface")
	_, _ = sshExec(ctx, host,
		fmt.Sprintf("ip -6 addr flush dev %s scope global", iface), to)
	_, _ = sshExec(ctx, host,
		fmt.Sprintf("ip -4 addr flush dev %s scope global", iface), to)

	// Step 5: Re-add original addresses with nodad (skip DAD, we need this NOW)
	log.Info("rollback: restoring original addresses",
		"v6", cfg.CurrentRealIPv6, "v4", cfg.CurrentRealIPv4)
	_, _ = sshExec(ctx, host,
		fmt.Sprintf("ip -6 addr add %s dev %s nodad", cfg.CurrentRealIPv6, iface), to)
	_, _ = sshExec(ctx, host,
		fmt.Sprintf("ip addr add %s dev %s", cfg.CurrentRealIPv4, iface), to)

	// Step 6: Send gratuitous NDP to update neighbor caches immediately
	log.Info("rollback: sending unsolicited neighbor advertisement")
	_, _ = sshExec(ctx, host,
		fmt.Sprintf("ndisc6 -q %s %s 2>/dev/null || ip -6 neigh replace proxy %s dev %s 2>/dev/null",
			cfg.CurrentRealIPv6[:len(cfg.CurrentRealIPv6)-3], iface,
			cfg.CurrentRealIPv6[:len(cfg.CurrentRealIPv6)-3], iface), to)

	// Step 7: Verify — if SSH works and the address is there, we're good
	log.Info("rollback: verifying")
	out, err = sshExec(ctx, host,
		fmt.Sprintf("ip -6 addr show dev %s scope global", iface), to)
	if err != nil {
		log.Error("rollback: SSH to VM failed during verify", "err", err)
		// Try serial-exec as last resort
		log.Info("rollback: attempting verification via serial-exec")
		serialOut, serialErr := localExec(ctx, "serial-exec",
			[]string{"run", "mwan", fmt.Sprintf("ip -6 addr show dev %s scope global", iface)}, 15)
		if serialErr != nil {
			log.Error("rollback: serial-exec also failed", "err", serialErr)
		} else {
			log.Info("rollback: serial-exec verify", "addrs", serialOut)
		}
	} else {
		log.Info("rollback: interface state", "addrs", out.Stdout)
	}

	// Step 8: Verify connectivity
	log.Info("rollback: testing internet connectivity")
	_, pingErr := localExec(ctx, "ping6",
		[]string{"-c", "2", "-W", "3", cfg.PingTargetIPv6}, to)
	if pingErr != nil {
		log.Error("rollback: internet NOT reachable after rollback", "err", pingErr)
		_ = sendEmail(cfg, fmt.Sprintf("%s ROLLBACK COMPLETE BUT INTERNET DOWN", subjectPfx),
			"Rollback completed but internet connectivity test failed. Manual intervention may be needed.")
		return fmt.Errorf("rollback completed but internet unreachable: %w", pingErr)
	}

	log.Info("rollback: internet reachable, rollback successful")
	_ = sendEmail(cfg, fmt.Sprintf("%s ROLLBACK COMPLETE", subjectPfx),
		fmt.Sprintf("Rollback completed. Internet reachable. Interface state:\n%s", out.Stdout))

	log.Info("rollback: done")
	return nil
}
