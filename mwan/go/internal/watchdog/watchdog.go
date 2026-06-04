package watchdog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"goodkind.io/mwan/internal/alert"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/notify"
	"goodkind.io/mwan/internal/ops"
	"goodkind.io/mwan/internal/rollback"
	"goodkind.io/mwan/internal/tracing"
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
	notify  notify.Notifier
	coord   *alert.Coord
	limiter *alert.Limiter
	log     *slog.Logger
	runID   string

	// exitFn, if non-nil, replaces os.Exit when rollback defers shutdown after SIGTERM.
	exitFn func(code int)
	// testHeartbeatInterval overrides heartbeatInterval in run() when > 0 (tests only).
	testHeartbeatInterval time.Duration
	nowFn                 func() time.Time

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

	// failoverMu guards failoverActive, failoverStartedAt, and failoverReason.
	// These track BGP failover state so the recovery hook can fire exactly
	// once when the primary returns.
	failoverMu        sync.Mutex
	failoverActive    bool
	failoverStartedAt time.Time
	failoverReason    string
}

func (w *watchdog) heartbeatTick() time.Duration {
	if w.testHeartbeatInterval > 0 {
		return w.testHeartbeatInterval
	}
	return heartbeatInterval
}

func (w *watchdog) now() time.Time {
	if w.nowFn == nil {
		now := time.Now
		return now()
	}
	return w.nowFn()
}

func (w *watchdog) since(start time.Time) time.Duration {
	return w.now().Sub(start)
}

func (w *watchdog) appendProbe(msg string) {
	w.probeLog = append(w.probeLog, msg)
}

func (w *watchdog) flushProbeLog() string {
	s := strings.Join(w.probeLog, "\n")
	w.probeLog = w.probeLog[:0]
	return s
}

func (w *watchdog) tracedLogger(ctx context.Context) *slog.Logger {
	return tracing.Logger(ctx, w.log)
}

// notifierOrNull returns w.notify when configured, or a NullNotifier
// otherwise. Tests construct watchdog instances without wiring notify so
// the call sites stay safe without conditional guards.
func (w *watchdog) notifierOrNull() notify.Notifier {
	if w.notify == nil {
		return notify.NullNotifier{}
	}
	return w.notify
}

// routeChannelFallback bounds the per-cycle TCP-fallback warning to one
// email per state transition plus one per repeat-cadence window. The
// vsock-unavailable path used to fire WARN every 5-minute cycle on
// testbed because VM 950 has no vhost-vsock-pci device. When the vsock
// channel returns successfully, Resolve fires the recovery line.
func (w *watchdog) routeChannelFallback(ctx context.Context, usedChannel string) {
	notifier := w.notifierOrNull()
	key := w.cfg.MwanVMID + ":" + strconv.FormatUint(uint64(w.cfg.Watchdog.VsockPort), 10)
	if usedChannel == "tcp" {
		notifier.Notify(ctx, notify.Event{
			Now:     w.now(),
			Level:   slog.LevelWarn,
			Kind:    "vsock-fallback",
			Key:     key,
			Message: "getConfigState: vsock unavailable, used TCP fallback",
			Fields: []slog.Attr{
				slog.String("channel", usedChannel),
				slog.String("vm_id", w.cfg.MwanVMID),
			},
			IsRecovery: false,
		})
		return
	}
	if usedChannel == "vsock" {
		notifier.Resolve(ctx, "vsock-fallback", key,
			"getConfigState: vsock channel restored",
			slog.String("channel", usedChannel),
			slog.String("vm_id", w.cfg.MwanVMID),
		)
	}
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
	log := w.tracedLogger(ctx)
	parsed, err := w.ops.GuestExec(ctx, w.cfg.MwanVMID, args...)
	if err != nil {
		if errors.Is(err, ops.ErrGuestExecUnavailable) {
			log.WarnContext(ctx,
				"guestExec unavailable",
				"args", strings.Join(args, " "),
				"err", err,
			)
			return false, true
		}
		log.ErrorContext(ctx,
			"guestExec error",
			"args", strings.Join(args, " "),
			"err", err,
		)
		return false, false
	}
	if parsed.ExitCode != 0 {
		log.InfoContext(ctx,
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
	log := w.tracedLogger(ctx)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Add(2)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(ctx, "IPv4 probe panic", "err", fmt.Errorf("panic: %v", recovered))
			}
		}()
		defer wg.Done()
		ok := w.ops.Ping(ctx, "ping", w.cfg.Network.PingTargetIPv4)
		mu.Lock()
		v4ok = ok
		mu.Unlock()
	}()
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(ctx, "IPv6 probe panic", "err", fmt.Errorf("panic: %v", recovered))
			}
		}()
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
	log.InfoContext(ctx,
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
	log := w.tracedLogger(ctx)
	log.InfoContext(ctx,
		"Testing VM default-route connectivity",
		"vmid", w.cfg.MwanVMID,
		"ping6_target", w.cfg.Network.PingTargetIPv6,
		"ping_target", w.cfg.Network.PingTargetIPv4,
	)
	v6ok, v6Unavailable := w.guestExecProbe(
		ctx, "ping6", "-c", "2", "-W", "3", w.cfg.Network.PingTargetIPv6,
	)
	if v6ok {
		log.InfoContext(ctx,
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
		log.InfoContext(ctx,
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
		log.WarnContext(ctx,
			"VM default-route probes unavailable due to guest-exec transport",
			"vmid", w.cfg.MwanVMID,
		)
		w.appendProbe(fmt.Sprintf(
			"VM %s default-route probes unavailable (guest-exec transport)",
			w.cfg.MwanVMID,
		))
	}
	log.InfoContext(ctx,
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
	log := w.tracedLogger(ctx)
	ifaces := w.cfg.Network.WanIfaceNames()
	log.InfoContext(ctx,
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
			log.DebugContext(ctx,
				"ISP reachable from VM (IPv4 OK)",
				"interface", iface,
			)
			w.appendProbe(fmt.Sprintf("WAN %s: IPv4 OK", iface))
			return true
		}
		if v6ok {
			log.DebugContext(ctx,
				"ISP reachable from VM (IPv6 OK)",
				"interface", iface,
			)
			w.appendProbe(fmt.Sprintf("WAN %s: IPv6 OK", iface))
			return true
		}
		if v4Unavailable || v6Unavailable {
			log.DebugContext(ctx,
				"ISP probe via WAN interface unavailable due to guest-exec transport",
				"interface", iface,
			)
			w.appendProbe(fmt.Sprintf(
				"WAN %s: probe unavailable (guest-exec transport)",
				iface,
			))
			continue
		}
		log.DebugContext(ctx,
			"ISP unreachable from VM (IPv4 FAIL, IPv6 FAIL)",
			"interface", iface,
		)
		w.appendProbe(fmt.Sprintf("WAN %s: IPv4 FAIL, IPv6 FAIL", iface))
	}
	log.DebugContext(ctx, "ISP unreachable from VM on all tested WAN interfaces")
	return false
}

func (w *watchdog) findSnapshot(ctx context.Context) (string, error) {
	log := w.tracedLogger(ctx)
	log.InfoContext(ctx, "Listing snapshots for VM", "vmid", w.cfg.MwanVMID)
	out, err := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID)
	if err != nil {
		return "", err
	}
	snap := rollback.ExtractLatestSnapshot(out)
	if snap == "" {
		log.InfoContext(ctx,
			"No rollback snapshot (pre-deploy-* or known-good-*)",
			"listsnapshot_output", string(out),
		)
	} else {
		log.InfoContext(ctx, "Found rollback snapshot", "snapshot", snap)
	}
	return snap, nil
}

func (w *watchdog) readGuestUnix(ctx context.Context, path string) (int64, bool) {
	log := w.tracedLogger(ctx)
	parsed, err := w.ops.GuestExec(ctx, w.cfg.MwanVMID, "cat", path)
	if err != nil {
		if errors.Is(err, ops.ErrGuestExecUnavailable) {
			log.WarnContext(ctx,
				"PVE guest-exec unavailable; cannot read deploy timestamp; assuming no recent deploy",
				"vmid", w.cfg.MwanVMID,
			)
		} else {
			log.ErrorContext(ctx, "guestExec(cat) error", "path", path, "err", err)
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
		log.ErrorContext(ctx,
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
// silently skipped.
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
		return "  (no per-file diff available; composite hash changed)\n"
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
	log := w.tracedLogger(ctx)
	resp, usedChannel, err := w.ops.GetConfigState(ctx, w.cfg.MwanVMID)
	if err != nil {
		log.WarnContext(ctx, "checkConfigHash getConfigState", "err", err)
		w.lastHashCheckOK = false
		return
	}
	w.routeChannelFallback(ctx, usedChannel)
	h := strings.TrimSpace(resp.GetConfigHash())
	if h == "" {
		return
	}
	currentManifest := parseManifest(resp.GetConfigManifest())

	if w.lastConfigHash != "" && h != w.lastConfigHash {
		if !w.postRollbackGraceUntil.IsZero() &&
			w.now().Before(w.postRollbackGraceUntil) {
			log.InfoContext(ctx,
				"Post-rollback hash change suppressed",
				"old_hash", w.lastConfigHash,
				"new_hash", h,
				"grace_until", w.postRollbackGraceUntil,
			)
		} else {
			w.hashChangeWindowStart = w.now().Unix()
			diffSection := manifestDiff(w.lastManifest, currentManifest)
			log.WarnContext(ctx,
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
		log.DebugContext(ctx,
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
	log := w.tracedLogger(ctx)
	if w.cfg.Watchdog.SnapshotHealthyThreshold <= 0 {
		return
	}
	if w.consecutiveHealthy < w.cfg.Watchdog.SnapshotHealthyThreshold {
		return
	}
	windowSec := int64(w.cfg.Watchdog.DeployWindowMinutes) * 60
	if w.hashChangeWindowStart > 0 {
		elapsed := w.now().Unix() - w.hashChangeWindowStart
		if elapsed < windowSec {
			return
		}
	}
	deployTS, dOK := w.readGuestUnix(ctx, w.cfg.Network.LastDeployPath)
	if dOK && (w.now().Unix()-deployTS) < windowSec {
		return
	}
	minGap := time.Duration(w.cfg.Watchdog.MinSnapshotIntervalSeconds) * time.Second
	if !w.lastSnapshotAt.IsZero() && w.since(w.lastSnapshotAt) < minGap {
		return
	}
	if w.cfg.Watchdog.HashCheckEveryNHealthy > 0 && !w.lastHashCheckOK {
		return
	}
	name := "known-good-" + w.now().Format("20060102-150405")
	if err := w.ops.VMSnapshot(ctx, w.cfg.MwanVMID, name); err != nil {
		log.ErrorContext(ctx, "vmSnapshot failed", "err", err, "snapshot", name)
		return
	}
	log.InfoContext(ctx, "created known-good snapshot", "snapshot", name)
	w.lastSnapshotAt = w.now()
	w.consecutiveHealthy = 0
	if err := w.pruneSnapshots(ctx); err != nil {
		log.ErrorContext(ctx, "pruneSnapshots failed", "err", err)
	}
	log.InfoContext(ctx, "Interface monitor stopped (context cancelled)")
}

func (w *watchdog) pruneSnapshots(ctx context.Context) error {
	log := w.tracedLogger(ctx)
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
				log.ErrorContext(ctx,
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
			log.ErrorContext(ctx,
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
	log := w.tracedLogger(ctx)
	running, err := w.ops.VMStatus(ctx, w.cfg.MwanVMID)
	if err != nil {
		log.ErrorContext(ctx, "checkDeploy: vmStatus error", "err", err)
		return 0, false
	}
	if !running {
		log.InfoContext(ctx,
			"checkDeploy: VM is not running; cannot check change window",
			"vmid", w.cfg.MwanVMID,
		)
		return 0, false
	}

	log.InfoContext(ctx,
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
		log.InfoContext(ctx, "checkDeploy: no change markers or hash window")
		return 0, false
	}
	effective := candidates[0]
	for _, t := range candidates[1:] {
		if t > effective {
			effective = t
		}
	}

	ageMin := (w.now().Unix() - effective) / 60
	log.InfoContext(ctx,
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
		log.InfoContext(ctx,
			"checkDeploy: change window stale",
			"age_minutes", ageMin,
			"window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
		)
		return 0, false
	}

	log.InfoContext(ctx,
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
	log := w.tracedLogger(ctx)
	stopStart := w.now()
	log.InfoContext(ctx,
		"Stopping VM",
		"vmid", w.cfg.MwanVMID,
		"timeout", ops.TimeoutQmStop,
	)
	if err := w.ops.VMStop(ctx, w.cfg.MwanVMID); err != nil {
		log.ErrorContext(ctx,
			"vmStop error (continuing to rollback)",
			"vmid", w.cfg.MwanVMID,
			"err", err,
		)
	} else {
		log.DebugContext(ctx,
			"VM stopped",
			"vmid", w.cfg.MwanVMID,
			"elapsed", w.since(stopStart).Round(time.Millisecond),
		)
	}

	// Delete any watchdog-managed snapshots that are children of the target.
	// Proxmox/ZFS only allows rollback to the leaf snapshot in the chain.
	if listOut, lErr := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID); lErr == nil {
		toDelete := rollback.SnapshotsAfter(listOut, snap)
		for _, child := range slices.Backward(toDelete) {
			log.DebugContext(ctx, "Deleting intermediate snapshot before rollback",
				"snapshot", child, "target", snap)
			if dErr := w.ops.VMDelSnapshot(ctx, w.cfg.MwanVMID, child); dErr != nil {
				log.ErrorContext(ctx, "Failed to delete intermediate snapshot",
					"snapshot", child, "err", dErr)
			}
		}
	} else {
		log.WarnContext(ctx, "Could not list snapshots before rollback", "err", lErr)
	}

	var rollbackErr error
	rollbackStart := w.now()
	log.InfoContext(ctx,
		"Running qm rollback",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"timeout", ops.TimeoutQmRollback,
	)
	if err := w.ops.VMRollback(ctx, w.cfg.MwanVMID, snap); err != nil {
		rollbackErr = err
		log.ErrorContext(ctx,
			"qm rollback FAILED; attempting qm start anyway",
			"vmid", w.cfg.MwanVMID,
			"snapshot", snap,
			"elapsed", w.since(rollbackStart).Round(time.Millisecond),
			"err", err,
		)
	} else {
		log.InfoContext(ctx,
			"qm rollback completed",
			"elapsed", w.since(rollbackStart).Round(time.Millisecond),
		)
	}

	startTime := w.now()
	log.InfoContext(ctx,
		"Starting VM",
		"vmid", w.cfg.MwanVMID,
		"timeout", ops.TimeoutQmStart,
	)
	if err := w.ops.VMStart(ctx, w.cfg.MwanVMID); err != nil {
		log.ErrorContext(ctx,
			"qm start FAILED; VM may remain stopped",
			"vmid", w.cfg.MwanVMID,
			"elapsed", w.since(startTime).Round(time.Millisecond),
			"err", err,
		)
	} else {
		log.InfoContext(ctx,
			"VM started",
			"vmid", w.cfg.MwanVMID,
			"elapsed", w.since(startTime).Round(time.Millisecond),
		)
	}
	return rollbackErr
}

// recordRollbackResult persists rollback state, removes the lock file, and
// logs the outcome. Called after executeRollbackVM completes.
func (w *watchdog) recordRollbackResult(
	ctx context.Context,
	deployTS int64,
	snap string,
	rollbackErr error,
) {
	log := w.tracedLogger(ctx)
	if err := os.Remove(w.cfg.Watchdog.RollbackLockFile); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		log.ErrorContext(ctx, "remove rollback lock", "err", err)
	} else {
		log.InfoContext(ctx, "Removed rollback lock file")
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
		rollbackAttempts, rollbackSucceeded, w.now(),
	); writeErr != nil {
		log.ErrorContext(ctx, "write rollback state", "err", writeErr)
	} else {
		log.InfoContext(ctx,
			"Wrote rollback state",
			"path", w.cfg.Watchdog.RollbackStateFile,
			"deploy_ts", deployTS,
			"snapshot", snap,
			"success", rollbackSucceeded,
			"attempts", rollbackAttempts,
		)
	}

	log.WarnContext(ctx,
		"auto-rollback completed",
		"vm_id", w.cfg.MwanVMID,
		"snapshot", snap,
		"node", w.cfg.PVE.Node,
	)
}

func (w *watchdog) rollback(ctx context.Context, deployTS int64, snap string) {
	log := w.tracedLogger(ctx)
	w.hashChangeWindowStart = 0
	w.coord.SetRollingBack(true)
	w.totalFailStart = time.Time{}
	w.consecutiveHealthy = 0
	defer w.coord.SetRollingBack(false)

	_ = w.flushProbeLog()

	lockContent := fmt.Sprintf(
		"deploy_ts=%d snapshot=%s ts=%d\n",
		deployTS, snap, w.now().Unix(),
	)
	if err := os.WriteFile(
		w.cfg.Watchdog.RollbackLockFile, []byte(lockContent), 0o644,
	); err != nil {
		log.ErrorContext(ctx, "write rollback lock", "err", err)
	} else {
		log.InfoContext(ctx, "Wrote rollback lock", "path", w.cfg.Watchdog.RollbackLockFile)
	}

	log.InfoContext(ctx,
		"INITIATING ROLLBACK",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"deploy_ts", deployTS,
		"deploy_age_seconds", w.now().Unix()-deployTS,
	)

	rollbackErr := w.executeRollbackVM(ctx, snap)
	w.recordRollbackResult(ctx, deployTS, snap, rollbackErr)

	log.InfoContext(ctx,
		"ROLLBACK COMPLETE; waiting for routes to converge",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"grace", w.cfg.Watchdog.PostRollbackGraceSeconds,
	)
	w.postRollbackGraceUntil = w.now().Add(
		time.Duration(w.cfg.Watchdog.DeployWindowMinutes) * time.Minute,
	)
	if w.coord.TakeShutdownAfterRollback() {
		log.InfoContext(ctx, "Deferred shutdown now executing after rollback")
		if w.exitFn != nil {
			w.exitFn(0)
		}
	}
}

func (w *watchdog) recoverInterrupted(ctx context.Context) {
	log := w.tracedLogger(ctx)
	data, err := os.ReadFile(w.cfg.Watchdog.RollbackLockFile)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.ErrorContext(ctx, "read rollback lock", "err", err)
		return
	}
	log.InfoContext(ctx,
		"Found rollback lock from previous instance",
		"lock_content", strings.TrimSpace(string(data)),
	)
	running, statusErr := w.ops.VMStatus(ctx, w.cfg.MwanVMID)
	if statusErr != nil {
		log.ErrorContext(ctx, "qm status during recovery", "err", statusErr)
		return
	}
	if running {
		log.InfoContext(ctx,
			"VM is running; previous rollback completed. Removing lock.",
			"vmid", w.cfg.MwanVMID,
		)
		if err := os.Remove(w.cfg.Watchdog.RollbackLockFile); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			log.ErrorContext(ctx, "remove stale rollback lock", "err", err)
		}
		return
	}
	log.InfoContext(ctx,
		"VM is STOPPED and rollback lock exists; attempting to start VM to complete interrupted rollback",
		"vmid", w.cfg.MwanVMID,
	)
	if startErr := w.ops.VMStart(ctx, w.cfg.MwanVMID); startErr != nil {
		log.ErrorContext(ctx,
			"VM start after interrupted rollback FAILED; manual intervention needed",
			"vmid", w.cfg.MwanVMID,
			"err", startErr,
		)
	} else {
		log.InfoContext(ctx,
			"VM started successfully after interrupted rollback",
			"vmid", w.cfg.MwanVMID,
		)
	}
	if err := os.Remove(w.cfg.Watchdog.RollbackLockFile); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		log.ErrorContext(ctx, "remove rollback lock after recovery", "err", err)
	}
	w.recoveredFromRollback = true
	log.InfoContext(ctx,
		"Waiting for VM to boot and routes to converge after interrupted rollback recovery",
		"grace", w.cfg.Watchdog.PostRollbackGraceSeconds,
	)
	if !sleepOrDone(ctx, time.Duration(w.cfg.Watchdog.PostRollbackGraceSeconds)*time.Second) {
		log.InfoContext(ctx, "Context cancelled during interrupted rollback recovery")
	}
}

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

// handleTotalLoss processes a probe where both protocols are down.
// It tracks downtime and returns true when the connectivity timeout has been
// exceeded (meaning the caller should invoke handleTimeoutExceeded).
func (w *watchdog) handleTotalLoss(ctx context.Context) bool {
	log := w.tracedLogger(ctx)
	w.consecutiveTotalFails++
	now := w.now().Unix()
	if w.totalDownStartUnix == 0 {
		w.totalDownStartUnix = now
		log.InfoContext(ctx,
			"TOTAL connectivity loss (IPv4 and IPv6 both FAILED); starting timeout",
			"timeout_seconds", w.cfg.Watchdog.ConnectivityTimeoutSeconds,
			"fail_count", w.consecutiveTotalFails,
		)
	}
	w.lastState = stateDown
	downDuration := int(now - w.totalDownStartUnix)
	remaining := w.cfg.Watchdog.ConnectivityTimeoutSeconds - downDuration
	if downDuration < w.cfg.Watchdog.ConnectivityTimeoutSeconds {
		log.InfoContext(ctx,
			"Still down before timeout threshold",
			"elapsed_seconds", downDuration,
			"remaining_seconds", remaining,
			"fail_count", w.consecutiveTotalFails,
		)
		return false
	}
	log.InfoContext(ctx,
		"Timeout exceeded; entering diagnosis",
		"down_seconds", downDuration,
		"threshold_seconds", w.cfg.Watchdog.ConnectivityTimeoutSeconds,
	)
	return true
}

// sleepOrShutdown sleeps for the given duration, returning false if the
// context was cancelled (indicating the caller should return from run).
func (w *watchdog) sleepOrShutdown(ctx context.Context, d time.Duration) bool {
	log := w.tracedLogger(ctx)
	if !sleepOrDone(ctx, d) {
		log.InfoContext(ctx, "Context cancelled during sleep; watchdog shutting down")
		return false
	}
	return true
}

// runIteration executes one iteration of the watchdog loop.
// Returns false if the loop should exit (context cancelled).
func (w *watchdog) runIteration(ctx context.Context, iteration int) bool {
	iterCtx := tracing.WithOperation(ctx, "watchdog_iteration")
	iterCtx = tracing.WithAttempt(iterCtx, iteration)
	iterCtx, _ = tracing.StartTrace(iterCtx, "", "watchdog_iteration")
	log := w.tracedLogger(iterCtx)
	select {
	case <-ctx.Done():
		log.InfoContext(ctx, "Context cancelled; watchdog shutting down")
		return false
	default:
	}

	running, err := w.ops.VMStatus(iterCtx, w.cfg.MwanVMID)
	if err != nil {
		log.ErrorContext(ctx, "qm status error", "vmid", w.cfg.MwanVMID, "err", err)
		return w.sleepOrShutdown(iterCtx, w.cfg.Watchdog.DegradedInterval())
	}
	if !running {
		w.handleVMStopped(iterCtx)
		return w.sleepOrShutdown(iterCtx, w.cfg.Watchdog.DegradedInterval())
	}
	if w.vmStoppedLogged {
		log.DebugContext(ctx, "VM is running again", "vmid", w.cfg.MwanVMID)
	}
	w.vmStoppedLogged = false

	v4ok, v6ok := w.probeConnectivity(iterCtx)
	if !v4ok || !v6ok {
		w.consecutiveHealthy = 0
		w.healthyCyclesForHash = 0
	}

	switch {
	case v4ok && v6ok:
		w.handleHealthyProbe(iterCtx, iteration)
		w.maybeTriggerRecovery(iterCtx, w.cfg)
		return w.sleepOrShutdown(iterCtx, w.cfg.Watchdog.HealthyInterval())
	case v4ok || v6ok:
		w.handlePartialProbe(iterCtx, v6ok)
		return w.sleepOrShutdown(iterCtx, w.cfg.Watchdog.DegradedInterval())
	default:
		if w.handleTotalLoss(iterCtx) {
			w.handleTimeoutExceeded(iterCtx)
		}
		return w.sleepOrShutdown(iterCtx, w.cfg.Watchdog.DegradedInterval())
	}
}

func (w *watchdog) run(ctx context.Context) {
	log := w.tracedLogger(ctx)
	w.logStartupConfig(ctx)
	w.runStartupChecks(ctx)
	w.lastState = stateUnknown
	w.lastHeartbeat = w.now()
	iteration := 0

	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(ctx, "interface monitor panic", "err", fmt.Errorf("panic: %v", recovered))
			}
		}()
		w.runIfaceMonitor(ctx)
	}()

	for w.cfg.Watchdog.MaxIterations <= 0 ||
		iteration < w.cfg.Watchdog.MaxIterations {
		iteration++
		if !w.runIteration(ctx, iteration) {
			return
		}
	}
	log.InfoContext(ctx, "Reached max iterations; exiting", "max", w.cfg.Watchdog.MaxIterations)
}

// runIfaceMonitor polls the vault host's interface addresses every 30 seconds.
// When any address is added or removed on any interface, it logs the change and
// sends an alert email so network changes are never silent.
func (w *watchdog) runIfaceMonitor(ctx context.Context) {
	log := w.tracedLogger(ctx)
	const pollInterval = 30 * time.Second
	prev := collectIfaceAddrs()
	log.InfoContext(ctx,
		"Interface monitor started",
		"poll_interval", pollInterval,
		"interface_count", len(prev),
	)
	for {
		select {
		case <-ctx.Done():
			break
		case <-time.After(pollInterval):
		}
		if ctx.Err() != nil {
			break
		}
		curr := collectIfaceAddrs()
		diff := diffIfaceAddrs(prev, curr)
		if diff == "" {
			prev = curr
			continue
		}
		log.WarnContext(ctx,
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
	log := w.tracedLogger(ctx)
	log.InfoContext(ctx,
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
		if w.cfg.Failover.LXCID != "" {
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
	log.InfoContext(ctx,
		"--- DIAGNOSIS END (no recent config change) ---",
		"reason", reason,
	)
	return false
}

// attemptRollbackForDeploy checks rollback eligibility, finds a snapshot, and
// triggers a rollback if appropriate. Returns true if a rollback was executed.
func (w *watchdog) attemptRollbackForDeploy(ctx context.Context, deployTS int64) bool {
	log := w.tracedLogger(ctx)
	log.InfoContext(ctx,
		"Step 2: checking rollback state",
		"deploy_ts", deployTS,
	)
	already, attempts, err := rollback.AlreadyDone(
		w.cfg.Watchdog.RollbackStateFile, deployTS,
	)
	if err != nil {
		log.ErrorContext(ctx,
			"read rollback state file (proceeding cautiously)",
			"path", w.cfg.Watchdog.RollbackStateFile,
			"err", err,
		)
	}
	if already {
		log.InfoContext(ctx,
			"Rollback already performed for this deploy_ts; "+
				"not rolling back again",
			"deploy_ts", deployTS,
		)
		log.InfoContext(ctx, "--- DIAGNOSIS END (rollback already done) ---")
		return false
	}
	if w.cfg.Watchdog.MaxRollbackAttempts > 0 && attempts >= w.cfg.Watchdog.MaxRollbackAttempts {
		log.ErrorContext(ctx,
			"Rollback attempt limit reached; manual intervention required",
			"deploy_ts", deployTS,
			"attempts", attempts,
			"max_attempts", w.cfg.Watchdog.MaxRollbackAttempts,
			"err", "rollback attempt budget exhausted",
		)
		log.InfoContext(ctx, "--- DIAGNOSIS END (rollback exhausted) ---")
		return false
	}

	log.InfoContext(ctx, "Step 3: finding rollback snapshot...")
	snap, snapErr := w.findSnapshot(ctx)
	if snapErr != nil {
		log.ErrorContext(ctx, "listsnapshot error", "err", snapErr)
	}
	if snap == "" {
		log.InfoContext(ctx, "No rollback snapshot found; cannot rollback")
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
		log.InfoContext(ctx, "--- DIAGNOSIS END (no snapshot) ---")
		return false
	}

	log.InfoContext(ctx,
		"--- DIAGNOSIS END: triggering rollback ---",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"deploy_ts", deployTS,
	)
	rbCtx := tracing.WithRunID(context.Background(), w.runID)
	rbCtx = tracing.WithTraceID(rbCtx, tracing.TraceID(ctx))
	rbCtx = tracing.WithOperation(rbCtx, "rollback")
	rbCtx, _ = tracing.StartTrace(rbCtx, "", "rollback")
	w.rollback(rbCtx, deployTS, snap)
	return true
}

func (w *watchdog) handleTimeoutExceeded(ctx context.Context) {
	diagCtx := tracing.WithOperation(ctx, "diagnose_connectivity")
	diagCtx, _ = tracing.StartTrace(diagCtx, "", "diagnose_connectivity")
	log := w.tracedLogger(diagCtx)
	log.InfoContext(ctx, "--- DIAGNOSIS START ---")

	log.InfoContext(ctx, "Step 1: checking for recent config change...")
	deployTS, recent := w.checkDeploy(diagCtx)

	if !recent {
		if w.diagnoseNoRecentChange(diagCtx) {
			log.InfoContext(ctx, "--- DIAGNOSIS END (failover triggered) ---")
		}
		sleepOrDone(diagCtx, 60*time.Second)
		return
	}

	if w.cfg.Watchdog.DeployGracePeriodSeconds > 0 {
		rawDeployTS, dOK := w.readGuestUnix(
			diagCtx, w.cfg.Network.LastDeployPath,
		)
		if dOK {
			deployAge := w.now().Unix() - rawDeployTS
			grace := int64(w.cfg.Watchdog.DeployGracePeriodSeconds)
			if deployAge >= 0 && deployAge < grace {
				remaining := grace - deployAge
				log.InfoContext(ctx,
					"Within deploy grace period; waiting for "+
						"VM to stabilize",
					"deploy_ts", rawDeployTS,
					"deploy_age_seconds", deployAge,
					"remaining_seconds", remaining,
					"grace_period_seconds",
					w.cfg.Watchdog.DeployGracePeriodSeconds,
				)
				log.InfoContext(ctx,
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

	log.InfoContext(ctx,
		"Config recently changed and connectivity still down",
		"deploy_ts", deployTS,
	)

	if w.attemptRollbackForDeploy(diagCtx, deployTS) {
		log.InfoContext(ctx,
			"Waiting for VM to boot and routes to converge after rollback",
			"grace", w.cfg.Watchdog.PostRollbackGraceSeconds,
		)
		sleepOrDone(diagCtx, w.cfg.Watchdog.PostRollbackGrace())
	} else {
		sleepOrDone(diagCtx, 60*time.Second)
	}
}
