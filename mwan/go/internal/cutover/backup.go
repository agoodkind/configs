package cutover

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"goodkind.io/mwan/internal/config"
)

// StartBackup configures and starts the failover LXC in BACKUP state.
// It handles IP forwarding, masquerade, routing, keepalived deployment, and startup.
func StartBackup(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	return cmdStartBackup(ctx, log, cfg, false)
}

func cmdStartBackup(ctx context.Context, log *slog.Logger, cfg *config.Config, dryRun bool) error {
	if dryRun {
		log.Info("start-backup: DRY RUN — would configure and start LXC failover", "lxc", cfg.Cutover.FailoverLXCID)
		return nil
	}

	lxc := cfg.Cutover.FailoverLXCID
	lxcIface := cfg.Cutover.FailoverLXCIface
	if lxcIface == "" {
		lxcIface = "eth1"
	}
	wanIface := cfg.Cutover.FailoverLXCWanIface
	if wanIface == "" {
		wanIface = "eth0"
	}
	to := cfg.Cutover.SSHTimeoutSec

	if backupAlreadyRunning(ctx, lxc, to, log) {
		return nil
	}

	lxcRun := func(cmd string) (string, error) {
		return localExec(ctx, "pct", []string{"exec", lxc, "--", "bash", "-c", cmd}, to)
	}

	if err := backupConfigureForwarding(lxcRun, lxc, log); err != nil {
		return err
	}
	if err := backupConfigureMasquerade(lxcRun, lxc, wanIface, log); err != nil {
		return err
	}
	if err := backupConfigureRoutes(lxcRun, cfg, lxc, wanIface, lxcIface, log); err != nil {
		return err
	}
	if err := deployKeepalived(ctx, log, cfg, lxcRun, "BACKUP", lxcIface, cfg.Cutover.BackupPriority); err != nil {
		return fmt.Errorf("deploy keepalived on LXC %s: %w", lxc, err)
	}
	return backupStartAndVerify(ctx, lxc, to, log)
}

// backupAlreadyRunning returns true if keepalived is already active in BACKUP state on the LXC.
func backupAlreadyRunning(ctx context.Context, lxc string, to int, log *slog.Logger) bool {
	chkOut, chkErr := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"systemctl", "is-active", "keepalived"}, to)
	if chkErr != nil || strings.TrimSpace(chkOut) != "active" {
		return false
	}
	logOut, _ := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"journalctl", "-u", "keepalived", "-n", "3", "--no-pager"}, to)
	if strings.Contains(logOut, "BACKUP") {
		log.Info("start-backup: already running in BACKUP state, skipping")
		return true
	}
	return false
}

// backupConfigureForwarding enables IPv6 and IPv4 forwarding inside the LXC (persisted via sysctl.d).
func backupConfigureForwarding(lxcRun execFunc, lxc string, log *slog.Logger) error {
	log.Info("start-backup: configuring forwarding on LXC", "lxc", lxc)
	if _, err := lxcRun(`
		sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null
		sysctl -w net.ipv4.ip_forward=1 >/dev/null
		mkdir -p /etc/sysctl.d
		cat > /etc/sysctl.d/99-mwan-failover.conf << '__MWAN_EOF__'
net.ipv6.conf.all.forwarding=1
net.ipv4.ip_forward=1
__MWAN_EOF__
		echo ok
	`); err != nil {
		return fmt.Errorf("configure forwarding on LXC %s: %w", lxc, err)
	}
	return nil
}

// backupConfigureMasquerade sets up nftables masquerade rules on the LXC WAN interface (persisted).
func backupConfigureMasquerade(lxcRun execFunc, lxc, wanIface string, log *slog.Logger) error {
	log.Info("start-backup: configuring masquerade on LXC", "lxc", lxc)
	if _, err := lxcRun(fmt.Sprintf(`
		nft flush ruleset 2>/dev/null
		nft add table ip6 nat
		nft 'add chain ip6 nat postrouting { type nat hook postrouting priority 100; policy accept; }'
		nft add rule ip6 nat postrouting oif %s masquerade
		nft add table ip nat
		nft 'add chain ip nat postrouting { type nat hook postrouting priority 100; policy accept; }'
		nft add rule ip nat postrouting oif %s masquerade
		nft list ruleset > /etc/nftables.conf
		systemctl enable nftables 2>/dev/null
		echo ok
	`, wanIface, wanIface)); err != nil {
		return fmt.Errorf("configure masquerade on LXC %s: %w", lxc, err)
	}
	return nil
}

// backupConfigureRoutes writes and executes the persistent route script on the LXC.
func backupConfigureRoutes(lxcRun execFunc, cfg *config.Config, lxc, wanIface, lxcIface string, log *slog.Logger) error {
	log.Info("start-backup: configuring routes on LXC", "lxc", lxc)
	routeScript := buildRouteScript(cfg, wanIface, lxcIface)
	if _, err := lxcRun(fmt.Sprintf(`
		cat > /etc/network/if-up.d/mwan-failover << '__MWAN_EOF__'
%s
__MWAN_EOF__
		chmod +x /etc/network/if-up.d/mwan-failover
		/etc/network/if-up.d/mwan-failover
		echo ok
	`, routeScript)); err != nil {
		return fmt.Errorf("configure routes on LXC %s: %w", lxc, err)
	}
	return nil
}

// backupStartAndVerify enables keepalived on the LXC and waits for it to enter BACKUP state.
func backupStartAndVerify(ctx context.Context, lxc string, to int, log *slog.Logger) error {
	log.Info("start-backup: enabling and starting keepalived on LXC", "lxc", lxc)
	if _, err := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"systemctl", "enable", "--now", "keepalived"}, to); err != nil {
		return fmt.Errorf("start keepalived on LXC %s: %w", lxc, err)
	}

	log.Info("start-backup: waiting for BACKUP state")
	time.Sleep(3 * time.Second)
	out, err := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"journalctl", "-u", "keepalived", "-n", "5", "--no-pager"}, to)
	if err != nil {
		return fmt.Errorf("check keepalived state on LXC %s: %w", lxc, err)
	}
	if !strings.Contains(out, "BACKUP") {
		return fmt.Errorf("LXC %s did not enter BACKUP state:\n%s", lxc, out)
	}

	log.Info("start-backup: LXC fully configured and in BACKUP state", "lxc", lxc)
	return nil
}

// buildRouteScript generates the persistent route script for the failover LXC.
func buildRouteScript(cfg *config.Config, wanIface, intIface string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Managed by mwan cutover. Do not edit.\n\n")

	// Default route via Monkeybrains
	fmt.Fprintf(&b, "ip -6 route replace default via %s dev %s 2>/dev/null\n", cfg.Cutover.FailoverDefaultGW6, wanIface)
	if cfg.Cutover.FailoverDefaultGW4 != "" {
		fmt.Fprintf(&b, "ip -4 route replace default via %s dev %s 2>/dev/null\n", cfg.Cutover.FailoverDefaultGW4, wanIface)
	}

	// Internal return route
	if cfg.Cutover.FailoverInternalPfx != "" && cfg.Cutover.FailoverOPNsenseLL != "" {
		fmt.Fprintf(&b, "\n# Internal return route (LAN clients via OPNsense)\n")
		fmt.Fprintf(&b, "ip -6 route replace %s via %s dev %s 2>/dev/null\n", cfg.Cutover.FailoverInternalPfx, cfg.Cutover.FailoverOPNsenseLL, intIface)
	}

	// IPv4 return route
	if cfg.Cutover.FailoverIPv4Return != "" {
		parts := strings.SplitN(cfg.Cutover.FailoverIPv4Return, " via ", 2)
		if len(parts) == 2 {
			fmt.Fprintf(&b, "ip -4 route replace %s via %s dev %s 2>/dev/null\n", parts[0], parts[1], intIface)
		}
	}

	// IPv4 VIP on vmac
	fmt.Fprintf(&b, "\n# IPv4 VIP (keepalived VRRPv3 only adds IPv6)\n")
	fmt.Fprintf(&b, "ip addr replace %s dev %s 2>/dev/null || true\n", cfg.Cutover.VIPIPv4, vrrpIface(cfg))

	return b.String()
}
