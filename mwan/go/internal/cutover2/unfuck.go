package cutover2

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/opnsense"
)

// cmdUnfuck is the nuclear rollback: reverse everything cutover2 did.
// Every step is best-effort and continues on failure. The goal is to
// restore the pre-cutover state (static gateway, keepalived VIP) no
// matter what state the system is in.
//
// Order matters: stop BGP announcements first (so OPNsense doesn't keep
// BGP routes), then stop FRR (remove BGP from routing table), then
// re-enable the static gateway so the old path is restored.
func cmdUnfuck(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== UNFUCK: nuclear rollback to pre-cutover state ===")

	if err := validateOPNsenseConfig(cfg); err != nil {
		return err
	}

	client := opnsense.New(opnsense.Config{
		URL:       cfg.OPNsense.URL,
		APIKey:    cfg.OPNsense.APIKey,
		APISecret: cfg.OPNsense.APISecret,
		Insecure:  cfg.OPNsense.Insecure,
	}, log)

	// Step 1: Stop mwan-agent on VM (stops BGP announcements from primary)
	// Uses qm guest exec (QEMU guest agent channel) instead of SSH to avoid
	// the circular dependency where SSH requires the network we're trying to fix.
	vmid := cfg.MwanVMID
	if vmid == "" {
		vmid = fmt.Sprintf("%d", cfg.Watchdog.VsockCID) // CID often matches VMID
	}
	if vmid != "" && vmid != "0" {
		log.Info("unfuck: stopping mwan-agent on VM via guest agent", "vmid", vmid)
		qmBestEffort(ctx, log, vmid, "systemctl stop mwan-agent")
	}

	// Step 2: Stop mwan-agent on LXC (stops BGP announcements from backup)
	if cfg.Cutover.FailoverLXCID != "" {
		log.Info("unfuck: stopping mwan-agent on LXC", "lxc", cfg.Cutover.FailoverLXCID)
		pctBestEffort(ctx, log, cfg.Cutover.FailoverLXCID, "systemctl stop mwan-agent")
	}

	// Step 3: Stop FRR on OPNsense (removes BGP routes from kernel routing table)
	log.Info("unfuck: stopping FRR on OPNsense")
	if err := client.StopFRR(ctx); err != nil {
		log.Error("unfuck: failed to stop FRR (continuing)", "err", err)
	}

	// Step 4: Unforce_down all gateways
	for _, gwName := range cfg.OPNsense.GatewayNames {
		log.Info("unfuck: re-enabling gateway", "name", gwName)
		gwUUID, findErr := client.FindGatewayByName(ctx, gwName)
		if findErr != nil {
			log.Error("unfuck: failed to find gateway (continuing)", "name", gwName, "err", findErr)
			continue
		}
		if err := client.UnforceDownGateway(ctx, gwUUID); err != nil {
			log.Error("unfuck: failed to unforce_down gateway (continuing)", "name", gwName, "err", err)
		}
	}

	// Step 5: Reconfigure routing (reinstalls static default routes)
	log.Info("unfuck: reconfiguring routing")
	if err := client.Reconfigure(ctx); err != nil {
		log.Error("unfuck: failed to reconfigure routing (continuing)", "err", err)
	}

	// Step 6: Restart keepalived on VM (restore VIP)
	if vmid != "" && vmid != "0" {
		log.Info("unfuck: restarting keepalived on VM via guest agent", "vmid", vmid)
		qmBestEffort(ctx, log, vmid, "systemctl restart keepalived")
	}

	// Step 7: Restore gatewayv6 to OPNsense config (removed by switch-to-bgp)
	restoreGatewayV6(ctx, log, cfg)

	// Step 8: Restart watchdog (may have been stopped by switch-to-bgp)
	log.Info("unfuck: restarting mwan-watchdog")
	startWatchdog(log)

	// Step 8: Verify connectivity
	log.Info("unfuck: testing connectivity...")
	if verifyConnectivity(ctx, log) {
		log.Info("=== UNFUCK complete: connectivity restored ===")
	} else {
		log.Error("=== UNFUCK complete but connectivity NOT verified ===")
		log.Error("manual intervention may be needed")
	}

	return nil
}

// pctBestEffort runs a command inside an LXC via pct exec and logs but does not fail.
func pctBestEffort(ctx context.Context, log *slog.Logger, lxcID, cmd string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	parts := strings.Fields(cmd)
	args := append([]string{"exec", lxcID, "--"}, parts...)
	pctCmd := exec.CommandContext(ctx, "pct", args...)
	out, err := pctCmd.CombinedOutput()
	if err != nil {
		log.Error("unfuck: pct exec failed (continuing)", "lxc", lxcID, "cmd", cmd, "err", err, "output", string(out))
	} else {
		log.Info("unfuck: pct exec OK", "lxc", lxcID, "cmd", cmd)
	}
}

// verifyConnectivity tests both IPv4 and IPv6 internet reachability.
func verifyConnectivity(ctx context.Context, log *slog.Logger) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	v4ok := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "3", "1.1.1.1").Run() == nil
	v6ok := exec.CommandContext(ctx, "ping6", "-c", "1", "-W", "3", "2606:4700:4700::1111").Run() == nil

	log.Info("unfuck: connectivity check", "v4", v4ok, "v6", v6ok)
	return v4ok || v6ok
}

// writeDeployTimestamp writes a timestamp to the deploy file so the watchdog
// enters its grace period and doesn't auto-rollback during the cutover.
func writeDeployTimestamp(log *slog.Logger, cfg *config.Config) {
	path := cfg.Network.LastDeployPath
	if path == "" {
		path = "/var/run/mwan-last-deploy"
	}
	ts := fmt.Sprintf("%d", time.Now().Unix())
	if err := exec.Command("sh", "-c", fmt.Sprintf("echo %s > %s", ts, path)).Run(); err != nil {
		log.Warn("failed to write deploy timestamp (watchdog may interfere)", "err", err)
	} else {
		log.Info("deploy timestamp written", "path", path)
	}
}

// qmBestEffort runs a command inside a QEMU VM via the guest agent.
// This bypasses the network entirely (uses UNIX socket on hypervisor).
func qmBestEffort(ctx context.Context, log *slog.Logger, vmid, cmd string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	qmCmd := exec.CommandContext(ctx, "qm", "guest", "exec", vmid, "--", "bash", "-c", cmd)
	out, err := qmCmd.CombinedOutput()
	if err != nil {
		log.Error("unfuck: qm guest exec failed (continuing)", "vmid", vmid, "cmd", cmd, "err", err, "output", string(out))
	} else {
		log.Info("unfuck: qm guest exec OK", "vmid", vmid, "cmd", cmd)
	}
}

// opnsenseSSHHost extracts the SSH host from the OPNsense API URL.
// Safe to SSH because OPNsense is on the LAN bridge, not through MWAN.
func opnsenseSSHHost(cfg *config.Config) string {
	host := strings.TrimPrefix(cfg.OPNsense.URL, "https://")
	host = strings.TrimPrefix(host, "http://")
	return strings.Trim(host, "[]")
}

// opnsenseSSH runs a command on OPNsense via SSH.
func opnsenseSSH(ctx context.Context, log *slog.Logger, host, cmd string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	sshCmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("root@%s", host), cmd)
	out, err := sshCmd.CombinedOutput()
	if err != nil {
		log.Warn("OPNsense SSH command failed", "host", host, "cmd", cmd, "err", err, "output", string(out))
		return err
	}
	log.Info("OPNsense SSH command OK", "host", host, "cmd", cmd)
	return nil
}


// stopWatchdog stops the mwan-watchdog service on the local hypervisor.
func stopWatchdog(log *slog.Logger) {
	cmd := exec.Command("systemctl", "stop", "mwan-watchdog")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Warn("failed to stop watchdog (may not be running)", "err", err, "output", string(out))
	} else {
		log.Info("watchdog stopped")
	}
}

// startWatchdog starts the mwan-watchdog service on the local hypervisor.
func startWatchdog(log *slog.Logger) {
	cmd := exec.Command("systemctl", "start", "mwan-watchdog")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Warn("failed to start watchdog", "err", err, "output", string(out))
	} else {
		log.Info("watchdog started")
	}
}

