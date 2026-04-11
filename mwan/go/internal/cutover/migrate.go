package cutover

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"goodkind.io/mwan/internal/config"
)

func cmdMigrate(ctx context.Context, log *slog.Logger, cfg *config.Config, dryRun bool) error {
	if dryRun {
		log.Info("migrate: DRY RUN")
		log.Info("would add new real address", "addr", cfg.Cutover.NewRealIPv6, "iface", cfg.MwanIntIface)
		log.Info("would deploy keepalived scripts and config")
		log.Info("would wait for VIP on " + vrrpIface(cfg))
		log.Info("would remove old real address", "addr", cfg.Cutover.CurrentRealIPv6, "iface", cfg.MwanIntIface)
		return nil
	}

	host := cfg.MwanMgmtAddr
	iface := cfg.MwanIntIface
	to := cfg.Cutover.SSHTimeoutSec
	vrIface := vrrpIface(cfg)

	if migrateAlreadyDone(ctx, host, vrIface, cfg, to, log) {
		return nil
	}

	if err := migrateDeployKeepalived(ctx, log, cfg, host, iface, to); err != nil {
		return err
	}

	if err := migrateAddNewAddresses(ctx, log, cfg, host, iface, to); err != nil {
		return err
	}

	if err := migrateStartKeepalived(ctx, log, host, to); err != nil {
		return err
	}

	if err := migrateWaitForVIP(ctx, log, cfg, host, vrIface, to); err != nil {
		return err
	}

	migrateRemoveOldAddresses(ctx, log, cfg, host, iface, to)
	migratePersistAddressChange(ctx, log, cfg, host, to)
	migrateWriteTimestamp(ctx, log, host, to)

	return migrateVerifyMaster(ctx, log, host, to)
}

// migrateAlreadyDone returns true if the VIP is already on the vmac and keepalived is MASTER.
func migrateAlreadyDone(ctx context.Context, host, vrIface string, cfg *config.Config, to int, log *slog.Logger) bool {
	chk, chkErr := sshExec(ctx, host, fmt.Sprintf("ip -6 addr show dev %s 2>/dev/null", vrIface), to)
	if chkErr != nil {
		return false
	}
	vipAddr := strings.Split(cfg.Cutover.VIPIPv6, "/")[0]
	if !strings.Contains(chk.Stdout, vipAddr) {
		return false
	}
	kaChk, _ := sshExec(ctx, host, "journalctl -u keepalived -n 3 --no-pager", to)
	if strings.Contains(kaChk.Stdout, "MASTER") {
		log.Info("migrate: already migrated, skipping")
		return true
	}
	return false
}

// migrateDeployKeepalived deploys keepalived scripts and config to the VM via SSH.
func migrateDeployKeepalived(ctx context.Context, log *slog.Logger, cfg *config.Config, host, iface string, to int) error {
	sshRun := func(cmd string) (string, error) {
		return sshMustExec(ctx, host, cmd, to)
	}
	if err := deployKeepalived(ctx, log, cfg, sshRun, "MASTER", iface, cfg.Cutover.MasterPriority); err != nil {
		return fmt.Errorf("deploy keepalived on VM: %w", err)
	}
	return nil
}

// migrateAddNewAddresses adds the new real IPv6 and IPv4 addresses alongside the existing ones.
func migrateAddNewAddresses(ctx context.Context, log *slog.Logger, cfg *config.Config, host, iface string, to int) error {
	log.Info("migrate: adding new real address", "addr", cfg.Cutover.NewRealIPv6)
	if _, err := sshMustExec(ctx, host, fmt.Sprintf("ip -6 addr add %s dev %s nodad", cfg.Cutover.NewRealIPv6, iface), to); err != nil {
		return fmt.Errorf("add new real v6: %w", err)
	}
	if _, err := sshMustExec(ctx, host, fmt.Sprintf("ip addr add %s dev %s", cfg.Cutover.NewRealIPv4, iface), to); err != nil {
		log.Warn("add new real v4 (may already exist)", "err", err)
	}
	return nil
}

// migrateStartKeepalived enables and starts the keepalived service on the VM.
func migrateStartKeepalived(ctx context.Context, log *slog.Logger, host string, to int) error {
	log.Info("migrate: enabling and starting keepalived")
	if _, err := sshMustExec(ctx, host, "systemctl enable --now keepalived", to); err != nil {
		return fmt.Errorf("start keepalived: %w", err)
	}
	return nil
}

// migrateWaitForVIP waits for the IPv6 VIP to appear on the vmac interface and ensures IPv4 VIP is present.
func migrateWaitForVIP(ctx context.Context, log *slog.Logger, cfg *config.Config, host, vrIface string, to int) error {
	log.Info("migrate: waiting for VIP on " + vrIface)
	vipAddr := strings.Split(cfg.Cutover.VIPIPv6, "/")[0]
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

	// Wait for notify script to add IPv4 VIP
	log.Info("migrate: waiting for notify script to add IPv4 VIP")
	time.Sleep(2 * time.Second)
	v4Addr := strings.Split(cfg.Cutover.VIPIPv4, "/")[0]
	v4Out, _ := sshExec(ctx, host, fmt.Sprintf("ip -4 addr show dev %s 2>/dev/null", vrIface), to)
	if !strings.Contains(v4Out.Stdout, v4Addr) {
		log.Warn("migrate: IPv4 VIP not on vmac after notify, adding manually")
		if _, err := sshExec(ctx, host, fmt.Sprintf("ip addr replace %s dev %s", cfg.Cutover.VIPIPv4, vrIface), to); err != nil {
			log.Warn("migrate: failed to add IPv4 VIP manually", "err", err)
		}
	} else {
		log.Info("migrate: IPv4 VIP confirmed (added by notify script)")
	}

	return nil
}

// migrateRemoveOldAddresses removes the old real IPv6 and IPv4 addresses from the physical interface.
func migrateRemoveOldAddresses(ctx context.Context, log *slog.Logger, cfg *config.Config, host, iface string, to int) {
	log.Info("migrate: removing old real addresses from physical interface")
	if r, err := sshExec(ctx, host, fmt.Sprintf("ip -6 addr del %s dev %s", cfg.Cutover.CurrentRealIPv6, iface), to); err != nil || r.ExitCode != 0 {
		log.Warn("migrate: failed to remove old IPv6 (may already be gone)", "err", err, "stderr", r.Stderr)
	}
	if r, err := sshExec(ctx, host, fmt.Sprintf("ip addr del %s dev %s", cfg.Cutover.CurrentRealIPv4, iface), to); err != nil || r.ExitCode != 0 {
		log.Warn("migrate: failed to remove old IPv4 (may already be gone)", "err", err, "stderr", r.Stderr)
	}
}

// migratePersistAddressChange updates networkd config files to use the new addresses.
func migratePersistAddressChange(ctx context.Context, log *slog.Logger, cfg *config.Config, host string, to int) {
	log.Info("migrate: persisting address change in networkd config")
	persistCmd := fmt.Sprintf(
		"sed -i 's|%s|%s|g' /etc/systemd/network/*mwanbr* /etc/systemd/network/*internal* 2>/dev/null; "+
			"sed -i 's|%s|%s|g' /etc/systemd/network/*mwanbr* /etc/systemd/network/*internal* 2>/dev/null; echo ok",
		strings.Split(cfg.Cutover.CurrentRealIPv6, "/")[0], strings.Split(cfg.Cutover.NewRealIPv6, "/")[0],
		strings.Split(cfg.Cutover.CurrentRealIPv4, "/")[0], strings.Split(cfg.Cutover.NewRealIPv4, "/")[0])
	if r, err := sshExec(ctx, host, persistCmd, to); err != nil || r.ExitCode != 0 {
		log.Warn("migrate: failed to persist address change (manual update needed)", "err", err)
	}
}

// migrateWriteTimestamp writes a deploy timestamp to the VM for the watchdog.
func migrateWriteTimestamp(ctx context.Context, log *slog.Logger, host string, to int) {
	log.Info("migrate: writing deploy timestamp to VM")
	_, _ = sshExec(ctx, host, "date +%s > /var/run/mwan-last-deploy", to)
}

// migrateVerifyMaster checks that keepalived reached MASTER state.
func migrateVerifyMaster(ctx context.Context, log *slog.Logger, host string, to int) error {
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
