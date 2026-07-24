//go:build linux

package health

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"sync"
	"time"

	internalclock "goodkind.io/mwan/internal/clock"
	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

const (
	moduleName            = "health"
	alertKindWANUnhealthy = "wan-health"
)

type pingFunc func(
	context.Context,
	string,
	netip.Addr,
	time.Duration,
) (time.Duration, error)

type httpFunc func(context.Context, string, string, time.Duration) (int, error)

// State stays wire-compatible with the shell state file while preventing
// arbitrary strings from entering the module's hysteresis state machine.
type State string

const (
	// StateUnknown preserves the shell warmup state until a threshold is met.
	StateUnknown State = "unknown"
	// StateHealthy records that consecutive dual-family cycles met recovery.
	StateHealthy State = "healthy"
	// StateUnhealthy records that consecutive dual-family cycles met failure.
	StateUnhealthy State = "unhealthy"
)

// WAN embeds the shared identity so every WAN-role module uses the same name
// and source-bound interface.
type WAN struct {
	ifmgr.WANRef
}

// Config keeps probe policy module-wide while the shared WAN section owns WAN
// identity and interface selection.
type Config struct {
	ShadowMode        bool
	StateFile         string
	PersistStateFile  string
	TargetsV4         []netip.Addr
	TargetsV6         []netip.Addr
	HTTPURLs          []string
	Timeout           time.Duration
	Interval          time.Duration
	PingCount         int
	SuccessThreshold  int
	FailureThreshold  int
	RecoveryThreshold int
	WANs              []WAN
}

// ModuleConfigName returns the registry key used by the WAN role.
func (Config) ModuleConfigName() string { return moduleName }

type wanStatus struct {
	State     State
	OKCount   int
	FailCount int
}

type probeResult struct {
	Passed        bool
	V6Successes   int
	V4Successes   int
	HTTPSuccesses int
}

type transition struct {
	WAN  WAN
	From State
	To   State
}

// Module serializes probe cycles so the interval loop and reconcile-triggered
// cycle cannot advance hysteresis from overlapping observations.
type Module struct {
	ifmgr.BaseModule

	cfg Config

	clock            internalclock.Clock
	cycleMu          sync.Mutex
	reconcileMu      sync.Mutex
	reconcilePending bool
	statuses         map[string]wanStatus

	probeV4   pingFunc
	probeV6   pingFunc
	probeHTTP httpFunc
}

// Init implements ifmgr.Module and binds the steady-state loop to daemon
// cancellation so no probe worker outlives the role instance.
func (m *Module) Init(ctx context.Context, env *ifmgr.Env) error {
	log := m.InitBase(env, "module", moduleName)
	log.InfoContext(
		ctx,
		"health: Init",
		"wan_count", len(m.cfg.WANs),
		"shadow_mode", m.cfg.ShadowMode,
		"interval", m.cfg.Interval.String(),
	)
	if len(m.cfg.WANs) == 0 {
		log.WarnContext(ctx, "health: no WAN config; disabling module")
		return fmt.Errorf("%w: health: no [ifmgr.wan] WANs", ifmgr.ErrModuleDisabled)
	}
	if err := validateConfig(m.cfg); err != nil {
		log.WarnContext(ctx, "health: invalid config", "err", err)
		return fmt.Errorf("health: invalid config: %w", err)
	}
	if m.clock == nil {
		m.clock = internalclock.Real{}
	}
	if m.probeV4 == nil {
		m.probeV4 = netif.Ping4
	}
	if m.probeV6 == nil {
		m.probeV6 = netif.Ping6
	}
	if m.probeHTTP == nil {
		m.probeHTTP = netif.HTTPCheck
	}
	if err := m.loadStatuses(ctx, log); err != nil {
		return err
	}
	// Seed the state files. writeStateFiles already tolerates a persist-mirror
	// failure (best-effort restart recovery under the read-only /var/lib
	// sandbox), so any error here is a runtime-file write failure. The runtime
	// file is the required output, so fail Init rather than run blind on a
	// genuinely broken /var/run.
	if err := m.writeStateFiles(ctx, log, m.snapshotStatuses()); err != nil {
		log.WarnContext(ctx, "health: initialize runtime state failed", "err", err)
		return fmt.Errorf("health: initialize state files: %w", err)
	}
	// The per-cycle recover in runCycleGuarded keeps a panicking cycle from
	// killing the loop; this outer recover is a required last-resort backstop for
	// a panic in the loop's own control code (it should effectively never fire).
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(
					ctx,
					"health: probe loop panicked",
					"err", fmt.Sprint(recovered),
				)
			}
		}()
		m.probeLoop(ctx, log)
	}()
	return nil
}

// Reconcile runs an immediate cycle so later WAN-role modules read fresh state
// during the daemon's ordered startup reconcile.
func (m *Module) Reconcile(ctx context.Context, log *slog.Logger) error {
	m.reconcileMu.Lock()
	if !m.reconcilePending {
		m.reconcileMu.Unlock()
		return nil
	}
	m.reconcilePending = false
	m.reconcileMu.Unlock()

	if err := m.runCycle(ctx, log); err != nil {
		m.reconcileMu.Lock()
		m.reconcilePending = true
		m.reconcileMu.Unlock()
		return err
	}
	return nil
}

// EvaluateAlerts implements ifmgr.Module; transitions emit synchronously from
// the probe cycle so the ten-second driver does not wait for daemon reconcile.
func (m *Module) EvaluateAlerts(_ context.Context, _ *slog.Logger, _ time.Time) {}

func (m *Module) probeLoop(ctx context.Context, log *slog.Logger) {
	// Use a timer reset after each cycle rather than a ticker so the full
	// interval always elapses between the end of one cycle and the start of the
	// next. A ticker queues a tick during a long cycle and fires it immediately,
	// collapsing the shell's post-cycle delay and probing back to back.
	timer := time.NewTimer(m.cfg.Interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.runCycleGuarded(ctx, log)
			timer.Reset(m.cfg.Interval)
		}
	}
}

// runCycleGuarded runs one probe cycle and recovers from a panic so a single
// bad cycle (for example inside a netif primitive) logs and the interval loop
// keeps publishing state, instead of the goroutine dying for the rest of the
// process lifetime. health is the sole source of WAN state for later WAN-role
// modules, so a permanently dead loop is a quiet, serious degradation.
func (m *Module) runCycleGuarded(ctx context.Context, log *slog.Logger) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.ErrorContext(
				ctx,
				"health: probe cycle panicked",
				"err", fmt.Sprint(recovered),
			)
		}
	}()
	if err := m.runCycle(ctx, log); err != nil {
		log.WarnContext(ctx, "health: interval probe cycle failed", "err", err)
	}
}

func (m *Module) runCycle(ctx context.Context, log *slog.Logger) error {
	m.cycleMu.Lock()
	defer m.cycleMu.Unlock()

	nextStatuses := m.snapshotStatuses()
	transitions := make([]transition, 0, len(m.cfg.WANs))
	for _, wan := range m.cfg.WANs {
		result := m.probeWAN(ctx, wan, log)
		current := nextStatuses[wan.Name]
		next, changed := advanceHealth(
			current,
			result.Passed,
			m.cfg.FailureThreshold,
			m.cfg.RecoveryThreshold,
		)
		nextStatuses[wan.Name] = next
		if changed {
			transitions = append(transitions, transition{
				WAN: wan, From: current.State, To: next.State,
			})
		}
		log.DebugContext(
			ctx,
			"health: probe result",
			"wan", wan.Name,
			"iface", wan.Iface,
			"passed", result.Passed,
			"v6_successes", result.V6Successes,
			"v4_successes", result.V4Successes,
			"http_successes", result.HTTPSuccesses,
			"state", next.State,
			"ok_count", next.OKCount,
			"fail_count", next.FailCount,
		)
	}
	if err := m.writeStateFiles(ctx, log, nextStatuses); err != nil {
		log.WarnContext(ctx, "health: write state files failed", "err", err)
		return fmt.Errorf("health: write state files: %w", err)
	}
	m.Lock()
	m.statuses = nextStatuses
	m.Unlock()
	m.emitTransitions(ctx, log, transitions)
	return nil
}

func (m *Module) probeWAN(ctx context.Context, wan WAN, log *slog.Logger) probeResult {
	v6Successes := m.probeTargets(ctx, wan, m.cfg.TargetsV6, m.probeV6)
	v4Successes := m.probeTargets(ctx, wan, m.cfg.TargetsV4, m.probeV4)
	httpSuccesses := 0
	for _, url := range m.cfg.HTTPURLs {
		statusCode, err := m.probeHTTP(ctx, wan.Iface, url, m.cfg.Timeout)
		reachable := err == nil
		if reachable {
			httpSuccesses++
		}
		log.DebugContext(
			ctx,
			"health: HTTP probe result",
			"wan", wan.Name,
			"iface", wan.Iface,
			"url", url,
			"status_code", statusCode,
			"reachable", reachable,
			"err", err,
		)
	}
	// Verdict matches health-check.sh check_wan_health: the WAN is healthy when
	// IPv6 meets the threshold (preferred, the P0 signal) OR IPv4 meets it
	// (fallback), or any HTTP probe succeeds. Both families are always probed
	// (v6 primary, v4 always); v6 leads, but v4 or HTTP alone can still keep a
	// WAN up, so a v4 flap never marks a healthy-v6 WAN down.
	passed := v6Successes >= m.cfg.SuccessThreshold ||
		v4Successes >= m.cfg.SuccessThreshold ||
		httpSuccesses >= 1
	return probeResult{
		Passed:        passed,
		V6Successes:   v6Successes,
		V4Successes:   v4Successes,
		HTTPSuccesses: httpSuccesses,
	}
}

func (m *Module) probeTargets(
	ctx context.Context,
	wan WAN,
	targets []netip.Addr,
	probe pingFunc,
) int {
	successes := 0
	for _, target := range targets {
		targetReached := false
		for range m.cfg.PingCount {
			if _, err := probe(ctx, wan.Iface, target, m.cfg.Timeout); err == nil {
				targetReached = true
			}
		}
		if targetReached {
			successes++
		}
	}
	return successes
}

func (m *Module) snapshotStatuses() map[string]wanStatus {
	m.Lock()
	defer m.Unlock()

	statuses := make(map[string]wanStatus, len(m.statuses))
	maps.Copy(statuses, m.statuses)
	return statuses
}

func advanceHealth(
	current wanStatus,
	cyclePassed bool,
	failureThreshold int,
	recoveryThreshold int,
) (wanStatus, bool) {
	next := current
	if cyclePassed {
		next.OKCount++
		next.FailCount = 0
		if next.OKCount >= recoveryThreshold {
			next.State = StateHealthy
		}
	} else {
		next.FailCount++
		next.OKCount = 0
		if next.FailCount >= failureThreshold {
			next.State = StateUnhealthy
		}
	}
	return next, next.State != current.State
}

func (m *Module) emitTransitions(
	ctx context.Context,
	log *slog.Logger,
	transitions []transition,
) {
	for _, event := range transitions {
		m.emitTransition(ctx, log, event)
	}
}

func (m *Module) emitTransition(
	ctx context.Context,
	log *slog.Logger,
	event transition,
) {
	log.InfoContext(
		ctx,
		"health: WAN state transition",
		"wan", event.WAN.Name,
		"iface", event.WAN.Iface,
		"from", event.From,
		"to", event.To,
		"shadow_mode", m.cfg.ShadowMode,
	)
	if m.Env == nil || m.Env.Alerts == nil {
		return
	}
	now := m.clock.Now()
	if event.To == StateUnhealthy {
		// Alert even from the unknown warmup state: a WAN that is broken from
		// startup must raise its first unhealthy alert, not stay silent until it
		// has been healthy once.
		m.Env.Alerts.NotifyContext(
			ctx,
			now,
			slog.LevelWarn,
			alertKindWANUnhealthy,
			event.WAN.Name,
			"health: WAN became unhealthy",
			slog.String("iface", event.WAN.Iface),
		)
		return
	}
	if event.To == StateHealthy {
		// An unknown-to-healthy transition has no prior alert to resolve, so skip
		// the no-op resolve rather than emitting a spurious recovery.
		if event.From == StateUnknown {
			return
		}
		m.Env.Alerts.ResolveContext(
			ctx,
			now,
			alertKindWANUnhealthy,
			event.WAN.Name,
			"health: WAN recovered",
			slog.String("iface", event.WAN.Iface),
		)
	}
}
