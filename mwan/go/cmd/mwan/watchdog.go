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
	lastManifest          map[string]string // path -> sha256hex from previous run
	hashChangeWindowStart int64
	consecutiveHealthy    int
	lastSnapshotAt        time.Time
	healthyCyclesForHash  int

	postRollbackGraceUntil time.Time
	lastHashCheckOK        bool
	totalFailStart         time.Time
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
	ok, _ := w.guestExecProbe(ctx, args...)
	return ok
}

func (w *watchdog) guestExecProbe(ctx context.Context, args ...string) (bool, bool) {
	parsed, err := w.ops.guestExec(ctx, w.cfg.MwanVMID, args...)
	if err != nil {
		if errors.Is(err, ErrGuestExecUnavailable) {
			w.log.Warn(
				"guestExec unavailable",
				"args", strings.Join(args, " "),
				"err", err,
			)
			return false, true
		}
		w.log.Error(
			"guestExec error",
			"args", strings.Join(args, " "),
			"err", err,
		)
		return false, false
	}
	if parsed.ExitCode != 0 {
		w.log.Info(
			"guestExec non-zero exit",
			"args", strings.Join(args, " "),
			"exit_code", parsed.ExitCode,
		)
		return false, false
	}
	return true, false
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
	v6ok, v6Unavailable := w.guestExecProbe(
		ctx, "ping6", "-c", "2", "-W", "3", w.nc.PingTargetIPv6,
	)
	if v6ok {
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
	v4ok, v4Unavailable := w.guestExecProbe(
		ctx, "ping", "-c", "2", "-W", "3", w.nc.PingTargetIPv4,
	)
	if v4ok {
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
	if v6Unavailable || v4Unavailable {
		w.log.Warn(
			"VM default-route probes unavailable due to guest-exec transport",
			"vmid", w.cfg.MwanVMID,
		)
		w.appendProbe(fmt.Sprintf(
			"VM %s default-route probes unavailable (guest-exec transport)",
			w.cfg.MwanVMID,
		))
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
		v4ok, v4Unavailable := w.guestExecProbe(
			ctx, "ping", "-c", "3", "-W", "3", "-I", iface, w.nc.PingTargetIPv4,
		)
		v6ok, v6Unavailable := w.guestExecProbe(
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
		if v4Unavailable || v6Unavailable {
			w.log.Warn(
				"ISP probe via WAN interface unavailable due to guest-exec transport",
				"interface", iface,
			)
			w.appendProbe(fmt.Sprintf(
				"WAN %s: probe unavailable (guest-exec transport)",
				iface,
			))
			continue
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
		if errors.Is(err, ErrGuestExecUnavailable) {
			w.log.Warn(
				"PVE guest-exec unavailable; cannot read deploy timestamp; assuming no recent deploy",
				"vmid", w.cfg.MwanVMID,
			)
		} else {
			w.log.Error("guestExec(cat) error", "path", path, "err", err)
		}
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

// parseManifest parses a manifest in sha256sum(1) format: "<hash>  <path>\n".
// Returns a map of path -> sha256hex. Lines that don't match the format are
// silently skipped (e.g. legacy plain-path manifests from before this change).
func parseManifest(raw string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// sha256sum format: 64 hex chars, two spaces, then the path.
		if len(line) < 66 || line[64] != ' ' || line[65] != ' ' {
			continue
		}
		hash := line[:64]
		path := line[66:]
		if path != "" {
			m[path] = hash
		}
	}
	return m
}

// manifestDiff compares two path->sha256hex maps and returns a formatted
// summary of changed, added, and removed files for inclusion in an email.
func manifestDiff(prev, curr map[string]string) string {
	if len(prev) == 0 || len(curr) == 0 {
		if len(curr) == 0 {
			return "  (manifest unavailable for current state)\n"
		}
		var lines []string
		for path := range curr {
			lines = append(lines, "  "+path)
		}
		sort.Strings(lines)
		return strings.Join(lines, "\n") + "\n"
	}

	var changed, added, removed []string
	for path, hash := range curr {
		if oldHash, ok := prev[path]; !ok {
			added = append(added, path)
		} else if hash != oldHash {
			changed = append(changed, path)
		}
	}
	for path := range prev {
		if _, ok := curr[path]; !ok {
			removed = append(removed, path)
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(removed)

	if len(changed) == 0 && len(added) == 0 && len(removed) == 0 {
		return "  (no per-file diff available — composite hash changed)\n"
	}
	var sb strings.Builder
	for _, p := range changed {
		sb.WriteString("  modified: ")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	for _, p := range added {
		sb.WriteString("  added:    ")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	for _, p := range removed {
		sb.WriteString("  removed:  ")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (w *watchdog) checkConfigHash(ctx context.Context) {
	resp, usedChannel, err := w.ops.getConfigState(ctx, w.cfg.MwanVMID)
	if err != nil {
		w.log.Warn("checkConfigHash getConfigState", "err", err)
		w.lastHashCheckOK = false
		return
	}
	if usedChannel == "tcp" {
		w.log.Warn(
			"getConfigState: vsock unavailable, used TCP fallback",
			"channel", usedChannel,
		)
	}
	h := strings.TrimSpace(resp.GetConfigHash())
	if h == "" {
		return
	}
	currentManifest := parseManifest(resp.GetConfigManifest())

	if w.lastConfigHash != "" && h != w.lastConfigHash {
		if !w.postRollbackGraceUntil.IsZero() &&
			time.Now().Before(w.postRollbackGraceUntil) {
			w.log.Info(
				"Post-rollback hash change suppressed",
				"old_hash", w.lastConfigHash,
				"new_hash", h,
				"grace_until", w.postRollbackGraceUntil,
			)
		} else {
			w.hashChangeWindowStart = time.Now().Unix()
			diffSection := manifestDiff(w.lastManifest, currentManifest)
			w.log.Warn(
				"config hash drift detected",
				"old_hash", w.lastConfigHash,
				"new_hash", resp.GetConfigHash(),
				"changed_files", diffSection,
				"vm_id", w.cfg.MwanVMID,
				"node", w.cfg.PVENode,
				"change_window_minutes", w.cfg.DeployWindowMinutes,
			)
		}
	} else {
		w.log.Debug(
			"config hash check: no drift",
			"hash", h,
			"channel", usedChannel,
		)
		w.lastHashCheckOK = true
	}
	w.lastConfigHash = h
	w.lastManifest = currentManifest
}

func (w *watchdog) maybeSnapshot(ctx context.Context) {
	if w.cfg.SnapshotHealthyThreshold <= 0 {
		return
	}
	if w.consecutiveHealthy < w.cfg.SnapshotHealthyThreshold {
		return
	}
	windowSec := int64(w.cfg.DeployWindowMinutes) * 60
	if w.hashChangeWindowStart > 0 {
		elapsed := time.Now().Unix() - w.hashChangeWindowStart
		if elapsed < windowSec {
			return
		}
	}
	deployTS, dOK := w.readGuestUnix(ctx, w.nc.LastDeployPath)
	if dOK && (time.Now().Unix()-deployTS) < windowSec {
		return
	}
	minGap := time.Duration(w.cfg.MinSnapshotIntervalSeconds) * time.Second
	if !w.lastSnapshotAt.IsZero() && time.Since(w.lastSnapshotAt) < minGap {
		return
	}
	if w.cfg.HashCheckEveryNHealthy > 0 && !w.lastHashCheckOK {
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
	w.totalFailStart = time.Time{}
	w.consecutiveHealthy = 0
	defer w.coord.setRollingBack(false)

	var rollbackErr error

	_ = w.flushProbeLog()

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

	// Delete any watchdog-managed snapshots that are children of the target.
	// Proxmox/ZFS only allows rollback to the leaf snapshot in the chain.
	if listOut, lErr := w.ops.vmSnapshots(ctx, w.cfg.MwanVMID); lErr == nil {
		toDelete := snapshotsAfter(listOut, snap)
		for i := len(toDelete) - 1; i >= 0; i-- {
			child := toDelete[i]
			w.log.Info("Deleting intermediate snapshot before rollback",
				"snapshot", child, "target", snap)
			if dErr := w.ops.vmDelSnapshot(ctx, w.cfg.MwanVMID, child); dErr != nil {
				w.log.Error("Failed to delete intermediate snapshot",
					"snapshot", child, "err", dErr)
			}
		}
	} else {
		w.log.Warn("Could not list snapshots before rollback", "err", lErr)
	}

	rollbackStart := time.Now()
	w.log.Info(
		"Running qm rollback",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"timeout", timeoutQmRollback,
	)
	if err := w.ops.vmRollback(ctx, w.cfg.MwanVMID, snap); err != nil {
		rollbackErr = err
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

	rollbackAttempts := 1
	if existing, att, _ := rollbackAlreadyDone(
		w.cfg.RollbackStateFile, deployTS,
	); !existing {
		rollbackAttempts = att + 1
	}
	rollbackSucceeded := rollbackErr == nil
	if writeErr := writeRollbackState(
		w.cfg.RollbackStateFile, deployTS, snap,
		rollbackAttempts, rollbackSucceeded,
	); writeErr != nil {
		w.log.Error("write rollback state", "err", writeErr)
	} else {
		w.log.Info(
			"Wrote rollback state",
			"path", w.cfg.RollbackStateFile,
			"deploy_ts", deployTS,
			"snapshot", snap,
			"success", rollbackSucceeded,
			"attempts", rollbackAttempts,
		)
	}

	w.log.Error(
		"auto-rollback completed",
		"vm_id", w.cfg.MwanVMID,
		"snapshot", snap,
		"node", w.cfg.PVENode,
	)

	w.log.Info(
		"ROLLBACK COMPLETE; waiting for routes to converge",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"grace", w.cfg.PostRollbackGraceSeconds,
	)
	w.postRollbackGraceUntil = time.Now().Add(
		time.Duration(w.cfg.DeployWindowMinutes) * time.Minute,
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
	_ = ctx
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
	w.log.Warn(
		"connectivity partial loss",
		"proto", proto,
		"vm_id", w.cfg.MwanVMID,
		"node", w.cfg.PVENode,
	)
}

func (w *watchdog) pingTarget(proto string) string {
	if proto == "IPv6" {
		return w.nc.PingTargetIPv6
	}
	return w.nc.PingTargetIPv4
}

func (w *watchdog) sendTotalAlert(ctx context.Context, reason, detail string) {
	_ = ctx
	_ = detail
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
	w.log.Error(
		"connectivity total loss",
		"reason", reason,
		"vm_id", w.cfg.MwanVMID,
		"node", w.cfg.PVENode,
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

	// Startup checks.
	if listOut, lErr := w.ops.vmSnapshots(ctx, w.cfg.MwanVMID); lErr == nil {
		if extractLatestSnapshot(listOut) == "" {
			w.log.Warn(
				"No rollback snapshots found at startup",
				"vmid", w.cfg.MwanVMID,
				"note", "rollback will not be possible until a snapshot is created",
			)
		}
	} else {
		w.log.Warn("Could not check snapshots at startup", "err", lErr)
	}
	if data, lErr := os.ReadFile(w.cfg.RollbackLockFile); lErr == nil {
		w.log.Warn(
			"Stale rollback lock file found at startup",
			"path", w.cfg.RollbackLockFile,
			"content", strings.TrimSpace(string(data)),
			"note", "a previous run may have crashed mid-rollback",
		)
	}

	// Run a connectivity probe and config hash check before the startup log
	// so the channel tracker and ping results reflect actual startup state.
	// Only probe if the VM is currently running.
	var startupV4, startupV6 bool
	if running, err := w.ops.vmStatus(ctx, w.cfg.MwanVMID); err == nil && running {
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
	w.log.Info(
		"watchdog started",
		"vm_id", w.cfg.MwanVMID,
		"node", w.cfg.PVENode,
		"build", buildVersionString(),
		"ipv4", v4str,
		"ipv6", v6str,
		"deploy_window_minutes", w.cfg.DeployWindowMinutes,
		"check_interval_healthy", w.cfg.CheckIntervalHealthy,
		"wan_interfaces", strings.Join(w.nc.wanIfaceNames(), ","),
	)
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
						"VM stopped unexpectedly",
						"vm_id", w.cfg.MwanVMID,
						"node", w.cfg.PVENode,
					)

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
			"vault host interface address changed",
			"diff", diff,
			"vm_id", w.cfg.MwanVMID,
			"node", w.cfg.PVENode,
		)
		prev = curr
	}
}

func (w *watchdog) handleTimeoutExceeded(ctx context.Context) {
	w.log.Info("--- DIAGNOSIS START ---")

	w.log.Info("Step 1: checking for recent config change...")
	deployTS, recent := w.checkDeploy(ctx)

	if !recent {
		w.log.Info(
			"No recent config change; running diagnostics for alert context",
		)
		vmOK := w.testVMConnectivity(ctx)
		w.testISP(ctx)

		var reason, detail string
		if vmOK {
			reason = "Proxmox host cannot reach internet but MWAN VM can"
			detail = "VM has internet via default route. " +
				"This suggests a Proxmox-side routing or " +
				"OPNsense issue, not an MWAN configuration " +
				"problem.\nNo rollback triggered. " +
				"Manual investigation needed."
		} else {
			reason = "Total connectivity loss, no recent config change"
			detail = fmt.Sprintf(
				"All connectivity tests failed and no config "+
					"change detected within %dm.\n"+
					"Treating as external outage. "+
					"No rollback triggered.",
				w.cfg.DeployWindowMinutes,
			)
		}

		w.sendTotalAlert(ctx, reason, detail)
		w.log.Info(
			"--- DIAGNOSIS END (no recent config change) ---",
			"reason", reason,
		)
		sleepOrDone(ctx, 60*time.Second)
		return
	}

	if w.cfg.DeployGracePeriodSeconds > 0 {
		rawDeployTS, dOK := w.readGuestUnix(
			ctx, w.nc.LastDeployPath,
		)
		if dOK {
			deployAge := time.Now().Unix() - rawDeployTS
			grace := int64(w.cfg.DeployGracePeriodSeconds)
			if deployAge >= 0 && deployAge < grace {
				remaining := grace - deployAge
				w.log.Info(
					"Within deploy grace period; waiting for "+
						"VM to stabilize",
					"deploy_ts", rawDeployTS,
					"deploy_age_seconds", deployAge,
					"remaining_seconds", remaining,
					"grace_period_seconds",
					w.cfg.DeployGracePeriodSeconds,
				)
				w.log.Info(
					"--- DIAGNOSIS END (deploy grace period) ---",
				)
				sleepOrDone(
					ctx,
					time.Duration(remaining)*time.Second,
				)
				return
			}
		}
	}

	w.log.Info(
		"Config recently changed and connectivity still down",
		"deploy_ts", deployTS,
	)

	w.log.Info(
		"Step 2: checking rollback state",
		"deploy_ts", deployTS,
	)
	already, attempts, err := rollbackAlreadyDone(
		w.cfg.RollbackStateFile, deployTS,
	)
	if err != nil {
		w.log.Error(
			"read rollback state file (proceeding cautiously)",
			"path", w.cfg.RollbackStateFile,
			"err", err,
		)
	}
	if already {
		w.log.Info(
			"Rollback already performed for this deploy_ts; "+
				"not rolling back again",
			"deploy_ts", deployTS,
		)
		w.log.Info("--- DIAGNOSIS END (rollback already done) ---")
		sleepOrDone(ctx, 60*time.Second)
		return
	}
	if w.cfg.MaxRollbackAttempts > 0 && attempts >= w.cfg.MaxRollbackAttempts {
		w.log.Error(
			"Rollback attempt limit reached; manual intervention required",
			"deploy_ts", deployTS,
			"attempts", attempts,
			"max_attempts", w.cfg.MaxRollbackAttempts,
		)
		w.log.Info("--- DIAGNOSIS END (rollback exhausted) ---")
		sleepOrDone(ctx, 60*time.Second)
		return
	}

	w.log.Info("Step 3: finding rollback snapshot...")
	snap, snapErr := w.findSnapshot(ctx)
	if snapErr != nil {
		w.log.Error("listsnapshot error", "err", snapErr)
	}
	if snap == "" {
		w.log.Info("No rollback snapshot found; cannot rollback")
		w.sendTotalAlert(
			ctx,
			"Config changed but no rollback snapshot exists",
			fmt.Sprintf(
				"A recent change (effective ts=%d) was "+
					"detected and connectivity is broken, "+
					"but no pre-deploy-* or known-good-* "+
					"snapshot was found.\n\n"+
					"Manual intervention required.",
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
