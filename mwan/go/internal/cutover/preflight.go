package cutover

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
)

func cmdPreflight(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("preflight: checking all preconditions")

	checks := []struct {
		name string
		fn   func(context.Context, *slog.Logger, *config.Config) error
	}{
		{"vm-113-running", preflightVMRunning},
		{"vm-113-ssh", preflightVMSSH},
		{"vm-113-interface", preflightVMInterface},
		{"vm-113-current-addr", preflightCurrentAddr},
		{"vm-113-keepalived-installed", preflightVMKeepalived},
		{"vm-113-no-keepalived-running", preflightNoKeepalived},
		{"opnsense-ssh", preflightOPNsenseSSH},
		{"nftables-forward-vrrp", preflightNftForwardVRRP},
		{"host-ipv6-forwarding", preflightHostForwarding},
		{"host-ipv4-forwarding", preflightHostIPv4Forwarding},
		{"lxc-failover-running", preflightLXCRunning},
		{"lxc-failover-keepalived-installed", preflightLXCKeepalived},
		{"lxc-failover-forwarding", preflightLXCForwarding},
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

func preflightVMRunning(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	out, err := localExec(ctx, "qm", []string{"status", cfg.MwanVMID}, cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "running") {
		return fmt.Errorf("VM %s is not running: %s", cfg.MwanVMID, out)
	}
	return nil
}

func preflightVMSSH(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	_, err := sshMustExec(ctx, cfg.MwanMgmtAddr, "hostname", cfg.Cutover.SSHTimeoutSec)
	return err
}

func preflightVMInterface(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	out, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
		fmt.Sprintf("ip link show %s", cfg.MwanIntIface), cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "UP") {
		return fmt.Errorf("interface %s not UP: %s", cfg.MwanIntIface, out)
	}
	return nil
}

func preflightVMKeepalived(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	out, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
		"dpkg -l keepalived 2>/dev/null | grep -c '^ii'", cfg.Cutover.SSHTimeoutSec)
	if err != nil || strings.TrimSpace(out) != "1" {
		return fmt.Errorf("keepalived not installed on VM %s (run: apt-get install -y keepalived)", cfg.MwanVMID)
	}
	// Also verify /etc/keepalived/ exists
	_, err = sshMustExec(ctx, cfg.MwanMgmtAddr, "test -d /etc/keepalived", cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("/etc/keepalived/ does not exist on VM %s", cfg.MwanVMID)
	}
	return nil
}

func preflightCurrentAddr(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	out, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
		fmt.Sprintf("ip -6 addr show dev %s scope global", cfg.MwanIntIface), cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return err
	}

	preAddr := strings.Split(cfg.Cutover.CurrentRealIPv6, "/")[0]
	postAddr := strings.Split(cfg.Cutover.NewRealIPv6, "/")[0]

	if strings.Contains(out, preAddr) {
		log.Info("pre-cutover address found (ready for cutover)", "addr", preAddr)
		return nil
	}
	if strings.Contains(out, postAddr) {
		log.Info("post-cutover address found (cutover already completed)", "addr", postAddr)
		return nil
	}
	return fmt.Errorf("neither pre-cutover (%s) nor post-cutover (%s) address found on %s:\n%s",
		preAddr, postAddr, cfg.MwanIntIface, out)
}

func preflightNoKeepalived(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	r, err := sshExec(ctx, cfg.MwanMgmtAddr, "systemctl is-active keepalived", cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return err
	}
	if strings.Contains(r.Stdout, "active") && !strings.Contains(r.Stdout, "inactive") {
		log.Info("keepalived already running (cutover already completed)", "vmid", cfg.MwanVMID)
	}
	return nil
}

func preflightLXCRunning(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	out, err := localExec(ctx, "pct", []string{"status", cfg.Cutover.FailoverLXCID}, cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("LXC %s not found: %w", cfg.Cutover.FailoverLXCID, err)
	}
	if !strings.Contains(out, "running") {
		return fmt.Errorf("LXC %s is not running (status: %s). Start it first: pct start %s",
			cfg.Cutover.FailoverLXCID, out, cfg.Cutover.FailoverLXCID)
	}
	log.Debug("lxc status", "output", out)
	return nil
}

func preflightLXCKeepalived(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	out, err := localExec(ctx, "pct", []string{
		"exec", cfg.Cutover.FailoverLXCID, "--",
		"dpkg", "-l", "keepalived",
	}, cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("keepalived not installed on LXC %s: %w", cfg.Cutover.FailoverLXCID, err)
	}
	if !strings.Contains(out, "keepalived") {
		return fmt.Errorf("keepalived package not found on LXC %s", cfg.Cutover.FailoverLXCID)
	}
	return nil
}

func preflightOPNsenseSSH(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	if cfg.Cutover.OPNsenseAddr == "" {
		log.Warn("preflight: no opnsense_addr configured, skipping SSH check")
		return nil
	}
	_, err := sshMustExec(ctx, cfg.Cutover.OPNsenseAddr, "echo ok", cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("cannot SSH to OPNsense at %s (needed for NDP/ARP flush): %w", cfg.Cutover.OPNsenseAddr, err)
	}
	return nil
}

func preflightNftForwardVRRP(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	// Check if the mwan VM's nftables forward chain allows vrrp.* interfaces
	out, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
		"nft list chain inet filter forward 2>/dev/null || echo NO_CHAIN", cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("cannot check nftables forward chain: %w", err)
	}
	if strings.Contains(out, "NO_CHAIN") {
		// No forward chain = no restrictive rules = OK
		return nil
	}
	if strings.Contains(out, "policy accept") {
		return nil
	}
	// Forward chain exists with non-accept policy — must have vrrp.* rules
	if !strings.Contains(out, "vrrp") {
		return fmt.Errorf("nftables forward chain has policy drop but no vrrp.* rules. "+
			"Run on mwan: nft insert rule inet filter forward iifname \"vrrp.*\" oifname { WAN_IFACES } accept\n"+
			"Current chain:\n%s", out)
	}
	return nil
}

func preflightHostForwarding(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	// Host-side IPv6 forwarding is REQUIRED for LXC failover.
	// Without it, veth peers drop forwarded packets silently.
	checks := []struct {
		sysctl string
		want   string
	}{
		{"net.ipv6.conf.all.forwarding", "1"},
		{"net.ipv6.conf.default.forwarding", "1"},
	}
	for _, c := range checks {
		out, err := localExec(ctx, "sysctl", []string{"-n", c.sysctl}, cfg.Cutover.SSHTimeoutSec)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", c.sysctl, err)
		}
		if strings.TrimSpace(out) != c.want {
			// Fix it
			log.Warn("preflight: fixing host sysctl", "sysctl", c.sysctl, "was", strings.TrimSpace(out), "setting", c.want)
			_, err = localExec(ctx, "sysctl", []string{"-w", fmt.Sprintf("%s=%s", c.sysctl, c.want)}, cfg.Cutover.SSHTimeoutSec)
			if err != nil {
				return fmt.Errorf("failed to set %s=%s: %w", c.sysctl, c.want, err)
			}
		}
	}
	return nil
}

func preflightHostIPv4Forwarding(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	out, err := localExec(ctx, "sysctl", []string{"-n", "net.ipv4.ip_forward"}, cfg.Cutover.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("cannot read net.ipv4.ip_forward: %w", err)
	}
	if strings.TrimSpace(out) != "1" {
		log.Warn("preflight: fixing host IPv4 forwarding", "was", strings.TrimSpace(out))
		_, err = localExec(ctx, "sysctl", []string{"-w", "net.ipv4.ip_forward=1"}, cfg.Cutover.SSHTimeoutSec)
		if err != nil {
			return fmt.Errorf("failed to set net.ipv4.ip_forward=1: %w", err)
		}
	}
	return nil
}

func preflightLXCForwarding(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	lxc := cfg.Cutover.FailoverLXCID
	sysctls := []struct {
		name string
		want string
	}{
		{"net.ipv6.conf.all.forwarding", "1"},
		{"net.ipv4.ip_forward", "1"},
	}
	for _, s := range sysctls {
		out, err := localExec(ctx, "pct", []string{
			"exec", lxc, "--",
			"sysctl", "-n", s.name,
		}, cfg.Cutover.SSHTimeoutSec)
		if err != nil {
			return fmt.Errorf("cannot read %s on LXC %s: %w", s.name, lxc, err)
		}
		if strings.TrimSpace(out) != s.want {
			log.Warn("preflight: fixing LXC sysctl", "lxc", lxc, "sysctl", s.name, "was", strings.TrimSpace(out))
			_, err = localExec(ctx, "pct", []string{
				"exec", lxc, "--",
				"sysctl", "-w", fmt.Sprintf("%s=%s", s.name, s.want),
			}, cfg.Cutover.SSHTimeoutSec)
			if err != nil {
				return fmt.Errorf("failed to set %s=%s on LXC %s: %w", s.name, s.want, lxc, err)
			}
		}
	}
	return nil
}

func preflightInternet(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	_, err := localExec(ctx, "ping6",
		[]string{"-c", "1", "-W", "3", cfg.Network.PingTargetIPv6}, cfg.Cutover.SSHTimeoutSec)
	return err
}

func preflightEmail(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	sender := email.NewSender(cfg.Email.SMTP2GOAPIKey, cfg.Email.From, cfg.Email.BindIface, "mwan-cutover", log)
	return sender.Send(ctx, cfg.Email.AlertEmail,
		fmt.Sprintf("%s Preflight email test", cfg.Email.SubjectPrefix),
		"This is a preflight test email from mwan-cutover. If you received this, email is working.")
}
