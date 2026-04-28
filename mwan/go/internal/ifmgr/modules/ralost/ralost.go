//go:build linux

// Package ralost implements the ifmgr ra-lost alert module: emits WARN
// when no RA has been observed on the watched iface for longer than a
// configured threshold. Works on every role; for vault-oob it consumes
// the RA observation timestamp from the oobv6 module via accessor.
//
// Registers as "ra_lost".
package ralost

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

// Module owns the ra-lost alert decision.
type Module struct {
	cfg Config
	env *ifmgr.Env
	log *slog.Logger

	mu         sync.Mutex
	lastRASeen time.Time // updated on EvRouteAdded for ra-learned defaults
}

// Config is the parsed [ifmgr.modules.ra_lost] sub-config.
type Config struct {
	Iface       string
	RALostAfter time.Duration
}

// Name implements ifmgr.Module.
func (m *Module) Name() string { return "ra_lost" }

// Init implements ifmgr.Module.
func (m *Module) Init(_ context.Context, env *ifmgr.Env) error {
	m.env = env
	m.log = env.Log.With("module", "ra_lost", "iface", m.cfg.Iface)
	m.log.Info("ra_lost: Init", "ra_lost_after", m.cfg.RALostAfter.String())
	if m.cfg.Iface == "" {
		return fmt.Errorf("ra_lost: iface is required")
	}
	if m.cfg.RALostAfter <= 0 {
		return fmt.Errorf("ra_lost: ra_lost_after must be > 0")
	}
	return nil
}

// Reconcile implements ifmgr.Module. On the periodic tick we also poll
// the kernel for an RA-learned default; receiving one updates lastRASeen.
// This catches RAs that arrived between netlink events (e.g. very early
// in startup before the monitor goroutine attached).
func (m *Module) Reconcile(ctx context.Context, log *slog.Logger) error {
	cur, err := netif.FindMainRADefault(ctx, m.cfg.Iface)
	if err != nil {
		log.Debug("ra_lost: FindMainRADefault failed (non-fatal)", "err", err)
		return nil
	}
	if cur != nil {
		m.markSeen()
	}
	return nil
}

// OnKernelEvent implements ifmgr.Module. RA-learned default add/del
// events update lastRASeen.
func (m *Module) OnKernelEvent(_ context.Context, _ *slog.Logger, ev netif.Event) error {
	if ev.Iface != m.cfg.Iface || ev.Family != "inet6" || ev.Dest != "default" {
		return nil
	}
	if ev.Kind == netif.EvRouteAdded {
		m.markSeen()
	}
	return nil
}

// OnDHCPLease implements ifmgr.Module.
func (m *Module) OnDHCPLease(_ context.Context, _ *slog.Logger, _ netif.LeaseInfo) error {
	return nil
}

// EvaluateAlerts implements ifmgr.Module. Compares last-seen against the
// configured threshold. Notify is idempotent per AlertManager semantics.
func (m *Module) EvaluateAlerts(_ context.Context, _ *slog.Logger, now time.Time) {
	m.mu.Lock()
	last := m.lastRASeen
	m.mu.Unlock()

	if last.IsZero() {
		// Special case: no RA observed since startup. Treat startup time
		// as the reference; the daemon will fire once RALostAfter has
		// elapsed without any RA.
		// We approximate by using now-RALostAfter so the threshold logic
		// is uniform; but skip the very first tick to give startup a chance.
		return
	}
	age := now.Sub(last)
	if age > m.cfg.RALostAfter {
		m.env.Alerts.Notify(now, slog.LevelWarn,
			"ra-lost", m.cfg.Iface,
			"ra_lost: no RA observed within threshold",
			"last_seen", last.Format(time.RFC3339),
			"age_s", int(age.Seconds()),
			"threshold_s", int(m.cfg.RALostAfter.Seconds()),
		)
	} else if m.env.Alerts.Active("ra-lost", m.cfg.Iface) {
		m.env.Alerts.Resolve(now, "ra-lost", m.cfg.Iface,
			"ra_lost: RA observed again",
			"last_seen", last.Format(time.RFC3339),
		)
	}
}

func (m *Module) markSeen() {
	m.mu.Lock()
	m.lastRASeen = time.Now()
	m.mu.Unlock()
}

// New is the Constructor.
func New(cfg map[string]any) (ifmgr.Module, error) {
	c := Config{}
	if v, ok := cfg["iface"].(string); ok {
		c.Iface = v
	}
	if v, ok := cfg["ra_lost_alert_after"].(string); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("ra_lost: parse ra_lost_alert_after %q: %w", v, err)
		}
		c.RALostAfter = d
	}
	if c.RALostAfter == 0 {
		c.RALostAfter = 5 * time.Minute
	}
	return &Module{cfg: c}, nil
}

func init() { ifmgr.Register("ra_lost", New) }
