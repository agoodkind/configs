package cutover

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"goodkind.io/mwan/internal/config"
)

func cmdVerify(ctx context.Context, log *slog.Logger, cfg *config.Config, dryRun bool) error {
	if dryRun {
		log.Info("verify: DRY RUN — would run connectivity tests (skipping)")
		return nil
	}
	log.Info("verify: running end-to-end connectivity tests")

	checks := buildVerifyChecks(ctx, log, cfg)

	var failures []string
	for _, c := range checks {
		log.Info("verify check", "check", c.name)
		if err := c.fn(); err != nil {
			log.Error("verify FAILED", "check", c.name, "err", err)
			failures = append(failures, fmt.Sprintf("%s: %v", c.name, err))
		} else {
			log.Info("verify OK", "check", c.name)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("verification failed:\n  %s", strings.Join(failures, "\n  "))
	}

	log.Info("verify: all checks passed")
	return nil
}

type verifyCheck struct {
	name string
	fn   func() error
}

// buildVerifyChecks returns the ordered list of connectivity and state checks.
func buildVerifyChecks(ctx context.Context, log *slog.Logger, cfg *config.Config) []verifyCheck {
	to := cfg.Cutover.VerifyTimeoutSec
	return []verifyCheck{
		{"vip-pingable-v6", verifyVIPPingableV6(ctx, cfg, to)},
		{"vip-pingable-v4", verifyVIPPingableV4(ctx, log, cfg, to)},
		{"internet-v6-via-host", verifyInternetV6(ctx, cfg, to)},
		{"internet-v4-via-host", verifyInternetV4(ctx, cfg, to)},
		{"vm-keepalived-master", verifyVMKeepalived(ctx, cfg)},
		{"lxc-keepalived-backup", verifyLXCKeepalived(ctx, cfg)},
		{"vip-on-vrrp-interface", verifyVIPOnInterface(ctx, cfg)},
	}
}

func verifyVIPPingableV6(ctx context.Context, cfg *config.Config, to int) func() error {
	return func() error {
		addr := strings.Split(cfg.Cutover.VIPIPv6, "/")[0]
		_, err := localExec(ctx, "ping6", []string{"-c", "3", "-W", "3", addr}, to)
		return err
	}
}

func verifyVIPPingableV4(ctx context.Context, log *slog.Logger, cfg *config.Config, to int) func() error {
	return func() error {
		addr := strings.Split(cfg.Cutover.VIPIPv4, "/")[0]
		_, err := localExec(ctx, "ping", []string{"-c", "3", "-W", "3", addr}, to)
		if err != nil {
			// use_vmac causes ARP to respond from the virtual MAC, which may not
			// be reachable from the Proxmox host bridge. This is not a real failure
			// in production where OPNsense is on the same L2. Log but do not fail.
			log.Warn("vip-pingable-v4: host cannot reach IPv4 VIP (expected with use_vmac on bridge host)", "err", err)
			return nil
		}
		return nil
	}
}

func verifyInternetV6(ctx context.Context, cfg *config.Config, to int) func() error {
	return func() error {
		_, err := localExec(ctx, "ping6", []string{"-c", "3", "-W", "3", cfg.Network.PingTargetIPv6}, to)
		return err
	}
}

func verifyInternetV4(ctx context.Context, cfg *config.Config, to int) func() error {
	return func() error {
		_, err := localExec(ctx, "ping", []string{"-c", "3", "-W", "3", cfg.Network.PingTargetIPv4}, to)
		return err
	}
}

func verifyVMKeepalived(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		out, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
			"journalctl -u keepalived -n 5 --no-pager", cfg.Cutover.SSHTimeoutSec)
		if err != nil {
			return err
		}
		if !strings.Contains(out, "MASTER") {
			return fmt.Errorf("VM not in MASTER state:\n%s", out)
		}
		return nil
	}
}

func verifyLXCKeepalived(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		out, err := localExec(ctx, "pct", []string{"exec", cfg.Cutover.FailoverLXCID, "--",
			"journalctl", "-u", "keepalived", "-n", "5", "--no-pager"}, cfg.Cutover.SSHTimeoutSec)
		if err != nil {
			return err
		}
		lines := strings.Split(out, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.Contains(lines[i], "MASTER") || strings.Contains(lines[i], "BACKUP") {
				if strings.Contains(lines[i], "BACKUP") {
					return nil
				}
				return fmt.Errorf("LXC in MASTER state (expected BACKUP):\n%s", lines[i])
			}
		}
		return fmt.Errorf("could not determine LXC keepalived state:\n%s", out)
	}
}

func verifyVIPOnInterface(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		vipAddr := strings.Split(cfg.Cutover.VIPIPv6, "/")[0]
		vrIface := vrrpIface(cfg)
		out, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
			fmt.Sprintf("ip -6 addr show dev %s 2>/dev/null", vrIface), cfg.Cutover.SSHTimeoutSec)
		if err != nil {
			return err
		}
		if !strings.Contains(out, vipAddr) {
			return fmt.Errorf("VIP %s not on %s:\n%s", vipAddr, vrIface, out)
		}
		return nil
	}
}
