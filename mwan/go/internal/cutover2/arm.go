package cutover2

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/ops"
)

// armWatchdogPingTimeout caps each ICMP probe inside arm-watchdog.
const armWatchdogPingTimeout = 5 * time.Second

// cmdArmWatchdog finalizes a successful BGP cutover. It runs end-to-end
// connectivity probes through the (now-BGP) data path, takes a fresh
// PVE snapshot of the MWAN VM as the new known-good baseline, then
// re-enables mwan-watchdog so auto-rollback is armed against this new
// baseline. Intended to be run AFTER switch-to-bgp succeeds and the
// operator has confirmed traffic is flowing as expected.
//
// Why this is a separate subcommand (not embedded in switch-to-bgp):
// re-arming the watchdog is destructive (the watchdog can qm rollback
// the VM to a stale snapshot if anything looks even slightly off). We
// want a deliberate operator gate between "BGP is up" and "auto-rollback
// is armed against this state".
func cmdArmWatchdog(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Arming watchdog: post-cutover snapshot + service start ===")

	if cfg.MwanVMID == "" {
		return fmt.Errorf("mwan_vmid is required")
	}

	// 1. Real connectivity probe through the BGP path. We use the same
	//    well-known targets the watchdog itself will probe so a green
	//    here means the watchdog will be green when it starts.
	log.Info("arm-watchdog: probing connectivity")
	if err := probeArmConnectivity(ctx, log, cfg); err != nil {
		return fmt.Errorf("connectivity probe failed; refusing to arm watchdog: %w", err)
	}

	// 2. Take a fresh PVE snapshot of the MWAN VM as the new
	//    known-good baseline. Without this, watchdog would roll
	//    back to a pre-cutover snapshot if it ever fires.
	snapName := fmt.Sprintf("post-bgp-cutover-%s", time.Now().UTC().Format("20060102-150405"))
	log.Info("arm-watchdog: snapshotting MWAN VM",
		"vmid", cfg.MwanVMID, "snapshot", snapName)
	realOps := ops.NewRealOps(cfg, nil)
	if err := realOps.VMSnapshot(ctx, cfg.MwanVMID, snapName); err != nil {
		return fmt.Errorf("snapshot %s: %w", snapName, err)
	}
	log.Info("arm-watchdog: snapshot created", "snapshot", snapName)

	// 3. Start mwan-watchdog. It will treat the just-taken snapshot as
	//    the new known-good baseline.
	svc := cfg.Watchdog.ServiceName
	if svc == "" {
		svc = "mwan-watchdog"
	}
	log.Info("arm-watchdog: starting watchdog", "service", svc)
	if err := startWatchdogChecked(log, svc); err != nil {
		return fmt.Errorf("start watchdog: %w", err)
	}

	// 4. Verify the service stayed running for a few seconds (catches
	//    a config/startup error that would crash watchdog immediately).
	if err := verifyWatchdogActive(ctx, log, svc); err != nil {
		return fmt.Errorf("verify watchdog active: %w", err)
	}

	log.Info("=== arm-watchdog complete: cutover finalized ===",
		"snapshot", snapName,
	)
	log.Info("disarm: mwan cutover2 disarm-watchdog")
	return nil
}

// cmdDisarmWatchdog stops mwan-watchdog. Used when investigating issues
// or before any subsequent maintenance that would otherwise trigger
// auto-rollback (a second cutover, OPNsense reboot, OPNsense API
// outage, etc.).
func cmdDisarmWatchdog(_ context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Disarming watchdog ===", "service", cfg.Watchdog.ServiceName)
	stopWatchdog(log, cfg.Watchdog.ServiceName)
	log.Info("=== disarm-watchdog complete: auto-rollback is OFF ===")
	log.Info("re-arm: mwan cutover2 arm-watchdog")
	return nil
}

// probeArmConnectivity runs IPv4 + IPv6 ICMP probes to the canonical
// MWAN ping targets. Returns an error on any failure so arm-watchdog
// refuses to take a snapshot of a broken state.
func probeArmConnectivity(
	ctx context.Context, log *slog.Logger, cfg *config.Config,
) error {
	v4 := cfg.Network.PingTargetIPv4
	if v4 == "" {
		v4 = "1.1.1.1"
	}
	v6 := cfg.Network.PingTargetIPv6
	if v6 == "" {
		v6 = "2606:4700:4700::1111"
	}

	if err := pingOrFail(ctx, log, "ping", v4); err != nil {
		return fmt.Errorf("ipv4 ping %s failed: %w", v4, err)
	}
	if err := pingOrFail(ctx, log, "ping", "-6", v6); err != nil {
		return fmt.Errorf("ipv6 ping %s failed: %w", v6, err)
	}
	log.Info("arm-watchdog: connectivity OK", "ipv4_target", v4, "ipv6_target", v6)
	return nil
}

func pingOrFail(
	ctx context.Context, log *slog.Logger, bin string, args ...string,
) error {
	cctx, cancel := context.WithTimeout(ctx, armWatchdogPingTimeout)
	defer cancel()
	full := append([]string{"-c", "3", "-W", "2"}, args...)
	cmd := exec.CommandContext(cctx, bin, full...)
	out, err := cmd.CombinedOutput()
	log.Debug("arm-watchdog: ping",
		"argv", append([]string{bin}, full...),
		"err", err,
		"output", string(out),
	)
	return err
}

// startWatchdogChecked runs `systemctl start <serviceName>` and returns
// an error on failure (unlike startWatchdog in unfuck.go which logs and
// swallows errors as best-effort recovery). Used by arm-watchdog where
// failure is meaningful.
func startWatchdogChecked(log *slog.Logger, serviceName string) error {
	cmd := exec.Command("systemctl", "start", serviceName)
	out, err := cmd.CombinedOutput()
	log.Debug("arm-watchdog: systemctl start",
		"service", serviceName, "err", err, "output", string(out))
	if err != nil {
		return fmt.Errorf("systemctl start %s: %w (output: %s)",
			serviceName, err, string(out))
	}
	return nil
}

// verifyWatchdogActive sleeps briefly then confirms the watchdog
// service is in 'active' state. Catches the common "service starts
// but immediately exits with bad config" failure mode.
func verifyWatchdogActive(ctx context.Context, log *slog.Logger, serviceName string) error {
	const settleDelay = 3 * time.Second
	t := time.NewTimer(settleDelay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
	}

	cmd := exec.Command("systemctl", "is-active", serviceName)
	out, err := cmd.CombinedOutput()
	state := string(out)
	log.Debug("arm-watchdog: systemctl is-active",
		"service", serviceName, "state", state, "err", err)
	if err != nil {
		return fmt.Errorf("watchdog %s not active: %s (err: %w)",
			serviceName, state, err)
	}
	log.Info("arm-watchdog: watchdog is active and healthy",
		"service", serviceName)
	return nil
}
