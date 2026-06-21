package watchdog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
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
		w.cfg.Watchdog.RollbackLockFile, []byte(lockContent), 0o600,
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
	rbCtx := tracing.WithRunID(context.WithoutCancel(ctx), w.runID)
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
