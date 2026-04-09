package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func cmdStartBackup(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	if cfg.DryRun {
		log.Info("start-backup: DRY RUN — would configure and start LXC failover", "lxc", cfg.FailoverLXCID)
		return nil
	}

	lxc := cfg.FailoverLXCID
	lxcIface := cfg.FailoverLXCIface
	if lxcIface == "" {
		lxcIface = "eth1"
	}
	wanIface := cfg.FailoverLXCWanIface
	if wanIface == "" {
		wanIface = "eth0"
	}
	to := cfg.SSHTimeoutSec

	// Idempotency: if keepalived is already active in BACKUP state, skip
	if chkOut, chkErr := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"systemctl", "is-active", "keepalived"}, to); chkErr == nil && strings.TrimSpace(chkOut) == "active" {
		if logOut, _ := localExec(ctx, "pct", []string{"exec", lxc, "--",
			"journalctl", "-u", "keepalived", "-n", "3", "--no-pager"}, to); strings.Contains(logOut, "BACKUP") {
			log.Info("start-backup: already running in BACKUP state, skipping")
			return nil
		}
	}

	// Helper to exec inside LXC
	lxcRun := func(cmd string) (string, error) {
		return localExec(ctx, "pct", []string{"exec", lxc, "--", "bash", "-c", cmd}, to)
	}

	// Step 1: Configure IP forwarding (persisted)
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

	// Step 2: Configure nftables masquerade (persisted)
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

	// Step 3: Configure routes (persisted via if-up script)
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

	// Step 4: Deploy keepalived scripts and config (shared with migrate)
	if err := deployKeepalived(ctx, log, cfg, lxcRun, "BACKUP", lxcIface, cfg.BackupPriority); err != nil {
		return fmt.Errorf("deploy keepalived on LXC %s: %w", lxc, err)
	}

	// Step 5: Enable and start keepalived
	log.Info("start-backup: enabling and starting keepalived on LXC", "lxc", lxc)
	if _, err := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"systemctl", "enable", "--now", "keepalived"}, to); err != nil {
		return fmt.Errorf("start keepalived on LXC %s: %w", lxc, err)
	}

	// Step 6: Wait for BACKUP state
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
func buildRouteScript(cfg *CutoverConfig, wanIface, intIface string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Managed by mwan cutover. Do not edit.\n\n")

	// Default route via Monkeybrains
	fmt.Fprintf(&b, "ip -6 route replace default via %s dev %s 2>/dev/null\n", cfg.FailoverDefaultGW6, wanIface)
	if cfg.FailoverDefaultGW4 != "" {
		fmt.Fprintf(&b, "ip -4 route replace default via %s dev %s 2>/dev/null\n", cfg.FailoverDefaultGW4, wanIface)
	}

	// Internal return route
	if cfg.FailoverInternalPfx != "" && cfg.FailoverOPNsenseLL != "" {
		fmt.Fprintf(&b, "\n# Internal return route (LAN clients via OPNsense)\n")
		fmt.Fprintf(&b, "ip -6 route replace %s via %s dev %s 2>/dev/null\n", cfg.FailoverInternalPfx, cfg.FailoverOPNsenseLL, intIface)
	}

	// IPv4 return route
	if cfg.FailoverIPv4Return != "" {
		parts := strings.SplitN(cfg.FailoverIPv4Return, " via ", 2)
		if len(parts) == 2 {
			fmt.Fprintf(&b, "ip -4 route replace %s via %s dev %s 2>/dev/null\n", parts[0], parts[1], intIface)
		}
	}

	// IPv4 VIP on vmac
	fmt.Fprintf(&b, "\n# IPv4 VIP (keepalived VRRPv3 only adds IPv6)\n")
	fmt.Fprintf(&b, "ip addr replace %s dev %s 2>/dev/null || true\n", cfg.VIPIPv4, vrrpIface(cfg))

	return b.String()
}
