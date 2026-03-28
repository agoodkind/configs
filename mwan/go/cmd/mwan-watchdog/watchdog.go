package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type connectivityState string

const (
	stateUnknown   connectivityState = "unknown"
	stateHealthy   connectivityState = "healthy"
	statePartial   connectivityState = "partial"
	stateDown      connectivityState = "down"
	stateVMStopped connectivityState = "vm_stopped"

	// heartbeatInterval controls how often we log "still healthy" in steady state.
	heartbeatInterval = 5 * time.Minute
)

type watchdog struct {
	cfg config
	nc  networkConfig
	ops sysOps

	coord   *watchdogCoord
	limiter *alertLimiter
	log     *slog.Logger

	lastState             connectivityState
	vmStoppedLogged       bool
	consecutiveTotalFails int
	totalDownStartUnix    int64
	lastHeartbeat         time.Time

	// probeLog accumulates per-cycle probe results for inclusion in emails.
	probeLog []string
}

func (w *watchdog) appendProbe(msg string) {
	w.probeLog = append(w.probeLog, msg)
}

func (w *watchdog) flushProbeLog() string {
	s := strings.Join(w.probeLog, "\n")
	w.probeLog = w.probeLog[:0]
	return s
}

func (w *watchdog) guestExecOK(ctx context.Context, args ...string) bool {
	parsed, err := w.ops.guestExec(ctx, w.cfg.MwanVMID, args...)
	if err != nil {
		w.log.Error(
			"guestExec error",
			"args", strings.Join(args, " "),
			"err", err,
		)
		return false
	}
	if parsed.ExitCode != 0 {
		w.log.Info(
			"guestExec non-zero exit",
			"args", strings.Join(args, " "),
			"exit_code", parsed.ExitCode,
		)
		return false
	}
	return true
}

// probeConnectivity pings the configured IPv4 and IPv6 targets from the host.
func (w *watchdog) probeConnectivity(ctx context.Context) (v4ok, v6ok bool) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Add(2)
	go func() {
		defer wg.Done()
		ok := w.ops.ping(ctx, "ping", w.nc.PingTargetIPv4)
		mu.Lock()
		v4ok = ok
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		ok := w.ops.ping(ctx, "ping6", w.nc.PingTargetIPv6)
		mu.Lock()
		v6ok = ok
		mu.Unlock()
	}()
	wg.Wait()

	v4str := "OK"
	if !v4ok {
		v4str = "FAIL"
	}
	v6str := "OK"
	if !v6ok {
		v6str = "FAIL"
	}
	w.log.Info(
		"probe",
		"ipv4_target", w.nc.PingTargetIPv4,
		"ipv4", v4str,
		"ipv6_target", w.nc.PingTargetIPv6,
		"ipv6", v6str,
	)
	w.appendProbe(fmt.Sprintf(
		"Host probe: IPv4 %s (%s), IPv6 %s (%s)",
		w.nc.PingTargetIPv4, v4str,
		w.nc.PingTargetIPv6, v6str,
	))
	return v4ok, v6ok
}

// testVMConnectivity pings through the VM's default route to distinguish a
// MWAN routing failure from a Proxmox-side issue.
func (w *watchdog) testVMConnectivity(ctx context.Context) bool {
	w.log.Info(
		"Testing VM default-route connectivity",
		"vmid", w.cfg.MwanVMID,
		"ping6_target", w.nc.PingTargetIPv6,
		"ping_target", w.nc.PingTargetIPv4,
	)
	if w.guestExecOK(ctx, "ping6", "-c", "2", "-W", "3", w.nc.PingTargetIPv6) {
		w.log.Info(
			"VM default-route IPv6 ping OK -> issue is Proxmox-side",
			"vmid", w.cfg.MwanVMID,
		)
		w.appendProbe(fmt.Sprintf(
			"VM %s default-route: IPv6 OK (Proxmox-side issue confirmed)",
			w.cfg.MwanVMID,
		))
		return true
	}
	if w.guestExecOK(ctx, "ping", "-c", "2", "-W", "3", w.nc.PingTargetIPv4) {
		w.log.Info(
			"VM default-route IPv4 ping OK -> issue is Proxmox-side",
			"vmid", w.cfg.MwanVMID,
		)
		w.appendProbe(fmt.Sprintf(
			"VM %s default-route: IPv4 OK (Proxmox-side issue confirmed)",
			w.cfg.MwanVMID,
		))
		return true
	}
	w.log.Info(
		"VM default-route: both IPv4 and IPv6 FAILED",
		"vmid", w.cfg.MwanVMID,
	)
	w.appendProbe(fmt.Sprintf(
		"VM %s default-route: IPv4 FAIL, IPv6 FAIL",
		w.cfg.MwanVMID,
	))
	return false
}

// testISP pings through each configured WAN interface inside the VM.
// A success on any interface means the ISP link is up, pointing to a routing
// failure rather than a real outage.
func (w *watchdog) testISP(ctx context.Context) bool {
	ifaces := w.nc.wanIfaceNames()
	w.log.Info(
		"Testing ISP reachability via WAN interfaces",
		"wan_count", len(ifaces),
		"interfaces", strings.Join(ifaces, ", "),
	)
	for _, iface := range ifaces {
		v4ok := w.guestExecOK(
			ctx, "ping", "-c", "3", "-W", "3", "-I", iface, w.nc.PingTargetIPv4,
		)
		v6ok := w.guestExecOK(
			ctx, "ping6", "-c", "3", "-W", "3", "-I", iface, w.nc.PingTargetIPv6,
		)
		if v4ok {
			w.log.Info(
				"ISP reachable from VM (IPv4 OK)",
				"interface", iface,
			)
			w.appendProbe(fmt.Sprintf("WAN %s: IPv4 OK", iface))
			return true
		}
		if v6ok {
			w.log.Info(
				"ISP reachable from VM (IPv6 OK)",
				"interface", iface,
			)
			w.appendProbe(fmt.Sprintf("WAN %s: IPv6 OK", iface))
			return true
		}
		w.log.Info(
			"ISP unreachable from VM (IPv4 FAIL, IPv6 FAIL)",
			"interface", iface,
		)
		w.appendProbe(fmt.Sprintf("WAN %s: IPv4 FAIL, IPv6 FAIL", iface))
	}
	w.log.Info("ISP unreachable from VM on all tested WAN interfaces")
	return false
}

func (w *watchdog) findSnapshot(ctx context.Context) (string, error) {
	w.log.Info("Listing snapshots for VM", "vmid", w.cfg.MwanVMID)
	out, err := w.ops.vmSnapshots(ctx, w.cfg.MwanVMID)
	if err != nil {
		return "", err
	}
	snap := extractLatestSnapshot(out)
	if snap == "" {
		w.log.Info(
			"No pre-deploy-* snapshot found",
			"listsnapshot_output", string(out),
		)
	} else {
		w.log.Info("Found rollback snapshot", "snapshot", snap)
	}
	return snap, nil
}

func (w *watchdog) checkDeploy(ctx context.Context) (int64, bool) {
	running, err := w.ops.vmStatus(ctx, w.cfg.MwanVMID)
	if err != nil {
		w.log.Error("checkDeploy: vmStatus error", "err", err)
		return 0, false
	}
	if !running {
		w.log.Info(
			"checkDeploy: VM is not running; cannot check deploy timestamp",
			"vmid", w.cfg.MwanVMID,
		)
		return 0, false
	}

	w.log.Info(
		"checkDeploy: reading last deploy path from VM",
		"path", w.nc.LastDeployPath,
		"vmid", w.cfg.MwanVMID,
	)
	parsed, err := w.ops.guestExec(
		ctx, w.cfg.MwanVMID, "cat", w.nc.LastDeployPath,
	)
	if err != nil {
		w.log.Error(
			"checkDeploy: guestExec(cat) error",
			"path", w.nc.LastDeployPath,
			"err", err,
		)
		return 0, false
	}
	if parsed.ExitCode != 0 {
		w.log.Info(
			"checkDeploy: last deploy path not found or unreadable in VM; no recent deploy",
			"path", w.nc.LastDeployPath,
			"exit_code", parsed.ExitCode,
		)
		return 0, false
	}

	raw := strings.TrimSpace(parsed.Stdout)
	if raw == "" || raw == "null" {
		w.log.Info(
			"checkDeploy: last deploy path is empty or null; no recent deploy",
			"path", w.nc.LastDeployPath,
		)
		return 0, false
	}

	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		w.log.Error(
			"checkDeploy: cannot parse deploy timestamp",
			"raw", raw,
			"err", err,
		)
		return 0, false
	}

	ageMin := (time.Now().Unix() - ts) / 60
	w.log.Info(
		"checkDeploy: deploy timestamp and age",
		"deploy_ts", ts,
		"age_minutes", ageMin,
		"window_minutes", w.cfg.DeployWindowMinutes,
	)
	if ageMin > int64(w.cfg.DeployWindowMinutes) {
		w.log.Info(
			"checkDeploy: deploy is stale; no rollback",
			"age_minutes", ageMin,
			"window_minutes", w.cfg.DeployWindowMinutes,
		)
		return 0, false
	}

	w.log.Info(
		"checkDeploy: deploy is recent; rollback is eligible",
		"age_minutes", ageMin,
	)
	return ts, true
}

func (w *watchdog) rollback(ctx context.Context, deployTS int64, snap string) {
	w.coord.setRollingBack(true)
	defer w.coord.setRollingBack(false)

	probeHistory := w.flushProbeLog()

	lockContent := fmt.Sprintf(
		"deploy_ts=%d snapshot=%s ts=%d\n",
		deployTS, snap, time.Now().Unix(),
	)
	if err := os.WriteFile(
		w.cfg.RollbackLockFile, []byte(lockContent), 0o644,
	); err != nil {
		w.log.Error("write rollback lock", "err", err)
	} else {
		w.log.Info("Wrote rollback lock", "path", w.cfg.RollbackLockFile)
	}

	w.log.Info(
		"INITIATING ROLLBACK",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"deploy_ts", deployTS,
		"deploy_age_seconds", time.Now().Unix()-deployTS,
	)

	stopStart := time.Now()
	w.log.Info(
		"Stopping VM",
		"vmid", w.cfg.MwanVMID,
		"timeout", timeoutQmStop,
	)
	if err := w.ops.vmStop(ctx, w.cfg.MwanVMID); err != nil {
		w.log.Error(
			"vmStop error (continuing to rollback)",
			"vmid", w.cfg.MwanVMID,
			"err", err,
		)
	} else {
		w.log.Info(
			"VM stopped",
			"vmid", w.cfg.MwanVMID,
			"elapsed", time.Since(stopStart).Round(time.Millisecond),
		)
	}

	rollbackStart := time.Now()
	w.log.Info(
		"Running qm rollback",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"timeout", timeoutQmRollback,
	)
	if err := w.ops.vmRollback(ctx, w.cfg.MwanVMID, snap); err != nil {
		w.log.Error(
			"qm rollback FAILED; attempting qm start anyway",
			"vmid", w.cfg.MwanVMID,
			"snapshot", snap,
			"elapsed", time.Since(rollbackStart).Round(time.Millisecond),
			"err", err,
		)
	} else {
		w.log.Info(
			"qm rollback completed",
			"elapsed", time.Since(rollbackStart).Round(time.Millisecond),
		)
	}

	startTime := time.Now()
	w.log.Info(
		"Starting VM",
		"vmid", w.cfg.MwanVMID,
		"timeout", timeoutQmStart,
	)
	if err := w.ops.vmStart(ctx, w.cfg.MwanVMID); err != nil {
		w.log.Error(
			"qm start FAILED; VM may remain stopped",
			"vmid", w.cfg.MwanVMID,
			"elapsed", time.Since(startTime).Round(time.Millisecond),
			"err", err,
		)
	} else {
		w.log.Info(
			"VM started",
			"vmid", w.cfg.MwanVMID,
			"elapsed", time.Since(startTime).Round(time.Millisecond),
		)
	}

	if err := os.Remove(w.cfg.RollbackLockFile); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		w.log.Error("remove rollback lock", "err", err)
	} else {
		w.log.Info("Removed rollback lock file")
	}

	if err := writeRollbackState(w.cfg.RollbackStateFile, deployTS, snap); err != nil {
		w.log.Error("write rollback state", "err", err)
	} else {
		w.log.Info(
			"Wrote rollback state",
			"path", w.cfg.RollbackStateFile,
			"deploy_ts", deployTS,
			"snapshot", snap,
		)
	}

	deployAge := time.Now().Unix() - deployTS
	msg := fmt.Sprintf(
		"MWAN auto-rollback executed at %s.\n\n"+
			"VM:              %s\n"+
			"Snapshot:        %s\n"+
			"Deploy timestamp: %d (%ds ago, within %dm window)\n\n"+
			"Connectivity probe history:\n%s\n\n"+
			"The VM has been rolled back and restarted. "+
			"Monitor routing recovery and verify WAN connectivity.\n"+
			"If routing does not recover within %s, manual intervention is needed.",
		time.Now().Format(time.RFC3339),
		w.cfg.MwanVMID,
		snap,
		deployTS,
		deployAge,
		w.cfg.DeployWindowMinutes,
		probeHistory,
		w.cfg.PostRollbackGraceSeconds,
	)
	w.log.Info("Sending rollback notification email...")
	if err := w.ops.sendEmail(
		ctx, w.cfg.AlertEmail, "MWAN Auto-Rollback Triggered", msg,
	); err != nil {
		w.log.Error("rollback email send error", "err", err)
	} else {
		w.log.Info("Rollback email sent", "to", w.cfg.AlertEmail)
	}

	w.log.Info(
		"ROLLBACK COMPLETE; waiting for routes to converge",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"grace", w.cfg.PostRollbackGraceSeconds,
	)
	if w.coord.takeShutdownAfterRollback() {
		w.log.Info("Deferred shutdown now executing after rollback")
		os.Exit(0)
	}
}

func (w *watchdog) recoverInterrupted(ctx context.Context) {
	data, err := os.ReadFile(w.cfg.RollbackLockFile)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		w.log.Error("read rollback lock", "err", err)
		return
	}
	w.log.Info(
		"Found rollback lock from previous instance",
		"lock_content", strings.TrimSpace(string(data)),
	)
	running, statusErr := w.ops.vmStatus(ctx, w.cfg.MwanVMID)
	if statusErr != nil {
		w.log.Error("qm status during recovery", "err", statusErr)
		return
	}
	if running {
		w.log.Info(
			"VM is running; previous rollback completed. Removing lock.",
			"vmid", w.cfg.MwanVMID,
		)
		if err := os.Remove(w.cfg.RollbackLockFile); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			w.log.Error("remove stale rollback lock", "err", err)
		}
		return
	}
	w.log.Info(
		"VM is STOPPED and rollback lock exists; attempting to start VM to complete interrupted rollback",
		"vmid", w.cfg.MwanVMID,
	)
	if startErr := w.ops.vmStart(ctx, w.cfg.MwanVMID); startErr != nil {
		w.log.Error(
			"VM start after interrupted rollback FAILED; manual intervention needed",
			"vmid", w.cfg.MwanVMID,
			"err", startErr,
		)
	} else {
		w.log.Info(
			"VM started successfully after interrupted rollback",
			"vmid", w.cfg.MwanVMID,
		)
	}
	if err := os.Remove(w.cfg.RollbackLockFile); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		w.log.Error("remove rollback lock after recovery", "err", err)
	}
	w.log.Info(
		"Waiting for VM to boot and routes to converge after interrupted rollback recovery",
		"grace", w.cfg.PostRollbackGraceSeconds,
	)
	time.Sleep(w.cfg.PostRollbackGraceSeconds)
}

func (w *watchdog) sendPartialAlert(ctx context.Context, proto string) {
	if !w.limiter.trySendPartial(time.Now()) {
		remaining := w.limiter.partialCooldownRemaining(time.Now())
		w.log.Info(
			"Partial alert suppressed (cooldown)",
			"protocol", proto,
			"remaining", remaining.Round(time.Second),
		)
		return
	}
	w.log.Info(
		"Sending partial-degradation alert",
		"protocol_down", proto,
	)
	upProto := "IPv4"
	if proto == "IPv4" {
		upProto = "IPv6"
	}
	body := fmt.Sprintf(
		"MWAN partial connectivity degradation at %s.\n\n"+
			"DOWN:  %s (target: %s)\n"+
			"UP:    %s (target: %s)\n\n"+
			"VM:    %s\n"+
			"Path:  Proxmox -> OPNsense -> MWAN VM -> WAN\n\n"+
			"The other protocol is still working. "+
			"This is a partial degradation; no rollback will be triggered.\n"+
			"Manual investigation may be needed.\n\n"+
			"Probe history:\n%s",
		time.Now().Format(time.RFC3339),
		proto, w.pingTarget(proto),
		upProto, w.pingTarget(upProto),
		w.cfg.MwanVMID,
		w.flushProbeLog(),
	)
	if err := w.ops.sendEmail(
		ctx, w.cfg.AlertEmail, "MWAN: "+proto+" connectivity lost", body,
	); err != nil {
		w.log.Error("partial alert email error", "err", err)
	} else {
		w.log.Info("Partial alert email sent", "to", w.cfg.AlertEmail)
	}
}

func (w *watchdog) pingTarget(proto string) string {
	if proto == "IPv6" {
		return w.nc.PingTargetIPv6
	}
	return w.nc.PingTargetIPv4
}

func (w *watchdog) sendTotalAlert(ctx context.Context, reason, detail string) {
	if !w.limiter.trySendTotal(time.Now()) {
		remaining := w.limiter.totalCooldownRemaining(time.Now())
		w.log.Info(
			"Total alert suppressed (cooldown)",
			"remaining", remaining.Round(time.Second),
			"reason", reason,
		)
		return
	}
	w.log.Info("Sending total-loss alert", "reason", reason)
	body := fmt.Sprintf(
		"MWAN connectivity alert at %s.\n\n"+
			"Reason: %s\n\n"+
			"%s\n\n"+
			"VM:      %s\n"+
			"Consecutive total failures: %d\n\n"+
			"Probe history:\n%s",
		time.Now().Format(time.RFC3339),
		reason,
		detail,
		w.cfg.MwanVMID,
		w.consecutiveTotalFails,
		w.flushProbeLog(),
	)
	if err := w.ops.sendEmail(
		ctx, w.cfg.AlertEmail, "MWAN Connectivity Alert", body,
	); err != nil {
		w.log.Error("total alert email error", "err", err)
	} else {
		w.log.Info("Total alert email sent", "to", w.cfg.AlertEmail)
	}
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

func (w *watchdog) run(ctx context.Context) {
	w.log.Info(
		"Starting MWAN watchdog",
		"vmid", w.cfg.MwanVMID,
		"deploy_window_minutes", w.cfg.DeployWindowMinutes,
		"connectivity_timeout_seconds", w.cfg.ConnectivityTimeoutSeconds,
		"check_interval_healthy", w.cfg.CheckIntervalHealthy,
		"check_interval_degraded", w.cfg.CheckIntervalDegraded,
		"alert_cooldown_seconds", w.cfg.AlertCooldownSeconds,
	)
	w.log.Info(
		"Network config",
		"ping_target_ipv4", w.nc.PingTargetIPv4,
		"ping_target_ipv6", w.nc.PingTargetIPv6,
		"wan_interfaces", strings.Join(w.nc.wanIfaceNames(), ", "),
		"last_deploy_path", w.nc.LastDeployPath,
	)
	w.log.Info(
		"PVE",
		"node", w.cfg.PVENode,
		"token_id", w.cfg.PVETokenID,
		"vsock_cid", w.cfg.VsockCID,
		"vsock_port", w.cfg.VsockPort,
	)
	w.recoverInterrupted(ctx)
	w.lastState = stateUnknown
	w.lastHeartbeat = time.Now()
	iteration := 0

	for {
		if w.cfg.MaxIterations > 0 && iteration >= w.cfg.MaxIterations {
			w.log.Info("Reached max iterations; exiting", "max", w.cfg.MaxIterations)
			return
		}
		iteration++

		select {
		case <-ctx.Done():
			w.log.Info("Context cancelled; watchdog shutting down")
			return
		default:
		}

		running, err := w.ops.vmStatus(ctx, w.cfg.MwanVMID)
		if err != nil {
			w.log.Error("qm status error", "vmid", w.cfg.MwanVMID, "err", err)
			if !sleepOrDone(ctx, w.cfg.CheckIntervalDegraded) {
				w.log.Info(
					"Context cancelled during degraded sleep; watchdog shutting down",
				)
				return
			}
			continue
		}
		if !running {
			if !w.vmStoppedLogged {
				w.log.Info(
					"VM is not running; pausing checks",
					"vmid", w.cfg.MwanVMID,
					"recheck_interval", w.cfg.CheckIntervalDegraded,
				)
				w.vmStoppedLogged = true
				w.lastState = stateVMStopped
			}
			if !sleepOrDone(ctx, w.cfg.CheckIntervalDegraded) {
				w.log.Info(
					"Context cancelled during VM-stopped sleep; watchdog shutting down",
				)
				return
			}
			continue
		}
		if w.vmStoppedLogged {
			w.log.Info("VM is running again", "vmid", w.cfg.MwanVMID)
		}
		w.vmStoppedLogged = false

		v4ok, v6ok := w.probeConnectivity(ctx)

		if v4ok && v6ok {
			if w.lastState != stateHealthy {
				w.log.Info(
					"Connectivity OK: IPv4 and IPv6",
					"previous_state", w.lastState,
				)
			} else if time.Since(w.lastHeartbeat) >= heartbeatInterval {
				w.log.Info(
					"Heartbeat: connectivity healthy",
					"ping_target_ipv4", w.nc.PingTargetIPv4,
					"ping_target_ipv6", w.nc.PingTargetIPv6,
					"iteration", iteration,
				)
				w.lastHeartbeat = time.Now()
			}
			w.lastState = stateHealthy
			w.consecutiveTotalFails = 0
			w.totalDownStartUnix = 0
			w.limiter.resetCooldowns()
			w.probeLog = w.probeLog[:0]
			if !sleepOrDone(ctx, w.cfg.CheckIntervalHealthy) {
				w.log.Info(
					"Context cancelled during healthy sleep; watchdog shutting down",
				)
				return
			}
			continue
		}

		if v4ok || v6ok {
			downProto := "IPv6"
			if v6ok {
				downProto = "IPv4"
			}
			if w.lastState != statePartial {
				w.log.Info(
					"Partial degradation: one protocol DOWN, other OK",
					"protocol_down", downProto,
					"previous_state", w.lastState,
				)
			} else {
				w.log.Info(
					"Still in partial degradation: one protocol DOWN, other OK",
					"protocol_down", downProto,
				)
			}
			w.lastState = statePartial
			w.consecutiveTotalFails = 0
			w.totalDownStartUnix = 0
			w.sendPartialAlert(ctx, downProto)
			if !sleepOrDone(ctx, w.cfg.CheckIntervalDegraded) {
				w.log.Info(
					"Context cancelled during partial-degradation sleep; watchdog shutting down",
				)
				return
			}
			continue
		}

		w.consecutiveTotalFails++
		now := time.Now().Unix()
		if w.totalDownStartUnix == 0 {
			w.totalDownStartUnix = now
			w.log.Info(
				"TOTAL connectivity loss (IPv4 and IPv6 both FAILED); starting timeout",
				"timeout_seconds", w.cfg.ConnectivityTimeoutSeconds,
				"fail_count", w.consecutiveTotalFails,
			)
		}
		w.lastState = stateDown
		downDuration := int(now - w.totalDownStartUnix)
		remaining := w.cfg.ConnectivityTimeoutSeconds - downDuration
		if downDuration < w.cfg.ConnectivityTimeoutSeconds {
			w.log.Info(
				"Still down before timeout threshold",
				"elapsed_seconds", downDuration,
				"remaining_seconds", remaining,
				"fail_count", w.consecutiveTotalFails,
			)
			if !sleepOrDone(ctx, w.cfg.CheckIntervalDegraded) {
				w.log.Info(
					"Context cancelled during total-loss sleep; watchdog shutting down",
				)
				return
			}
			continue
		}

		w.log.Info(
			"Timeout exceeded; entering diagnosis",
			"down_seconds", downDuration,
			"threshold_seconds", w.cfg.ConnectivityTimeoutSeconds,
		)
		w.handleTimeoutExceeded(ctx)
	}
}

func (w *watchdog) handleTimeoutExceeded(ctx context.Context) {
	w.log.Info("--- DIAGNOSIS START ---")

	w.log.Info("Step 1: testing VM default-route connectivity...")
	if w.testVMConnectivity(ctx) {
		w.log.Info(
			"Diagnosis: VM has internet via default route -> issue is Proxmox-side, not MWAN",
		)
		w.sendTotalAlert(
			ctx,
			"Proxmox cannot reach internet but MWAN VM can",
			"This indicates a Proxmox routing or OPNsense issue, not an MWAN configuration problem.\n"+
				"No rollback will be triggered. Manual investigation of the Proxmox/OPNsense path is needed.",
		)
		w.log.Info("--- DIAGNOSIS END (Proxmox-side issue) ---")
		sleepOrDone(ctx, 60*time.Second)
		return
	}

	w.log.Info("Step 2: testing per-WAN-interface ISP reachability...")
	if !w.testISP(ctx) {
		if w.consecutiveTotalFails <= 2 {
			w.log.Info(
				"ISP unreachable from VM on all WAN interfaces; treating as real ISP outage (no rollback)",
				"interface_count", len(w.nc.WANInterfaces),
				"interfaces", strings.Join(w.nc.wanIfaceNames(), ", "),
			)
		} else {
			w.log.Info(
				"Still in ISP outage; no rollback",
				"fail_count", w.consecutiveTotalFails,
			)
		}
		w.log.Info("--- DIAGNOSIS END (ISP outage) ---")
		sleepOrDone(ctx, 60*time.Second)
		return
	}

	w.log.Info(
		"Diagnosis: ISP reachable per-interface but default route broken -> MWAN routing failure",
	)

	w.log.Info("Step 3: checking for recent deploy...")
	deployTS, recent := w.checkDeploy(ctx)
	if !recent {
		w.log.Info(
			"No recent deploy found; cannot auto-rollback (manual intervention needed)",
		)
		w.sendTotalAlert(
			ctx,
			"MWAN routing broken but no recent deploy found",
			fmt.Sprintf(
				"MWAN routing is broken (ISP reachable per-interface, "+
					"default route failed) but no deploy timestamp was found within "+
					"the %dm window.\n\nThis may indicate:\n"+
					"  - A spontaneous routing failure (nftables, npd, etc.)\n"+
					"  - A deploy that happened outside the tracking window\n"+
					"  - The deploy timestamp file is missing from the VM\n\n"+
					"Manual investigation is required.",
				w.cfg.DeployWindowMinutes,
			),
		)
		w.log.Info("--- DIAGNOSIS END (no recent deploy) ---")
		sleepOrDone(ctx, 60*time.Second)
		return
	}

	w.log.Info(
		"Step 4: checking rollback state",
		"deploy_ts", deployTS,
	)
	already, err := rollbackAlreadyDone(w.cfg.RollbackStateFile, deployTS)
	if err != nil {
		w.log.Error(
			"read rollback state file (proceeding cautiously)",
			"path", w.cfg.RollbackStateFile,
			"err", err,
		)
	}
	if already {
		w.log.Info(
			"Rollback already performed for this deploy_ts; not rolling back again",
			"deploy_ts", deployTS,
		)
		w.log.Info("--- DIAGNOSIS END (rollback already done) ---")
		sleepOrDone(ctx, 60*time.Second)
		return
	}

	w.log.Info("Step 5: finding rollback snapshot...")
	snap, snapErr := w.findSnapshot(ctx)
	if snapErr != nil {
		w.log.Error("listsnapshot error", "err", snapErr)
	}
	if snap == "" {
		w.log.Info("No pre-deploy snapshot found; cannot rollback")
		w.sendTotalAlert(
			ctx,
			"MWAN routing broken after deploy but no pre-deploy snapshot exists",
			fmt.Sprintf(
				"A recent deploy (ts=%d) was detected and routing is broken, "+
					"but no pre-deploy-* snapshot was found.\n\n"+
					"Manual intervention is required. "+
					"Create a snapshot and fix routing manually.",
				deployTS,
			),
		)
		w.log.Info("--- DIAGNOSIS END (no snapshot) ---")
		sleepOrDone(ctx, 60*time.Second)
		return
	}

	w.log.Info(
		"--- DIAGNOSIS END: triggering rollback ---",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"deploy_ts", deployTS,
	)
	rbCtx := context.Background()
	w.rollback(rbCtx, deployTS, snap)
	w.log.Info(
		"Waiting for VM to boot and routes to converge after rollback",
		"grace", w.cfg.PostRollbackGraceSeconds,
	)
	sleepOrDone(ctx, w.cfg.PostRollbackGraceSeconds)
}
