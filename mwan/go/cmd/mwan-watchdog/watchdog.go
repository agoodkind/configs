package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
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

	// exitFn, if non-nil, replaces os.Exit when rollback defers shutdown after SIGTERM.
	exitFn func(code int)
	// testHeartbeatInterval overrides heartbeatInterval in run() when > 0 (tests only).
	testHeartbeatInterval time.Duration

	lastState             connectivityState
	vmStoppedLogged       bool
	recoveredFromRollback bool // set by recoverInterrupted when VM was stopped due to rollback
	consecutiveTotalFails int
	totalDownStartUnix    int64
	lastHeartbeat         time.Time

	// probeLog accumulates per-cycle probe results for inclusion in emails.
	probeLog []string

	// tracker may be nil when ops is not realOps (e.g. mock in tests).
	tracker *channelTracker

	lastConfigHash        string
	hashChangeWindowStart int64
	consecutiveHealthy    int
	lastSnapshotAt        time.Time
	healthyCyclesForHash  int
}

func (w *watchdog) heartbeatTick() time.Duration {
	if w.testHeartbeatInterval > 0 {
		return w.testHeartbeatInterval
	}
	return heartbeatInterval
}

func (w *watchdog) appendProbe(msg string) {
	w.probeLog = append(w.probeLog, msg)
}

func (w *watchdog) flushProbeLog() string {
	s := strings.Join(w.probeLog, "\n")
	w.probeLog = w.probeLog[:0]
	return s
}

func (w *watchdog) channelSummarySection() string {
	if w.tracker == nil {
		return ""
	}
	return "--- COMMUNICATION CHANNELS ---\n" + w.tracker.summary() + "\n"
}

// buildSystemContext collects a human-readable snapshot of the vault host's
// interface addresses for inclusion in alert emails.
func buildSystemContext() string {
	var b strings.Builder
	ifaces, err := net.Interfaces()
	if err != nil {
		return fmt.Sprintf("(error reading interfaces: %v)", err)
	}
	b.WriteString(fmt.Sprintf("Host interfaces at %s:\n", time.Now().Format(time.RFC3339)))
	for _, iface := range ifaces {
		addrs, addrErr := iface.Addrs()
		flags := iface.Flags.String()
		if addrErr != nil || len(addrs) == 0 {
			b.WriteString(fmt.Sprintf("  %-20s  flags=%-20s  (no addresses)\n",
				iface.Name, flags))
			continue
		}
		var addrStrs []string
		for _, a := range addrs {
			addrStrs = append(addrStrs, a.String())
		}
		b.WriteString(fmt.Sprintf("  %-20s  flags=%-20s  %s\n",
			iface.Name, flags, strings.Join(addrStrs, ", ")))
	}
	return b.String()
}

// collectIfaceAddrs returns a map[ifaceName][]cidr for all non-loopback interfaces.
func collectIfaceAddrs() map[string][]string {
	result := make(map[string][]string)
	ifaces, err := net.Interfaces()
	if err != nil {
		return result
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		var cidrs []string
		for _, a := range addrs {
			cidrs = append(cidrs, a.String())
		}
		result[iface.Name] = cidrs
	}
	return result
}

// diffIfaceAddrs returns a human-readable diff between two interface address snapshots.
// Returns empty string if nothing changed.
func diffIfaceAddrs(prev, curr map[string][]string) string {
	prevSet := func(m map[string][]string) map[string]map[string]struct{} {
		out := make(map[string]map[string]struct{})
		for iface, addrs := range m {
			out[iface] = make(map[string]struct{})
			for _, a := range addrs {
				out[iface][a] = struct{}{}
			}
		}
		return out
	}
	ps := prevSet(prev)
	cs := prevSet(curr)

	var changes []string

	// Check for removed addresses or removed interfaces.
	for iface, paddrs := range ps {
		caddrs, exists := cs[iface]
		if !exists {
			changes = append(changes, fmt.Sprintf("  REMOVED  %-20s  (interface gone)", iface))
			continue
		}
		for a := range paddrs {
			if _, ok := caddrs[a]; !ok {
				changes = append(changes,
					fmt.Sprintf("  REMOVED  %-20s  %s", iface, a))
			}
		}
	}

	// Check for added addresses or new interfaces.
	for iface, caddrs := range cs {
		paddrs, exists := ps[iface]
		if !exists {
			for a := range caddrs {
				changes = append(changes,
					fmt.Sprintf("  ADDED    %-20s  %s (new interface)", iface, a))
			}
			continue
		}
		for a := range caddrs {
			if _, ok := paddrs[a]; !ok {
				changes = append(changes,
					fmt.Sprintf("  ADDED    %-20s  %s", iface, a))
			}
		}
	}

	if len(changes) == 0 {
		return ""
	}
	return strings.Join(changes, "\n")
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
			"No rollback snapshot (pre-deploy-* or known-good-*)",
			"listsnapshot_output", string(out),
		)
	} else {
		w.log.Info("Found rollback snapshot", "snapshot", snap)
	}
	return snap, nil
}

func (w *watchdog) readGuestUnix(ctx context.Context, path string) (int64, bool) {
	parsed, err := w.ops.guestExec(ctx, w.cfg.MwanVMID, "cat", path)
	if err != nil {
		w.log.Error("guestExec(cat) error", "path", path, "err", err)
		return 0, false
	}
	if parsed.ExitCode != 0 {
		return 0, false
	}
	raw := strings.TrimSpace(parsed.Stdout)
	if raw == "" || raw == "null" {
		return 0, false
	}
	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		w.log.Error(
			"guest timestamp parse error",
			"path", path,
			"raw", raw,
			"err", err,
		)
		return 0, false
	}
	return ts, true
}

func (w *watchdog) checkConfigHash(ctx context.Context) {
	parsed, err := w.ops.guestExec(
		ctx, w.cfg.MwanVMID, "cat", w.nc.ConfigHashPath,
	)
	if err != nil {
		w.log.Error("checkConfigHash guestExec", "err", err)
		return
	}
	if parsed.ExitCode != 0 {
		return
	}
	h := strings.TrimSpace(parsed.Stdout)
	if h == "" {
		return
	}
	if w.lastConfigHash != "" && h != w.lastConfigHash {
		w.log.Warn(
			"config hash drift detected",
			"config_hash_path", w.nc.ConfigHashPath,
		)
		w.hashChangeWindowStart = time.Now().Unix()
	}
	w.lastConfigHash = h
}

func (w *watchdog) maybeSnapshot(ctx context.Context) {
	if w.cfg.SnapshotHealthyThreshold <= 0 {
		return
	}
	if w.consecutiveHealthy < w.cfg.SnapshotHealthyThreshold {
		return
	}
	minGap := time.Duration(w.cfg.MinSnapshotIntervalSeconds) * time.Second
	if !w.lastSnapshotAt.IsZero() && time.Since(w.lastSnapshotAt) < minGap {
		return
	}
	name := fmt.Sprintf(
		"known-good-%s",
		time.Now().Format("20060102-150405"),
	)
	if err := w.ops.vmSnapshot(ctx, w.cfg.MwanVMID, name); err != nil {
		w.log.Error("vmSnapshot failed", "err", err, "snapshot", name)
		return
	}
	w.log.Info("created known-good snapshot", "snapshot", name)
	w.lastSnapshotAt = time.Now()
	w.consecutiveHealthy = 0
	if err := w.pruneSnapshots(ctx); err != nil {
		w.log.Error("pruneSnapshots failed", "err", err)
	}
}

func (w *watchdog) pruneSnapshots(ctx context.Context) error {
	out, err := w.ops.vmSnapshots(ctx, w.cfg.MwanVMID)
	if err != nil {
		return err
	}
	s := string(out)
	knownGoods := knownGoodSnapRE.FindAllString(s, -1)
	sort.Strings(knownGoods)
	preDeploys := preDeploySnapRE.FindAllString(s, -1)
	total := len(knownGoods) + len(preDeploys)

	if w.cfg.MaxKnownGoodSnapshots > 0 &&
		len(knownGoods) > w.cfg.MaxKnownGoodSnapshots {
		toDrop := len(knownGoods) - w.cfg.MaxKnownGoodSnapshots
		for i := 0; i < toDrop; i++ {
			if err := w.ops.vmDelSnapshot(
				ctx, w.cfg.MwanVMID, knownGoods[i],
			); err != nil {
				w.log.Error(
					"vmDelSnapshot",
					"snapshot", knownGoods[i],
					"err", err,
				)
				return err
			}
		}
		out, err = w.ops.vmSnapshots(ctx, w.cfg.MwanVMID)
		if err != nil {
			return err
		}
		s = string(out)
		knownGoods = knownGoodSnapRE.FindAllString(s, -1)
		sort.Strings(knownGoods)
		preDeploys = preDeploySnapRE.FindAllString(s, -1)
		total = len(knownGoods) + len(preDeploys)
	}

	if w.cfg.MaxTotalSnapshots <= 0 ||
		total <= w.cfg.MaxTotalSnapshots || len(knownGoods) == 0 {
		return nil
	}
	excess := total - w.cfg.MaxTotalSnapshots
	if excess > len(knownGoods) {
		excess = len(knownGoods)
	}
	for i := 0; i < excess; i++ {
		if err := w.ops.vmDelSnapshot(
			ctx, w.cfg.MwanVMID, knownGoods[i],
		); err != nil {
			w.log.Error(
				"vmDelSnapshot max total",
				"snapshot", knownGoods[i],
				"err", err,
			)
			return err
		}
	}
	return nil
}

func (w *watchdog) checkDeploy(ctx context.Context) (int64, bool) {
	running, err := w.ops.vmStatus(ctx, w.cfg.MwanVMID)
	if err != nil {
		w.log.Error("checkDeploy: vmStatus error", "err", err)
		return 0, false
	}
	if !running {
		w.log.Info(
			"checkDeploy: VM is not running; cannot check change window",
			"vmid", w.cfg.MwanVMID,
		)
		return 0, false
	}

	w.log.Info(
		"checkDeploy: reading change window markers",
		"last_deploy_path", w.nc.LastDeployPath,
		"last_change_path", w.nc.LastChangePath,
		"vmid", w.cfg.MwanVMID,
	)

	deployTS, dOK := w.readGuestUnix(ctx, w.nc.LastDeployPath)
	changeTS, cOK := w.readGuestUnix(ctx, w.nc.LastChangePath)

	var candidates []int64
	if dOK {
		candidates = append(candidates, deployTS)
	}
	if cOK {
		candidates = append(candidates, changeTS)
	}
	if w.hashChangeWindowStart > 0 {
		candidates = append(candidates, w.hashChangeWindowStart)
	}
	if len(candidates) == 0 {
		w.log.Info("checkDeploy: no change markers or hash window")
		return 0, false
	}
	effective := candidates[0]
	for _, t := range candidates[1:] {
		if t > effective {
			effective = t
		}
	}

	ageMin := (time.Now().Unix() - effective) / 60
	w.log.Info(
		"checkDeploy: change window",
		"deploy_ts", deployTS,
		"deploy_ok", dOK,
		"change_ts", changeTS,
		"change_ok", cOK,
		"hash_window_ts", w.hashChangeWindowStart,
		"effective_ts", effective,
		"age_minutes", ageMin,
		"window_minutes", w.cfg.DeployWindowMinutes,
	)
	if ageMin > int64(w.cfg.DeployWindowMinutes) {
		w.log.Info(
			"checkDeploy: change window stale",
			"age_minutes", ageMin,
			"window_minutes", w.cfg.DeployWindowMinutes,
		)
		return 0, false
	}

	w.log.Info(
		"checkDeploy: within change window",
		"effective_ts", effective,
		"age_minutes", ageMin,
	)
	return effective, true
}

func (w *watchdog) rollback(ctx context.Context, deployTS int64, snap string) {
	w.hashChangeWindowStart = 0
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
	sysCtx := buildSystemContext()
	msg := fmt.Sprintf(
		"=== MWAN AUTO-ROLLBACK EXECUTED ===\n\n"+
			"Time:              %s\n"+
			"VM ID:             %s\n"+
			"PVE Node:          %s\n"+
			"Snapshot:          %s\n"+
			"Deploy timestamp:  %d  (%ds ago, within %dm window)\n\n"+
			"--- WHAT HAPPENED ---\n"+
			"The MWAN watchdog detected sustained connectivity failure consistent with a bad\n"+
			"MWAN configuration deploy. It has stopped the VM, rolled it back to the snapshot\n"+
			"above, and restarted it.\n\n"+
			"--- ROLLBACK SEQUENCE ---\n"+
			"  1. qm stop   %s\n"+
			"  2. qm rollback %s %s\n"+
			"  3. qm start  %s\n\n"+
			"--- CONNECTIVITY PROBE HISTORY ---\n"+
			"%s\n\n"+
			"%s"+
			"--- HOST INTERFACE STATE AT ROLLBACK ---\n"+
			"%s\n"+
			"--- CONFIG AT ROLLBACK ---\n"+
			"  deploy_window_minutes=%d  timeout_seconds=%d\n"+
			"  check_interval_healthy=%s  check_interval_degraded=%s\n"+
			"  alert_cooldown_seconds=%d  post_rollback_grace=%s\n"+
			"  rollback_state_file=%s\n"+
			"  rollback_lock_file=%s\n"+
			"  wan_interfaces=%s\n"+
			"  ping_target_ipv4=%s  ping_target_ipv6=%s\n\n"+
			"--- NEXT STEPS ---\n"+
			"Monitor WAN routing recovery and verify connectivity.\n"+
			"If routing does not recover within %s, manual intervention is needed.\n"+
			"Check the rollback state file to confirm rollback was recorded: %s\n",
		time.Now().Format(time.RFC3339),
		w.cfg.MwanVMID,
		w.cfg.PVENode,
		snap,
		deployTS, deployAge, w.cfg.DeployWindowMinutes,
		w.cfg.MwanVMID,
		w.cfg.MwanVMID, snap,
		w.cfg.MwanVMID,
		probeHistory,
		w.channelSummarySection(),
		sysCtx,
		w.cfg.DeployWindowMinutes,
		w.cfg.ConnectivityTimeoutSeconds,
		w.cfg.CheckIntervalHealthy,
		w.cfg.CheckIntervalDegraded,
		w.cfg.AlertCooldownSeconds,
		w.cfg.PostRollbackGraceSeconds,
		w.cfg.RollbackStateFile,
		w.cfg.RollbackLockFile,
		strings.Join(w.nc.wanIfaceNames(), ", "),
		w.nc.PingTargetIPv4,
		w.nc.PingTargetIPv6,
		w.cfg.PostRollbackGraceSeconds,
		w.cfg.RollbackStateFile,
	)
	w.log.Info("Sending rollback notification email...")
	if err := w.ops.sendEmail(
		ctx, w.cfg.AlertEmail,
		fmt.Sprintf("MWAN AUTO-ROLLBACK: VM %s rolled back to %s", w.cfg.MwanVMID, snap),
		msg,
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
		if w.exitFn != nil {
			w.exitFn(0)
		} else {
			// Production path: process exit is not exercised in unit tests (use exitFn).
			os.Exit(0)
		}
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
	w.recoveredFromRollback = true
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
	sysCtx := buildSystemContext()
	body := fmt.Sprintf(
		"=== MWAN PARTIAL CONNECTIVITY DEGRADATION ===\n\n"+
			"Time:         %s\n"+
			"VM ID:        %s\n"+
			"PVE Node:     %s\n"+
			"Alert to:     %s\n\n"+
			"--- CONNECTIVITY STATUS ---\n"+
			"DOWN:  %s (target: %s)\n"+
			"UP:    %s (target: %s)\n\n"+
			"--- DIAGNOSIS ---\n"+
			"One protocol is down; the other is still working.\n"+
			"This is a PARTIAL degradation -- no rollback will be triggered automatically.\n"+
			"Path: Proxmox host -> OPNsense -> MWAN VM (vmid=%s) -> WAN interfaces.\n"+
			"Manual investigation may be needed if this persists.\n\n"+
			"--- CONSECUTIVE FAIL COUNT ---\n"+
			"Consecutive total failures: %d\n"+
			"Current state:              %s\n\n"+
			"--- PROBE HISTORY (this cycle) ---\n"+
			"%s\n\n"+
			"%s"+
			"--- HOST INTERFACE STATE ---\n"+
			"%s\n"+
			"--- WAN INTERFACES CONFIGURED ---\n"+
			"  %s\n\n"+
			"--- CONFIG ---\n"+
			"  deploy_window_minutes=%d  timeout_seconds=%d\n"+
			"  check_interval_healthy=%s  check_interval_degraded=%s\n"+
			"  alert_cooldown_seconds=%d  post_rollback_grace=%s\n"+
			"  ping_target_ipv4=%s  ping_target_ipv6=%s\n",
		time.Now().Format(time.RFC3339),
		w.cfg.MwanVMID,
		w.cfg.PVENode,
		w.cfg.AlertEmail,
		proto, w.pingTarget(proto),
		upProto, w.pingTarget(upProto),
		w.cfg.MwanVMID,
		w.consecutiveTotalFails,
		w.lastState,
		w.flushProbeLog(),
		w.channelSummarySection(),
		sysCtx,
		strings.Join(w.nc.wanIfaceNames(), ", "),
		w.cfg.DeployWindowMinutes,
		w.cfg.ConnectivityTimeoutSeconds,
		w.cfg.CheckIntervalHealthy,
		w.cfg.CheckIntervalDegraded,
		w.cfg.AlertCooldownSeconds,
		w.cfg.PostRollbackGraceSeconds,
		w.nc.PingTargetIPv4,
		w.nc.PingTargetIPv6,
	)
	if err := w.ops.sendEmail(
		ctx, w.cfg.AlertEmail,
		fmt.Sprintf("MWAN PARTIAL ALERT: %s connectivity lost (VM %s)", proto, w.cfg.MwanVMID),
		body,
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
	sysCtx := buildSystemContext()
	downDurationSec := 0
	if w.totalDownStartUnix > 0 {
		downDurationSec = int(time.Now().Unix() - w.totalDownStartUnix)
	}
	body := fmt.Sprintf(
		"=== MWAN TOTAL CONNECTIVITY ALERT ===\n\n"+
			"Time:         %s\n"+
			"VM ID:        %s\n"+
			"PVE Node:     %s\n"+
			"Alert to:     %s\n\n"+
			"--- REASON ---\n"+
			"%s\n\n"+
			"--- DETAIL ---\n"+
			"%s\n\n"+
			"--- CONNECTIVITY STATUS ---\n"+
			"IPv4 target:  %s  -- FAILED\n"+
			"IPv6 target:  %s  -- FAILED\n\n"+
			"--- OUTAGE DURATION ---\n"+
			"Consecutive total failures: %d\n"+
			"Outage duration so far:     %d seconds\n"+
			"Current state:              %s\n\n"+
			"--- PROBE HISTORY (this cycle) ---\n"+
			"%s\n\n"+
			"%s"+
			"--- HOST INTERFACE STATE ---\n"+
			"%s\n"+
			"--- WAN INTERFACES CONFIGURED ---\n"+
			"  %s\n\n"+
			"--- CONFIG ---\n"+
			"  deploy_window_minutes=%d  timeout_seconds=%d\n"+
			"  check_interval_healthy=%s  check_interval_degraded=%s\n"+
			"  alert_cooldown_seconds=%d  post_rollback_grace=%s\n"+
			"  rollback_state_file=%s\n"+
			"  rollback_lock_file=%s\n"+
			"  last_deploy_path=%s\n",
		time.Now().Format(time.RFC3339),
		w.cfg.MwanVMID,
		w.cfg.PVENode,
		w.cfg.AlertEmail,
		reason,
		detail,
		w.nc.PingTargetIPv4,
		w.nc.PingTargetIPv6,
		w.consecutiveTotalFails,
		downDurationSec,
		w.lastState,
		w.flushProbeLog(),
		w.channelSummarySection(),
		sysCtx,
		strings.Join(w.nc.wanIfaceNames(), ", "),
		w.cfg.DeployWindowMinutes,
		w.cfg.ConnectivityTimeoutSeconds,
		w.cfg.CheckIntervalHealthy,
		w.cfg.CheckIntervalDegraded,
		w.cfg.AlertCooldownSeconds,
		w.cfg.PostRollbackGraceSeconds,
		w.cfg.RollbackStateFile,
		w.cfg.RollbackLockFile,
		w.nc.LastDeployPath,
	)
	if err := w.ops.sendEmail(
		ctx, w.cfg.AlertEmail,
		fmt.Sprintf("MWAN TOTAL ALERT: %s (VM %s)", reason, w.cfg.MwanVMID),
		body,
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
	w.sendStartupEmail(ctx)
	w.lastState = stateUnknown
	w.lastHeartbeat = time.Now()
	iteration := 0

	// Launch interface address monitor in a background goroutine.
	// It polls every 30 s and fires an alert + log entry whenever any
	// interface on the vault host gains or loses an IPv4/IPv6 address.
	go w.runIfaceMonitor(ctx)

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

				// Only alert and auto-start when no rollback is in progress.
				// If the rollback lock exists, the watchdog itself stopped the VM
				// intentionally; do not interfere. Also skip if recoverInterrupted
				// already handled this episode (lock was present at startup and removed).
				_, lockErr := os.Stat(w.cfg.RollbackLockFile)
				rollbackInProgress := lockErr == nil || w.recoveredFromRollback
				if rollbackInProgress {
					w.log.Info(
						"VM stopped but rollback lock present; skipping alert and auto-start",
						"lock_file", w.cfg.RollbackLockFile,
					)
				} else {
					w.log.Error(
						"VM stopped unexpectedly; sending alert and attempting restart",
						"vmid", w.cfg.MwanVMID,
					)
					sysCtx := buildSystemContext()
					alertBody := fmt.Sprintf(
						"=== MWAN VM STOPPED UNEXPECTEDLY ===\n\n"+
							"Time:     %s\n"+
							"VM ID:    %s\n"+
							"PVE Node: %s\n\n"+
							"--- WHAT HAPPENED ---\n"+
							"The MWAN VM was found in STOPPED state with no rollback in progress.\n"+
							"This likely means:\n"+
							"  - The VM crashed or was killed by the hypervisor\n"+
							"  - Someone ran 'qm stop %s' manually\n"+
							"  - A power event stopped the VM\n\n"+
							"--- ACTION TAKEN ---\n"+
							"The watchdog has issued 'qm start %s' to restart the VM.\n"+
							"Check VM logs in the Proxmox UI for details.\n\n"+
							"%s"+
							"--- HOST INTERFACE STATE AT DETECTION ---\n"+
							"%s\n"+
							"--- CONFIG ---\n"+
							"  rollback_lock_file=%s  rollback_state_file=%s\n"+
							"  pve_node=%s  pve_token_id=%s\n",
						time.Now().Format(time.RFC3339),
						w.cfg.MwanVMID,
						w.cfg.PVENode,
						w.cfg.MwanVMID,
						w.cfg.MwanVMID,
						w.channelSummarySection(),
						sysCtx,
						w.cfg.RollbackLockFile,
						w.cfg.RollbackStateFile,
						w.cfg.PVENode,
						w.cfg.PVETokenID,
					)
					if err := w.ops.sendEmail(
						ctx,
						w.cfg.AlertEmail,
						fmt.Sprintf("MWAN VM %s stopped unexpectedly", w.cfg.MwanVMID),
						alertBody,
					); err != nil {
						w.log.Error("Failed to send VM-stopped alert email", "err", err)
					} else {
						w.log.Info("VM-stopped alert email sent", "to", w.cfg.AlertEmail)
					}

					w.log.Info("Attempting to start stopped VM", "vmid", w.cfg.MwanVMID)
					if startErr := w.ops.vmStart(ctx, w.cfg.MwanVMID); startErr != nil {
						w.log.Error(
							"vmStart failed for stopped VM",
							"vmid", w.cfg.MwanVMID,
							"err", startErr,
						)
					} else {
						w.log.Info("vmStart issued for stopped VM", "vmid", w.cfg.MwanVMID)
					}
				}
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

		if !(v4ok && v6ok) {
			w.consecutiveHealthy = 0
			w.healthyCyclesForHash = 0
		}
		if v4ok && v6ok {
			w.consecutiveHealthy++
			w.healthyCyclesForHash++
			if w.cfg.HashCheckEveryNHealthy > 0 &&
				w.healthyCyclesForHash >= w.cfg.HashCheckEveryNHealthy {
				w.healthyCyclesForHash = 0
				w.checkConfigHash(ctx)
			}
			w.maybeSnapshot(ctx)
		}

		if v4ok && v6ok {
			if w.lastState != stateHealthy {
				w.log.Info(
					"Connectivity OK: IPv4 and IPv6",
					"previous_state", w.lastState,
				)
			} else if time.Since(w.lastHeartbeat) >= w.heartbeatTick() {
				w.log.Info(
					"Heartbeat: connectivity healthy",
					"ping_target_ipv4", w.nc.PingTargetIPv4,
					"ping_target_ipv6", w.nc.PingTargetIPv6,
					"iteration", iteration,
				)
				if w.tracker != nil {
					w.tracker.logAll(w.log)
				}
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

// runIfaceMonitor polls the vault host's interface addresses every 30 seconds.
// When any address is added or removed on any interface, it logs the change and
// sends an alert email so network changes are never silent.
func (w *watchdog) runIfaceMonitor(ctx context.Context) {
	const pollInterval = 30 * time.Second
	prev := collectIfaceAddrs()
	w.log.Info(
		"Interface monitor started",
		"poll_interval", pollInterval,
		"interface_count", len(prev),
	)
	for {
		select {
		case <-ctx.Done():
			w.log.Info("Interface monitor stopped (context cancelled)")
			return
		case <-time.After(pollInterval):
		}
		curr := collectIfaceAddrs()
		diff := diffIfaceAddrs(prev, curr)
		if diff == "" {
			prev = curr
			continue
		}
		w.log.Warn(
			"HOST INTERFACE ADDRESS CHANGE DETECTED",
			"diff", diff,
		)
		sysCtx := buildSystemContext()
		body := fmt.Sprintf(
			"=== VAULT HOST INTERFACE ADDRESS CHANGE ===\n\n"+
				"Time:     %s\n"+
				"VM ID:    %s\n"+
				"PVE Node: %s\n\n"+
				"--- WHAT CHANGED ---\n"+
				"%s\n\n"+
				"--- CURRENT HOST INTERFACE STATE ---\n"+
				"%s\n"+
				"This change was detected by the mwan-watchdog interface monitor.\n"+
				"If this is unexpected, check vault's network config, OPNsense, and the MWAN VM.\n",
			time.Now().Format(time.RFC3339),
			w.cfg.MwanVMID,
			w.cfg.PVENode,
			diff,
			sysCtx,
		)
		if err := w.ops.sendEmail(
			ctx,
			w.cfg.AlertEmail,
			fmt.Sprintf(
				"VAULT HOST: interface address change at %s",
				time.Now().Format("2006-01-02 15:04:05"),
			),
			body,
		); err != nil {
			w.log.Error("Interface change alert email error", "err", err)
		} else {
			w.log.Info("Interface change alert email sent", "to", w.cfg.AlertEmail)
		}
		prev = curr
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
			"No recent change within window; cannot auto-rollback (manual intervention needed)",
		)
		w.sendTotalAlert(
			ctx,
			"MWAN routing broken but no recent change within window",
			fmt.Sprintf(
				"MWAN routing is broken (ISP reachable per-interface, "+
					"default route failed) but no deploy/change/hash window was found "+
					"within the %dm window.\n\nThis may indicate:\n"+
					"  - A spontaneous routing failure (nftables, npd, etc.)\n"+
					"  - A change that happened outside the tracking window\n"+
					"  - Change marker files are missing from the VM\n\n"+
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
		w.log.Info("No rollback snapshot found; cannot rollback")
		w.sendTotalAlert(
			ctx,
			"MWAN routing broken after change but no rollback snapshot exists",
			fmt.Sprintf(
				"A recent change (effective ts=%d) was detected and routing is broken, "+
					"but no pre-deploy-* or known-good-* snapshot was found.\n\n"+
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
