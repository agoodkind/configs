package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

func cmdPreflight(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	log.Info("preflight: checking all preconditions")

	checks := []struct {
		name string
		fn   func(context.Context, *slog.Logger, *CutoverConfig) error
	}{
		{"vm-113-running", preflightVMRunning},
		{"vm-113-ssh", preflightVMSSH},
		{"vm-113-interface", preflightVMInterface},
		{"vm-113-current-addr", preflightCurrentAddr},
		{"vm-113-no-keepalived", preflightNoKeepalived},
		{"lxc-failover-exists", preflightLXCExists},
		{"lxc-failover-keepalived-installed", preflightLXCKeepalived},
		{"internet-reachable", preflightInternet},
		{"email-works", preflightEmail},
	}

	var failures []string
	for _, c := range checks {
		log.Info("preflight check", "check", c.name)
		if err := c.fn(ctx, log, cfg); err != nil {
			log.Error("preflight FAILED", "check", c.name, "err", err)
			failures = append(failures, fmt.Sprintf("%s: %v", c.name, err))
		} else {
			log.Info("preflight OK", "check", c.name)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("preflight failed:\n  %s", strings.Join(failures, "\n  "))
	}

	log.Info("preflight: all checks passed")
	return nil
}

func preflightVMRunning(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	out, err := localExec(ctx, "qm", []string{"status", cfg.MwanVMID}, cfg.SSHTimeoutSec)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "running") {
		return fmt.Errorf("VM %s is not running: %s", cfg.MwanVMID, out)
	}
	return nil
}

func preflightVMSSH(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	_, err := sshMustExec(ctx, cfg.MwanMgmtAddr, "hostname", cfg.SSHTimeoutSec)
	return err
}

func preflightVMInterface(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	out, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
		fmt.Sprintf("ip link show %s", cfg.MwanIntIface), cfg.SSHTimeoutSec)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "UP") {
		return fmt.Errorf("interface %s not UP: %s", cfg.MwanIntIface, out)
	}
	return nil
}

func preflightCurrentAddr(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	// Strip CIDR for the grep
	addr := strings.Split(cfg.CurrentRealIPv6, "/")[0]
	out, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
		fmt.Sprintf("ip -6 addr show dev %s scope global", cfg.MwanIntIface), cfg.SSHTimeoutSec)
	if err != nil {
		return err
	}
	if !strings.Contains(out, addr) {
		return fmt.Errorf("current real address %s not found on %s:\n%s",
			cfg.CurrentRealIPv6, cfg.MwanIntIface, out)
	}
	return nil
}

func preflightNoKeepalived(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	r, err := sshExec(ctx, cfg.MwanMgmtAddr, "systemctl is-active keepalived", cfg.SSHTimeoutSec)
	if err != nil {
		return err
	}
	if strings.Contains(r.Stdout, "active") && !strings.Contains(r.Stdout, "inactive") {
		return fmt.Errorf("keepalived is already running on VM %s; expected stopped for fresh cutover",
			cfg.MwanVMID)
	}
	return nil
}

func preflightLXCExists(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	out, err := localExec(ctx, "pct", []string{"status", cfg.FailoverLXCID}, cfg.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("LXC %s not found: %w", cfg.FailoverLXCID, err)
	}
	log.Debug("lxc status", "output", out)
	return nil
}

func preflightLXCKeepalived(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	out, err := localExec(ctx, "pct", []string{"exec", cfg.FailoverLXCID, "--",
		"dpkg", "-l", "keepalived"}, cfg.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("keepalived not installed on LXC %s: %w", cfg.FailoverLXCID, err)
	}
	if !strings.Contains(out, "keepalived") {
		return fmt.Errorf("keepalived package not found on LXC %s", cfg.FailoverLXCID)
	}
	return nil
}

func preflightInternet(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	_, err := localExec(ctx, "ping6",
		[]string{"-c", "1", "-W", "3", cfg.PingTargetIPv6}, cfg.SSHTimeoutSec)
	return err
}

func preflightEmail(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	return sendEmail(cfg,
		fmt.Sprintf("%s Preflight email test", subjectPfx),
		"This is a preflight test email from mwan-cutover. If you received this, email is working.")
}
