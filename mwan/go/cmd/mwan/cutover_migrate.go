package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func cmdMigrate(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	if cfg.DryRun {
		log.Info("migrate: DRY RUN")
		log.Info("would add new real address", "addr", cfg.NewRealIPv6, "iface", cfg.MwanIntIface)
		log.Info("would deploy keepalived scripts and config")
		log.Info("would wait for VIP on " + vrrpIface(cfg))
		log.Info("would remove old real address", "addr", cfg.CurrentRealIPv6, "iface", cfg.MwanIntIface)
		return nil
	}

	host := cfg.MwanMgmtAddr
	iface := cfg.MwanIntIface
	to := cfg.SSHTimeoutSec
	vrIface := vrrpIface(cfg)

	// Idempotency: if VIP is already on vmac and keepalived is MASTER, skip
	if chk, chkErr := sshExec(ctx, host, fmt.Sprintf("ip -6 addr show dev %s 2>/dev/null", vrIface), to); chkErr == nil {
		vipAddr := strings.Split(cfg.VIPIPv6, "/")[0]
		if strings.Contains(chk.Stdout, vipAddr) {
			if kaChk, _ := sshExec(ctx, host, "journalctl -u keepalived -n 3 --no-pager", to); strings.Contains(kaChk.Stdout, "MASTER") {
				log.Info("migrate: already migrated, skipping")
				return nil
			}
		}
	}

	// Step 1: Deploy keepalived scripts and config via SSH
	sshRun := func(cmd string) (string, error) {
		return sshMustExec(ctx, host, cmd, to)
	}
	if err := deployKeepalived(ctx, log, cfg, sshRun, "MASTER", iface, cfg.MasterPriority); err != nil {
		return fmt.Errorf("deploy keepalived on VM: %w", err)
	}

	// Step 2: Add new real address alongside existing
	log.Info("migrate: adding new real address", "addr", cfg.NewRealIPv6)
	if _, err := sshMustExec(ctx, host, fmt.Sprintf("ip -6 addr add %s dev %s nodad", cfg.NewRealIPv6, iface), to); err != nil {
		return fmt.Errorf("add new real v6: %w", err)
	}
	if _, err := sshMustExec(ctx, host, fmt.Sprintf("ip addr add %s dev %s", cfg.NewRealIPv4, iface), to); err != nil {
		log.Warn("add new real v4 (may already exist)", "err", err)
	}

	// Step 3: Enable and start keepalived
	log.Info("migrate: enabling and starting keepalived")
	if _, err := sshMustExec(ctx, host, "systemctl enable --now keepalived", to); err != nil {
		return fmt.Errorf("start keepalived: %w", err)
	}

	// Step 4: Wait for VIP on vmac interface
	log.Info("migrate: waiting for VIP on " + vrIface)
	vipAddr := strings.Split(cfg.VIPIPv6, "/")[0]
	deadline := time.Now().Add(10 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		out, _ := sshExec(ctx, host, fmt.Sprintf("ip -6 addr show dev %s 2>/dev/null", vrIface), to)
		if strings.Contains(out.Stdout, vipAddr) {
			found = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !found {
		return fmt.Errorf("VIP %s did not appear on %s within 10s", vipAddr, vrIface)
	}
	log.Info("migrate: VIP confirmed on " + vrIface)

	// Step 4b: Wait for notify script to add IPv4 VIP
	log.Info("migrate: waiting for notify script to add IPv4 VIP")
	time.Sleep(2 * time.Second)
	v4Addr := strings.Split(cfg.VIPIPv4, "/")[0]
	v4Out, _ := sshExec(ctx, host, fmt.Sprintf("ip -4 addr show dev %s 2>/dev/null", vrIface), to)
	if !strings.Contains(v4Out.Stdout, v4Addr) {
		log.Warn("migrate: IPv4 VIP not on vmac after notify, adding manually")
		if _, err := sshExec(ctx, host, fmt.Sprintf("ip addr replace %s dev %s", cfg.VIPIPv4, vrIface), to); err != nil {
			log.Warn("migrate: failed to add IPv4 VIP manually", "err", err)
		}
	} else {
		log.Info("migrate: IPv4 VIP confirmed (added by notify script)")
	}

	// Step 5: Remove old real addresses from the physical interface
	log.Info("migrate: removing old real addresses from physical interface")
	if r, err := sshExec(ctx, host, fmt.Sprintf("ip -6 addr del %s dev %s", cfg.CurrentRealIPv6, iface), to); err != nil || r.ExitCode != 0 {
		log.Warn("migrate: failed to remove old IPv6 (may already be gone)", "err", err, "stderr", r.Stderr)
	}
	if r, err := sshExec(ctx, host, fmt.Sprintf("ip addr del %s dev %s", cfg.CurrentRealIPv4, iface), to); err != nil || r.ExitCode != 0 {
		log.Warn("migrate: failed to remove old IPv4 (may already be gone)", "err", err, "stderr", r.Stderr)
	}

	// Step 5b: Persist address change in networkd config
	log.Info("migrate: persisting address change in networkd config")
	persistCmd := fmt.Sprintf(
		"sed -i 's|%s|%s|g' /etc/systemd/network/*mwanbr* /etc/systemd/network/*internal* 2>/dev/null; "+
			"sed -i 's|%s|%s|g' /etc/systemd/network/*mwanbr* /etc/systemd/network/*internal* 2>/dev/null; echo ok",
		strings.Split(cfg.CurrentRealIPv6, "/")[0], strings.Split(cfg.NewRealIPv6, "/")[0],
		strings.Split(cfg.CurrentRealIPv4, "/")[0], strings.Split(cfg.NewRealIPv4, "/")[0])
	if r, err := sshExec(ctx, host, persistCmd, to); err != nil || r.ExitCode != 0 {
		log.Warn("migrate: failed to persist address change (manual update needed)", "err", err)
	}

	// Step 5c: Write deploy timestamp
	log.Info("migrate: writing deploy timestamp to VM")
	// best-effort: watchdog uses this to know a cutover is in progress
	_, _ = sshExec(ctx, host, "date +%s > /var/run/mwan-last-deploy", to)

	// Step 6: Verify keepalived reached MASTER
	log.Info("migrate: verifying MASTER state")
	time.Sleep(2 * time.Second)
	out, err := sshMustExec(ctx, host, "journalctl -u keepalived -n 5 --no-pager", to)
	if err != nil {
		return fmt.Errorf("check keepalived state: %w", err)
	}
	if !strings.Contains(out, "MASTER") {
		return fmt.Errorf("keepalived did not reach MASTER state:\n%s", out)
	}
	log.Info("migrate: VM is MASTER, VIP migration complete")

	return nil
}
