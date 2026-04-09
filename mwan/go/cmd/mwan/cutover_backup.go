package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const backupKeepaliveConf = `vrrp_script chk_internet {
    script "/etc/keepalived/check_internet.sh"
    interval %d
    weight %d
    fall %d
    rise %d
}

vrrp_instance VI_HA {
    state BACKUP
    interface %s
    virtual_router_id %d
    priority %d
    advert_int %d
    use_vmac vrrp.%d
    vmac_xmit_base
    virtual_ipaddress {
        %s
    }
    track_script {
        chk_internet
    }
    notify /etc/keepalived/notify.sh
}
`

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
	lxcExec := func(cmd string) (string, error) {
		return localExec(ctx, "pct", []string{"exec", lxc, "--", "bash", "-c", cmd}, to)
	}

	// =========================================================================
	// Step 1: Configure IP forwarding (persisted)
	// =========================================================================
	log.Info("start-backup: configuring forwarding on LXC", "lxc", lxc)
	_, err := lxcExec(`
		sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null
		sysctl -w net.ipv4.ip_forward=1 >/dev/null
		mkdir -p /etc/sysctl.d
		cat > /etc/sysctl.d/99-mwan-failover.conf << 'SYSEOF'
net.ipv6.conf.all.forwarding=1
net.ipv4.ip_forward=1
SYSEOF
		echo ok
	`)
	if err != nil {
		return fmt.Errorf("configure forwarding on LXC %s: %w", lxc, err)
	}

	// =========================================================================
	// Step 2: Configure nftables masquerade (persisted)
	// =========================================================================
	log.Info("start-backup: configuring masquerade on LXC", "lxc", lxc)
	_, err = lxcExec(fmt.Sprintf(`
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
	`, wanIface, wanIface))
	if err != nil {
		return fmt.Errorf("configure masquerade on LXC %s: %w", lxc, err)
	}

	// =========================================================================
	// Step 3: Configure routes (persisted via if-up script)
	// =========================================================================
	log.Info("start-backup: configuring routes on LXC", "lxc", lxc)

	routeScript := fmt.Sprintf(`#!/bin/sh
# Managed by mwan cutover. Do not edit.
# Routes for MWAN failover LXC.

# Default route via Monkeybrains (WAN)
ip -6 route replace default via %s dev %s 2>/dev/null
%s

# Internal return routes (so replies to LAN clients go back via OPNsense)
ip -6 route replace %s via %s dev %s 2>/dev/null

# IPv4 return route
%s

# IPv4 VIP on vmac (keepalived VRRPv3 only adds IPv6)
ip addr replace %s dev vrrp.%d 2>/dev/null || true
`,
		cfg.FailoverDefaultGW6, wanIface,
		func() string {
			if cfg.FailoverDefaultGW4 != "" {
				return fmt.Sprintf("ip -4 route replace default via %s dev %s 2>/dev/null", cfg.FailoverDefaultGW4, wanIface)
			}
			return "# no IPv4 default gateway configured"
		}(),
		cfg.FailoverInternalPfx, cfg.FailoverOPNsenseLL, lxcIface,
		func() string {
			if cfg.FailoverIPv4Return != "" {
				parts := strings.SplitN(cfg.FailoverIPv4Return, " via ", 2)
				if len(parts) == 2 {
					return fmt.Sprintf("ip -4 route replace %s via %s dev %s 2>/dev/null", parts[0], parts[1], lxcIface)
				}
			}
			return "# no IPv4 return route configured"
		}(),
		cfg.VIPIPv4, cfg.VRID)

	_, err = lxcExec(fmt.Sprintf(`
		cat > /etc/network/if-up.d/mwan-failover << 'RTEOF'
%s
RTEOF
		chmod +x /etc/network/if-up.d/mwan-failover
		/etc/network/if-up.d/mwan-failover
		echo ok
	`, routeScript))
	if err != nil {
		return fmt.Errorf("configure routes on LXC %s: %w", lxc, err)
	}

	// =========================================================================
	// Step 4: Deploy keepalived scripts (health check + notify)
	// =========================================================================
	log.Info("start-backup: writing health check script on LXC", "lxc", lxc)
	checkScript, err := renderScript(checkInternetTmpl, cfg)
	if err != nil {
		return fmt.Errorf("render check_internet.sh: %w", err)
	}
	_, err = lxcExec(fmt.Sprintf("cat > /etc/keepalived/check_internet.sh << 'CKEOF'\n%sCKEOF\nchmod +x /etc/keepalived/check_internet.sh", checkScript))
	if err != nil {
		return fmt.Errorf("write check_internet.sh on LXC %s: %w", lxc, err)
	}

	log.Info("start-backup: writing notify script on LXC", "lxc", lxc)
	notifyScript, err := renderScript(notifyTmpl, cfg)
	if err != nil {
		return fmt.Errorf("render notify.sh: %w", err)
	}
	_, err = lxcExec(fmt.Sprintf("cat > /etc/keepalived/notify.sh << 'NSEOF'\n%sNSEOF\nchmod +x /etc/keepalived/notify.sh", notifyScript))
	if err != nil {
		return fmt.Errorf("write notify.sh on LXC %s: %w", lxc, err)
	}

	// =========================================================================
	// Step 5: Write keepalived config and enable
	// =========================================================================
	log.Info("start-backup: writing keepalived config on LXC", "lxc", lxc)
	conf := fmt.Sprintf(backupKeepaliveConf,
		cfg.HealthCheckInterval, cfg.HealthCheckWeight, cfg.HealthCheckFall, cfg.HealthCheckRise,
		lxcIface, cfg.VRID, cfg.BackupPriority, cfg.AdvertInterval, cfg.VRID,
		cfg.VIPIPv6)

	_, err = lxcExec(fmt.Sprintf("cat > /etc/keepalived/keepalived.conf << 'KAEOF'\n%sKAEOF", conf))
	if err != nil {
		return fmt.Errorf("write keepalived config on LXC %s: %w", lxc, err)
	}

	log.Info("start-backup: enabling and starting keepalived on LXC", "lxc", lxc)
	_, err = localExec(ctx, "pct", []string{"exec", lxc, "--",
		"systemctl", "enable", "--now", "keepalived"}, to)
	if err != nil {
		return fmt.Errorf("start keepalived on LXC %s: %w", lxc, err)
	}

	// =========================================================================
	// Step 6: Wait for BACKUP state
	// =========================================================================
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
