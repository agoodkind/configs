package watchdog

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

	"goodkind.io/mwan/internal/alert"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/ops"
	"goodkind.io/mwan/internal/rollback"
	"goodkind.io/mwan/internal/version"
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
	cfg     *config.Config
	ops     ops.SysOps
	coord   *alert.Coord
	limiter *alert.Limiter
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
	tracker *ops.ChannelTracker

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

func (w *watchdog) guestExecProbe(ctx context.Context, args ...string) (bool, bool) {
	parsed, err := w.ops.GuestExec(ctx, w.cfg.MwanVMID, args...)
	if err != nil {
		if errors.Is(err, ops.ErrGuestExecUnavailable) {
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
		ok := w.ops.Ping(ctx, "ping", w.cfg.Network.PingTargetIPv4)
		mu.Lock()
		v4ok = ok
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		ok := w.ops.Ping(ctx, "ping6", w.cfg.Network.PingTargetIPv6)
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
		"ipv4_target", w.cfg.Network.PingTargetIPv4,
		"ipv4", v4str,
		"ipv6_target", w.cfg.Network.PingTargetIPv6,
		"ipv6", v6str,
	)
	w.appendProbe(fmt.Sprintf(
		"Host probe: IPv4 %s (%s), IPv6 %s (%s)",
		w.cfg.Network.PingTargetIPv4, v4str,
		w.cfg.Network.PingTargetIPv6, v6str,
	))
	return v4ok, v6ok
}

// testVMConnectivity pings through the VM's default route to distinguish a
// MWAN routing failure from a Proxmox-side issue.
func (w *watchdog) testVMConnectivity(ctx context.Context) bool {
	w.log.Info(
		"Testing VM default-route connectivity",
		"vmid", w.cfg.MwanVMID,
		"ping6_target", w.cfg.Network.PingTargetIPv6,
		"ping_target", w.cfg.Network.PingTargetIPv4,
	)
	v6ok, v6Unavailable := w.guestExecProbe(
		ctx, "ping6", "-c", "2", "-W", "3", w.cfg.Network.PingTargetIPv6,
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
		ctx, "ping", "-c", "2", "-W", "3", w.cfg.Network.PingTargetIPv4,
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
	ifaces := w.cfg.Network.WanIfaceNames()
	w.log.Info(
		"Testing ISP reachability via WAN interfaces",
		"wan_count", len(ifaces),
		"interfaces", strings.Join(ifaces, ", "),
	)
	for _, iface := range ifaces {
		v4ok, v4Unavailable := w.guestExecProbe(
			ctx, "ping", "-c", "3", "-W", "3", "-I", iface, w.cfg.Network.PingTargetIPv4,
		)
		v6ok, v6Unavailable := w.guestExecProbe(
			ctx, "ping6", "-c", "3", "-W", "3", "-I", iface, w.cfg.Network.PingTargetIPv6,
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
	out, err := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID)
	if err != nil {
		return "", err
	}
	snap := rollback.ExtractLatestSnapshot(out)
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
	parsed, err := w.ops.GuestExec(ctx, w.cfg.MwanVMID, "cat", path)
	if err != nil {
		if errors.Is(err, ops.ErrGuestExecUnavailable) {
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
	for line := range strings.SplitSeq(raw, "\n") {
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

// categorizeManifestChanges compares two path->sha256hex maps and returns
// sorted lists of changed, added, and removed paths.
func categorizeManifestChanges(prev, curr map[string]string) (changed, added, removed []string) {
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
	return changed, added, removed
}

// manifestDiff compares two path->sha256hex maps and returns a formatted
// summary of changed, added, and removed files for inclusion in an email.
func manifestDiff(prev, curr map[string]string) string {
	if len(curr) == 0 {
		return "  (manifest unavailable for current state)\n"
	}
	if len(prev) == 0 {
		var lines []string
		for path := range curr {
			lines = append(lines, "  "+path)
		}
		sort.Strings(lines)
		return strings.Join(lines, "\n") + "\n"
	}

	changed, added, removed := categorizeManifestChanges(prev, curr)
	if len(changed) == 0 && len(added) == 0 && len(removed) == 0 {
		return "  (no per-file diff available -- composite hash changed)\n"
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
	resp, usedChannel, err := w.ops.GetConfigState(ctx, w.cfg.MwanVMID)
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
				"node", w.cfg.PVE.Node,
				"change_window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
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
	if w.cfg.Watchdog.SnapshotHealthyThreshold <= 0 {
		return
	}
	if w.consecutiveHealthy < w.cfg.Watchdog.SnapshotHealthyThreshold {
		return
	}
	windowSec := int64(w.cfg.Watchdog.DeployWindowMinutes) * 60
	if w.hashChangeWindowStart > 0 {
		elapsed := time.Now().Unix() - w.hashChangeWindowStart
		if elapsed < windowSec {
			return
		}
	}
	deployTS, dOK := w.readGuestUnix(ctx, w.cfg.Network.LastDeployPath)
	if dOK && (time.Now().Unix()-deployTS) < windowSec {
		return
	}
	minGap := time.Duration(w.cfg.Watchdog.MinSnapshotIntervalSeconds) * time.Second
	if !w.lastSnapshotAt.IsZero() && time.Since(w.lastSnapshotAt) < minGap {
		return
	}
	if w.cfg.Watchdog.HashCheckEveryNHealthy > 0 && !w.lastHashCheckOK {
		return
	}
	name := fmt.Sprintf(
		"known-good-%s",
		time.Now().Format("20060102-150405"),
	)
	if err := w.ops.VMSnapshot(ctx, w.cfg.MwanVMID, name); err != nil {
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
	out, err := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID)
	if err != nil {
		return err
	}
	s := string(out)
	knownGoods := rollback.KnownGoodSnapRE.FindAllString(s, -1)
	sort.Strings(knownGoods)
	preDeploys := rollback.PreDeploySnapRE.FindAllString(s, -1)
	total := len(knownGoods) + len(preDeploys)

	if w.cfg.Watchdog.MaxKnownGoodSnapshots > 0 &&
		len(knownGoods) > w.cfg.Watchdog.MaxKnownGoodSnapshots {
		toDrop := len(knownGoods) - w.cfg.Watchdog.MaxKnownGoodSnapshots
		for i := range toDrop {
			if err := w.ops.VMDelSnapshot(
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
		out, err = w.ops.VMSnapshots(ctx, w.cfg.MwanVMID)
		if err != nil {
			return err
		}
		s = string(out)
		knownGoods = rollback.KnownGoodSnapRE.FindAllString(s, -1)
		sort.Strings(knownGoods)
		preDeploys = rollback.PreDeploySnapRE.FindAllString(s, -1)
		total = len(knownGoods) + len(preDeploys)
	}

	if w.cfg.Watchdog.MaxTotalSnapshots <= 0 ||
		total <= w.cfg.Watchdog.MaxTotalSnapshots || len(knownGoods) == 0 {
		return nil
	}
	excess := min(total-w.cfg.Watchdog.MaxTotalSnapshots, len(knownGoods))
	for i := range excess {
		if err := w.ops.VMDelSnapshot(
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
	running, err := w.ops.VMStatus(ctx, w.cfg.MwanVMID)
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
		"last_deploy_path", w.cfg.Network.LastDeployPath,
		"last_change_path", w.cfg.Network.LastChangePath,
		"vmid", w.cfg.MwanVMID,
	)

	deployTS, dOK := w.readGuestUnix(ctx, w.cfg.Network.LastDeployPath)
	changeTS, cOK := w.readGuestUnix(ctx, w.cfg.Network.LastChangePath)

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
		"window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
	)
	if ageMin > int64(w.cfg.Watchdog.DeployWindowMinutes) {
		w.log.Info(
			"checkDeploy: change window stale",
			"age_minutes", ageMin,
			"window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
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

// executeRollbackVM performs the stop-rollback-start cycle on the MWAN VM.
// It deletes intermediate snapshots that are children of the target (Proxmox/ZFS
// requires the target to be a leaf), then runs qm rollback and qm start.
// Returns a non-nil error if the qm rollback command itself failed.
func (w *watchdog) executeRollbackVM(ctx context.Context, snap string) error {
	stopStart := time.Now()
	w.log.Info(
		"Stopping VM",
		"vmid", w.cfg.MwanVMID,
		"timeout", ops.TimeoutQmStop,
	)
	if err := w.ops.VMStop(ctx, w.cfg.MwanVMID); err != nil {
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
	if listOut, lErr := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID); lErr == nil {
		toDelete := rollback.SnapshotsAfter(listOut, snap)
		for i := len(toDelete) - 1; i >= 0; i-- {
			child := toDelete[i]
			w.log.Info("Deleting intermediate snapshot before rollback",
				"snapshot", child, "target", snap)
			if dErr := w.ops.VMDelSnapshot(ctx, w.cfg.MwanVMID, child); dErr != nil {
				w.log.Error("Failed to delete intermediate snapshot",
					"snapshot", child, "err", dErr)
			}
		}
	} else {
		w.log.Warn("Could not list snapshots before rollback", "err", lErr)
	}

	var rollbackErr error
	rollbackStart := time.Now()
	w.log.Info(
		"Running qm rollback",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"timeout", ops.TimeoutQmRollback,
	)
	if err := w.ops.VMRollback(ctx, w.cfg.MwanVMID, snap); err != nil {
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
		"timeout", ops.TimeoutQmStart,
	)
	if err := w.ops.VMStart(ctx, w.cfg.MwanVMID); err != nil {
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
	return rollbackErr
}

// recordRollbackResult persists rollback state, removes the lock file, and
// logs the outcome. Called after executeRollbackVM completes.
func (w *watchdog) recordRollbackResult(deployTS int64, snap string, rollbackErr error) {
	if err := os.Remove(w.cfg.Watchdog.RollbackLockFile); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		w.log.Error("remove rollback lock", "err", err)
	} else {
		w.log.Info("Removed rollback lock file")
	}

	rollbackAttempts := 1
	if existing, att, _ := rollback.AlreadyDone(
		w.cfg.Watchdog.RollbackStateFile, deployTS,
	); !existing {
		rollbackAttempts = att + 1
	}
	rollbackSucceeded := rollbackErr == nil
	if writeErr := rollback.WriteState(
		w.cfg.Watchdog.RollbackStateFile, deployTS, snap,
		rollbackAttempts, rollbackSucceeded,
	); writeErr != nil {
		w.log.Error("write rollback state", "err", writeErr)
	} else {
		w.log.Info(
			"Wrote rollback state",
			"path", w.cfg.Watchdog.RollbackStateFile,
			"deploy_ts", deployTS,
			"snapshot", snap,
			"success", rollbackSucceeded,
			"attempts", rollbackAttempts,
		)
	}

	w.log.Warn(
		"auto-rollback completed",
		"vm_id", w.cfg.MwanVMID,
		"snapshot", snap,
		"node", w.cfg.PVE.Node,
	)
}

func (w *watchdog) rollback(ctx context.Context, deployTS int64, snap string) {
	w.hashChangeWindowStart = 0
	w.coord.SetRollingBack(true)
	w.totalFailStart = time.Time{}
	w.consecutiveHealthy = 0
	defer w.coord.SetRollingBack(false)

	_ = w.flushProbeLog()

	lockContent := fmt.Sprintf(
		"deploy_ts=%d snapshot=%s ts=%d\n",
		deployTS, snap, time.Now().Unix(),
	)
	if err := os.WriteFile(
		w.cfg.Watchdog.RollbackLockFile, []byte(lockContent), 0o644,
	); err != nil {
		w.log.Error("write rollback lock", "err", err)
	} else {
		w.log.Info("Wrote rollback lock", "path", w.cfg.Watchdog.RollbackLockFile)
	}

	w.log.Info(
		"INITIATING ROLLBACK",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"deploy_ts", deployTS,
		"deploy_age_seconds", time.Now().Unix()-deployTS,
	)

	rollbackErr := w.executeRollbackVM(ctx, snap)
	w.recordRollbackResult(deployTS, snap, rollbackErr)

	w.log.Info(
		"ROLLBACK COMPLETE; waiting for routes to converge",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"grace", w.cfg.Watchdog.PostRollbackGraceSeconds,
	)
	w.postRollbackGraceUntil = time.Now().Add(
		time.Duration(w.cfg.Watchdog.DeployWindowMinutes) * time.Minute,
	)
	if w.coord.TakeShutdownAfterRollback() {
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
	data, err := os.ReadFile(w.cfg.Watchdog.RollbackLockFile)
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
	running, statusErr := w.ops.VMStatus(ctx, w.cfg.MwanVMID)
	if statusErr != nil {
		w.log.Error("qm status during recovery", "err", statusErr)
		return
	}
	if running {
		w.log.Info(
			"VM is running; previous rollback completed. Removing lock.",
			"vmid", w.cfg.MwanVMID,
		)
		if err := os.Remove(w.cfg.Watchdog.RollbackLockFile); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			w.log.Error("remove stale rollback lock", "err", err)
		}
		return
	}
	w.log.Info(
		"VM is STOPPED and rollback lock exists; attempting to start VM to complete interrupted rollback",
		"vmid", w.cfg.MwanVMID,
	)
	if startErr := w.ops.VMStart(ctx, w.cfg.MwanVMID); startErr != nil {
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
	if err := os.Remove(w.cfg.Watchdog.RollbackLockFile); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		w.log.Error("remove rollback lock after recovery", "err", err)
	}
	w.recoveredFromRollback = true
	w.log.Info(
		"Waiting for VM to boot and routes to converge after interrupted rollback recovery",
		"grace", w.cfg.Watchdog.PostRollbackGraceSeconds,
	)
	time.Sleep(time.Duration(w.cfg.Watchdog.PostRollbackGraceSeconds) * time.Second)
}

func (w *watchdog) sendPartialAlert(ctx context.Context, proto string) {
	_ = ctx
	if !w.limiter.TrySendPartial(time.Now()) {
		remaining := w.limiter.PartialCooldownRemaining(time.Now())
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
		"node", w.cfg.PVE.Node,
	)
}

func (w *watchdog) sendTotalAlert(ctx context.Context, reason, detail string) {
	_ = ctx
	_ = detail
	if !w.limiter.TrySendTotal(time.Now()) {
		remaining := w.limiter.TotalCooldownRemaining(time.Now())
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
func (w *watchdog) logStartupConfig() {
	w.log.Info(
		"Starting MWAN watchdog",
		"vmid", w.cfg.MwanVMID,
		"deploy_window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
		"connectivity_timeout_seconds", w.cfg.Watchdog.ConnectivityTimeoutSeconds,
		"check_interval_healthy", w.cfg.Watchdog.HealthyInterval(),
		"check_interval_degraded", w.cfg.Watchdog.DegradedInterval(),
		"alert_cooldown_seconds", w.cfg.Watchdog.AlertCooldownSeconds,
	)
	w.log.Info(
		"Network config",
		"ping_target_ipv4", w.cfg.Network.PingTargetIPv4,
		"ping_target_ipv6", w.cfg.Network.PingTargetIPv6,
		"wan_interfaces", strings.Join(w.cfg.Network.WanIfaceNames(), ", "),
		"last_deploy_path", w.cfg.Network.LastDeployPath,
	)
	w.log.Info(
		"PVE",
		"node", w.cfg.PVE.Node,
		"token_id", w.cfg.PVE.TokenID,
		"vsock_cid", w.cfg.Watchdog.VsockCID,
		"vsock_port", w.cfg.Watchdog.VsockPort,
	)
}

// runStartupChecks performs one-time checks when the watchdog loop begins:
// recovering from interrupted rollbacks, verifying snapshots exist, probing
// connectivity, and checking the config hash.
func (w *watchdog) runStartupChecks(ctx context.Context) {
	w.recoverInterrupted(ctx)

	// Startup checks.
	if listOut, lErr := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID); lErr == nil {
		if rollback.ExtractLatestSnapshot(listOut) == "" {
			w.log.Warn(
				"No rollback snapshots found at startup",
				"vmid", w.cfg.MwanVMID,
				"note", "rollback will not be possible until a snapshot is created",
			)
		}
	} else {
		w.log.Warn("Could not check snapshots at startup", "err", lErr)
	}
	if data, lErr := os.ReadFile(w.cfg.Watchdog.RollbackLockFile); lErr == nil {
		w.log.Warn(
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
	w.log.Info(
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
	if w.vmStoppedLogged {
		return
	}
	w.log.Info(
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
		w.log.Info(
			"VM stopped but rollback lock present; skipping alert and auto-start",
			"lock_file", w.cfg.Watchdog.RollbackLockFile,
		)
		return
	}
	w.log.Error(
		"VM stopped unexpectedly",
		"vm_id", w.cfg.MwanVMID,
		"node", w.cfg.PVE.Node,
		"err", "vm transitioned to stopped state outside of mwan control",
	)

	w.log.Info("Attempting to start stopped VM", "vmid", w.cfg.MwanVMID)
	if startErr := w.ops.VMStart(ctx, w.cfg.MwanVMID); startErr != nil {
		w.log.Error(
			"vmStart failed for stopped VM",
			"vmid", w.cfg.MwanVMID,
			"err", startErr,
		)
	} else {
		w.log.Info("vmStart issued for stopped VM", "vmid", w.cfg.MwanVMID)
	}
}

// handleHealthyProbe processes a fully-healthy probe result (both v4 and v6 OK).
// It updates counters, checks config hash periodically, manages snapshots, and
// emits heartbeat logs.
func (w *watchdog) handleHealthyProbe(ctx context.Context, iteration int) {
	w.consecutiveHealthy++
	w.healthyCyclesForHash++
	if w.cfg.Watchdog.HashCheckEveryNHealthy > 0 &&
		w.healthyCyclesForHash >= w.cfg.Watchdog.HashCheckEveryNHealthy {
		w.healthyCyclesForHash = 0
		w.checkConfigHash(ctx)
	}
	w.maybeSnapshot(ctx)

	if w.lastState != stateHealthy {
		w.log.Info(
			"Connectivity OK: IPv4 and IPv6",
			"previous_state", w.lastState,
		)
	} else if time.Since(w.lastHeartbeat) >= w.heartbeatTick() {
		w.log.Info(
			"Heartbeat: connectivity healthy",
			"ping_target_ipv4", w.cfg.Network.PingTargetIPv4,
			"ping_target_ipv6", w.cfg.Network.PingTargetIPv6,
			"iteration", iteration,
		)
		if w.tracker != nil {
			w.tracker.LogAll(w.log)
		}
		w.lastHeartbeat = time.Now()
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
}

// handleTotalLoss processes a probe where both protocols are down.
// It tracks downtime and returns true when the connectivity timeout has been
// exceeded (meaning the caller should invoke handleTimeoutExceeded).
func (w *watchdog) handleTotalLoss() bool {
	w.consecutiveTotalFails++
	now := time.Now().Unix()
	if w.totalDownStartUnix == 0 {
		w.totalDownStartUnix = now
		w.log.Info(
			"TOTAL connectivity loss (IPv4 and IPv6 both FAILED); starting timeout",
			"timeout_seconds", w.cfg.Watchdog.ConnectivityTimeoutSeconds,
			"fail_count", w.consecutiveTotalFails,
		)
	}
	w.lastState = stateDown
	downDuration := int(now - w.totalDownStartUnix)
	remaining := w.cfg.Watchdog.ConnectivityTimeoutSeconds - downDuration
	if downDuration < w.cfg.Watchdog.ConnectivityTimeoutSeconds {
		w.log.Info(
			"Still down before timeout threshold",
			"elapsed_seconds", downDuration,
			"remaining_seconds", remaining,
			"fail_count", w.consecutiveTotalFails,
		)
		return false
	}
	w.log.Info(
		"Timeout exceeded; entering diagnosis",
		"down_seconds", downDuration,
		"threshold_seconds", w.cfg.Watchdog.ConnectivityTimeoutSeconds,
	)
	return true
}

// sleepOrShutdown sleeps for the given duration, returning false if the
// context was cancelled (indicating the caller should return from run).
func (w *watchdog) sleepOrShutdown(ctx context.Context, d time.Duration) bool {
	if !sleepOrDone(ctx, d) {
		w.log.Info("Context cancelled during sleep; watchdog shutting down")
		return false
	}
	return true
}

// runIteration executes one iteration of the watchdog loop.
// Returns false if the loop should exit (context cancelled).
func (w *watchdog) runIteration(ctx context.Context, iteration int) bool {
	select {
	case <-ctx.Done():
		w.log.Info("Context cancelled; watchdog shutting down")
		return false
	default:
	}

	running, err := w.ops.VMStatus(ctx, w.cfg.MwanVMID)
	if err != nil {
		w.log.Error("qm status error", "vmid", w.cfg.MwanVMID, "err", err)
		return w.sleepOrShutdown(ctx, w.cfg.Watchdog.DegradedInterval())
	}
	if !running {
		w.handleVMStopped(ctx)
		return w.sleepOrShutdown(ctx, w.cfg.Watchdog.DegradedInterval())
	}
	if w.vmStoppedLogged {
		w.log.Info("VM is running again", "vmid", w.cfg.MwanVMID)
	}
	w.vmStoppedLogged = false

	v4ok, v6ok := w.probeConnectivity(ctx)
	if !v4ok || !v6ok {
		w.consecutiveHealthy = 0
		w.healthyCyclesForHash = 0
	}

	switch {
	case v4ok && v6ok:
		w.handleHealthyProbe(ctx, iteration)
		return w.sleepOrShutdown(ctx, w.cfg.Watchdog.HealthyInterval())
	case v4ok || v6ok:
		w.handlePartialProbe(ctx, v6ok)
		return w.sleepOrShutdown(ctx, w.cfg.Watchdog.DegradedInterval())
	default:
		if w.handleTotalLoss() {
			w.handleTimeoutExceeded(ctx)
		}
		return w.sleepOrShutdown(ctx, w.cfg.Watchdog.DegradedInterval())
	}
}

func (w *watchdog) run(ctx context.Context) {
	w.logStartupConfig()
	w.runStartupChecks(ctx)
	w.lastState = stateUnknown
	w.lastHeartbeat = time.Now()
	iteration := 0

	go w.runIfaceMonitor(ctx)

	for {
		if w.cfg.Watchdog.MaxIterations > 0 && iteration >= w.cfg.Watchdog.MaxIterations {
			w.log.Info("Reached max iterations; exiting", "max", w.cfg.Watchdog.MaxIterations)
			return
		}
		iteration++
		if !w.runIteration(ctx, iteration) {
			return
		}
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
			"node", w.cfg.PVE.Node,
		)
		prev = curr
	}
}

// diagnoseNoRecentChange runs VM and ISP connectivity diagnostics when no
// recent config change was detected. It may trigger a failover to the LXC.
// Returns true if a failover was triggered (caller should return early).
func (w *watchdog) diagnoseNoRecentChange(ctx context.Context) bool {
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
		// VM has no internet and no config changed.
		// Check if failover LXC can reach the internet.
		// If yes: failover is useful (VM routing broken, LXC WAN works).
		// If no: real ISP outage, failover is pointless.
		if w.cfg.Cutover.FailoverLXCID != "" {
			if w.tryFailover(ctx, w.cfg, "Total connectivity loss on primary, no recent config change") {
				return true
			}
		}
		reason = "Total connectivity loss, no recent config change"
		detail = fmt.Sprintf(
			"All connectivity tests failed and no config "+
				"change detected within %dm.\n"+
				"Failover LXC also unreachable or not configured. "+
				"Treating as external outage.",
			w.cfg.Watchdog.DeployWindowMinutes,
		)
	}

	w.sendTotalAlert(ctx, reason, detail)
	w.log.Info(
		"--- DIAGNOSIS END (no recent config change) ---",
		"reason", reason,
	)
	return false
}

// attemptRollbackForDeploy checks rollback eligibility, finds a snapshot, and
// triggers a rollback if appropriate. Returns true if a rollback was executed.
func (w *watchdog) attemptRollbackForDeploy(ctx context.Context, deployTS int64) bool {
	w.log.Info(
		"Step 2: checking rollback state",
		"deploy_ts", deployTS,
	)
	already, attempts, err := rollback.AlreadyDone(
		w.cfg.Watchdog.RollbackStateFile, deployTS,
	)
	if err != nil {
		w.log.Error(
			"read rollback state file (proceeding cautiously)",
			"path", w.cfg.Watchdog.RollbackStateFile,
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
		return false
	}
	if w.cfg.Watchdog.MaxRollbackAttempts > 0 && attempts >= w.cfg.Watchdog.MaxRollbackAttempts {
		w.log.Error(
			"Rollback attempt limit reached; manual intervention required",
			"deploy_ts", deployTS,
			"attempts", attempts,
			"max_attempts", w.cfg.Watchdog.MaxRollbackAttempts,
			"err", "rollback attempt budget exhausted",
		)
		w.log.Info("--- DIAGNOSIS END (rollback exhausted) ---")
		return false
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
		return false
	}

	w.log.Info(
		"--- DIAGNOSIS END: triggering rollback ---",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"deploy_ts", deployTS,
	)
	rbCtx := context.Background()
	w.rollback(rbCtx, deployTS, snap)
	return true
}

func (w *watchdog) handleTimeoutExceeded(ctx context.Context) {
	w.log.Info("--- DIAGNOSIS START ---")

	w.log.Info("Step 1: checking for recent config change...")
	deployTS, recent := w.checkDeploy(ctx)

	if !recent {
		if w.diagnoseNoRecentChange(ctx) {
			w.log.Info("--- DIAGNOSIS END (failover triggered) ---")
		}
		sleepOrDone(ctx, 60*time.Second)
		return
	}

	if w.cfg.Watchdog.DeployGracePeriodSeconds > 0 {
		rawDeployTS, dOK := w.readGuestUnix(
			ctx, w.cfg.Network.LastDeployPath,
		)
		if dOK {
			deployAge := time.Now().Unix() - rawDeployTS
			grace := int64(w.cfg.Watchdog.DeployGracePeriodSeconds)
			if deployAge >= 0 && deployAge < grace {
				remaining := grace - deployAge
				w.log.Info(
					"Within deploy grace period; waiting for "+
						"VM to stabilize",
					"deploy_ts", rawDeployTS,
					"deploy_age_seconds", deployAge,
					"remaining_seconds", remaining,
					"grace_period_seconds",
					w.cfg.Watchdog.DeployGracePeriodSeconds,
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

	if w.attemptRollbackForDeploy(ctx, deployTS) {
		w.log.Info(
			"Waiting for VM to boot and routes to converge after rollback",
			"grace", w.cfg.Watchdog.PostRollbackGraceSeconds,
		)
		sleepOrDone(ctx, w.cfg.Watchdog.PostRollbackGrace())
	} else {
		sleepOrDone(ctx, 60*time.Second)
	}
}
