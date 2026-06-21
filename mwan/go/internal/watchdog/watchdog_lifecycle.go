package watchdog

import (
	"context"
	"os"
	"strings"
	"time"

	"goodkind.io/mwan/internal/rollback"
	"goodkind.io/mwan/internal/version"
)

func (w *watchdog) sendPartialAlert(ctx context.Context, proto string) {
	log := w.tracedLogger(ctx)
	if !w.limiter.TrySendPartial(w.now()) {
		remaining := w.limiter.PartialCooldownRemaining(w.now())
		log.InfoContext(ctx,
			"Partial alert suppressed (cooldown)",
			"protocol", proto,
			"remaining", remaining.Round(time.Second),
		)
		return
	}
	log.InfoContext(ctx,
		"Sending partial-degradation alert",
		"protocol_down", proto,
	)
	log.WarnContext(ctx,
		"connectivity partial loss",
		"proto", proto,
		"vm_id", w.cfg.MwanVMID,
		"node", w.cfg.PVE.Node,
	)
}

func (w *watchdog) sendTotalAlert(ctx context.Context, reason, detail string) {
	log := w.tracedLogger(ctx)
	_ = detail
	if !w.limiter.TrySendTotal(w.now()) {
		remaining := w.limiter.TotalCooldownRemaining(w.now())
		log.InfoContext(ctx,
			"Total alert suppressed (cooldown)",
			"remaining", remaining.Round(time.Second),
			"reason", reason,
		)
		return
	}
	log.InfoContext(ctx, "Sending total-loss alert", "reason", reason)
	log.ErrorContext(ctx,
		"connectivity total loss",
		"reason", reason,
		"vm_id", w.cfg.MwanVMID,
		"node", w.cfg.PVE.Node,
		"err", reason,
	)
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// logStartupConfig emits structured log entries describing the watchdog's
// configuration. Called once at the beginning of run().
func (w *watchdog) logStartupConfig(ctx context.Context) {
	log := w.log
	log.InfoContext(ctx,
		"Starting MWAN watchdog",
		"vmid", w.cfg.MwanVMID,
		"deploy_window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
		"connectivity_timeout_seconds", w.cfg.Watchdog.ConnectivityTimeoutSeconds,
		"check_interval_healthy", w.cfg.Watchdog.HealthyInterval(),
		"check_interval_degraded", w.cfg.Watchdog.DegradedInterval(),
		"alert_cooldown_seconds", w.cfg.Watchdog.AlertCooldownSeconds,
	)
	log.InfoContext(ctx,
		"Network config",
		"ping_target_ipv4", w.cfg.Network.PingTargetIPv4,
		"ping_target_ipv6", w.cfg.Network.PingTargetIPv6,
		"wan_interfaces", strings.Join(w.cfg.Network.WanIfaceNames(), ", "),
		"last_deploy_path", w.cfg.Network.LastDeployPath,
	)
	log.InfoContext(ctx,
		"PVE",
		"node", w.cfg.PVE.Node,
		"pve_api_configured", w.cfg.PVE.TokenID != "",
		"vsock_cid", w.cfg.Watchdog.VsockCID,
		"vsock_port", w.cfg.Watchdog.VsockPort,
	)
}

// runStartupChecks performs one-time checks when the watchdog loop begins:
// recovering from interrupted rollbacks, verifying snapshots exist, probing
// connectivity, and checking the config hash.
func (w *watchdog) runStartupChecks(ctx context.Context) {
	log := w.tracedLogger(ctx)
	w.recoverInterrupted(ctx)

	// Startup checks.
	if listOut, lErr := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID); lErr == nil {
		if rollback.ExtractLatestSnapshot(listOut) == "" {
			log.WarnContext(ctx,
				"No rollback snapshots found at startup",
				"vmid", w.cfg.MwanVMID,
				"note", "rollback will not be possible until a snapshot is created",
			)
		}
	} else {
		log.WarnContext(ctx, "Could not check snapshots at startup", "err", lErr)
	}
	if data, lErr := os.ReadFile(w.cfg.Watchdog.RollbackLockFile); lErr == nil {
		log.WarnContext(ctx,
			"Stale rollback lock file found at startup",
			"path", w.cfg.Watchdog.RollbackLockFile,
			"content", strings.TrimSpace(string(data)),
			"note", "a previous run may have crashed mid-rollback",
		)
	}

	// Run a connectivity probe and config hash check before the startup log
	// so the channel tracker and ping results reflect actual startup state.
	// Only probe if the VM is currently running.
	var startupV4, startupV6 bool
	if running, err := w.ops.VMStatus(ctx, w.cfg.MwanVMID); err == nil && running {
		startupV4, startupV6 = w.probeConnectivity(ctx)
		w.checkConfigHash(ctx)
	}
	v4str, v6str := "OK", "OK"
	if !startupV4 {
		v4str = "FAIL"
	}
	if !startupV6 {
		v6str = "FAIL"
	}
	log.InfoContext(ctx,
		"watchdog started",
		"vm_id", w.cfg.MwanVMID,
		"node", w.cfg.PVE.Node,
		"build", version.BuildVersionString(),
		"ipv4", v4str,
		"ipv6", v6str,
		"deploy_window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
		"check_interval_healthy", w.cfg.Watchdog.HealthyInterval(),
		"wan_interfaces", strings.Join(w.cfg.Network.WanIfaceNames(), ","),
	)
}

// handleVMStopped handles the case when the MWAN VM is not running.
// It logs the event, attempts auto-start if appropriate, and returns true
// if the caller should continue to the next loop iteration.
func (w *watchdog) handleVMStopped(ctx context.Context) {
	log := w.tracedLogger(ctx)
	if w.vmStoppedLogged {
		return
	}
	log.InfoContext(ctx,
		"VM is not running; pausing checks",
		"vmid", w.cfg.MwanVMID,
		"recheck_interval", w.cfg.Watchdog.DegradedInterval(),
	)
	w.vmStoppedLogged = true
	w.lastState = stateVMStopped

	// Only alert and auto-start when no rollback is in progress.
	// If the rollback lock exists, the watchdog itself stopped the VM
	// intentionally; do not interfere. Also skip if recoverInterrupted
	// already handled this episode (lock was present at startup and removed).
	_, lockErr := os.Stat(w.cfg.Watchdog.RollbackLockFile)
	rollbackInProgress := lockErr == nil || w.recoveredFromRollback
	if rollbackInProgress {
		log.InfoContext(ctx,
			"VM stopped but rollback lock present; skipping alert and auto-start",
			"lock_file", w.cfg.Watchdog.RollbackLockFile,
		)
		return
	}
	log.ErrorContext(ctx,
		"VM stopped unexpectedly",
		"vm_id", w.cfg.MwanVMID,
		"node", w.cfg.PVE.Node,
		"err", "vm transitioned to stopped state outside of mwan control",
	)

	log.InfoContext(ctx, "Attempting to start stopped VM", "vmid", w.cfg.MwanVMID)
	if startErr := w.ops.VMStart(ctx, w.cfg.MwanVMID); startErr != nil {
		log.ErrorContext(ctx,
			"vmStart failed for stopped VM",
			"vmid", w.cfg.MwanVMID,
			"err", startErr,
		)
	} else {
		log.InfoContext(ctx, "vmStart issued for stopped VM", "vmid", w.cfg.MwanVMID)
	}
}

// handleHealthyProbe processes a fully-healthy probe result (both v4 and v6 OK).
// It updates counters, checks config hash periodically, manages snapshots, and
// emits heartbeat logs.
func (w *watchdog) handleHealthyProbe(ctx context.Context, iteration int) {
	log := w.tracedLogger(ctx)
	w.consecutiveHealthy++
	w.healthyCyclesForHash++
	if w.cfg.Watchdog.HashCheckEveryNHealthy > 0 &&
		w.healthyCyclesForHash >= w.cfg.Watchdog.HashCheckEveryNHealthy {
		w.healthyCyclesForHash = 0
		w.checkConfigHash(ctx)
	}
	w.maybeSnapshot(ctx)

	if w.lastState != stateHealthy {
		log.InfoContext(ctx,
			"Connectivity OK: IPv4 and IPv6",
			"previous_state", w.lastState,
		)
	} else if w.since(w.lastHeartbeat) >= w.heartbeatTick() {
		log.InfoContext(ctx,
			"Heartbeat: connectivity healthy",
			"ping_target_ipv4", w.cfg.Network.PingTargetIPv4,
			"ping_target_ipv6", w.cfg.Network.PingTargetIPv6,
			"iteration", iteration,
		)
		if w.tracker != nil {
			w.tracker.LogAll(ctx, log)
		}
		w.lastHeartbeat = w.now()
	}
	w.lastState = stateHealthy
	w.consecutiveTotalFails = 0
	w.totalDownStartUnix = 0
	w.limiter.ResetCooldowns()
	w.probeLog = w.probeLog[:0]
}

// handlePartialProbe processes a probe where one protocol is up and the other
// is down.
func (w *watchdog) handlePartialProbe(ctx context.Context, v6ok bool) {
	log := w.tracedLogger(ctx)
	downProto := "IPv6"
	if v6ok {
		downProto = "IPv4"
	}
	if w.lastState != statePartial {
		log.InfoContext(ctx,
			"Partial degradation: one protocol DOWN, other OK",
			"protocol_down", downProto,
			"previous_state", w.lastState,
		)
	} else {
		log.InfoContext(ctx,
			"Still in partial degradation: one protocol DOWN, other OK",
			"protocol_down", downProto,
		)
	}
	w.lastState = statePartial
	w.consecutiveTotalFails = 0
	w.totalDownStartUnix = 0
	w.sendPartialAlert(ctx, downProto)
}
