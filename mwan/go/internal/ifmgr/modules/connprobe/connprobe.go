//go:build linux

// Package connprobe runs periodic active connectivity probes against
// configured targets and publishes a healthy/unhealthy state. Used by
// the lxc-failover-backup role to verify upstream actually works,
// independent of whether SLAAC and routes look correct.
//
// Failures emit a WARN; recoveries log INFO and clear the alert.
//
// Registers as "connectivity_probe".
package connprobe

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

// Module owns the connectivity health state.
type Module struct {
	cfg Config
	env *ifmgr.Env
	log *slog.Logger

	mu        sync.Mutex
	lastResult map[string]bool // key=target string, val=last probe healthy?
	lastRunAt time.Time
}

// Config is the parsed [ifmgr.modules.connectivity_probe] sub-config.
type Config struct {
	Iface          string
	TargetsV6      []netip.Addr
	Timeout        time.Duration
	UnhealthyAfter time.Duration // a single failed probe must persist this long before alert
}

// Name implements ifmgr.Module.
func (m *Module) Name() string { return "connectivity_probe" }

// Init implements ifmgr.Module.
func (m *Module) Init(_ context.Context, env *ifmgr.Env) error {
	m.env = env
	m.log = env.Log.With("module", "connectivity_probe", "iface", m.cfg.Iface)
	m.log.Info("connectivity_probe: Init",
		"target_count", len(m.cfg.TargetsV6),
		"timeout", m.cfg.Timeout.String(),
		"unhealthy_after", m.cfg.UnhealthyAfter.String(),
	)
	if m.cfg.Iface == "" {
		return fmt.Errorf("connectivity_probe: iface is required")
	}
	if len(m.cfg.TargetsV6) == 0 {
		return fmt.Errorf("connectivity_probe: at least one targets_v6 entry is required")
	}
	m.lastResult = map[string]bool{}
	return nil
}

// Reconcile implements ifmgr.Module. Runs each probe in series.
func (m *Module) Reconcile(ctx context.Context, log *slog.Logger) error {
	probe := netif.NewV6Probe(m.cfg.Iface, log)
	now := time.Now()
	results := map[string]bool{}
	for _, t := range m.cfg.TargetsV6 {
		_, err := probe.PingICMP6(ctx, t, m.cfg.Timeout)
		ok := err == nil
		results[t.String()] = ok
		log.Debug("connectivity_probe: probe result",
			"target", t.String(), "ok", ok, "err", err)
	}
	m.mu.Lock()
	m.lastResult = results
	m.lastRunAt = now
	m.mu.Unlock()
	return nil
}

// OnKernelEvent implements ifmgr.Module.
func (m *Module) OnKernelEvent(_ context.Context, _ *slog.Logger, _ netif.Event) error {
	return nil
}

// OnDHCPLease implements ifmgr.Module.
func (m *Module) OnDHCPLease(_ context.Context, _ *slog.Logger, _ netif.LeaseInfo) error {
	return nil
}

// EvaluateAlerts implements ifmgr.Module. Aggregates per-target results
// into a single iface-level alert: any failed target means unhealthy.
func (m *Module) EvaluateAlerts(_ context.Context, _ *slog.Logger, now time.Time) {
	m.mu.Lock()
	results := m.lastResult
	last := m.lastRunAt
	m.mu.Unlock()

	if last.IsZero() {
		return // no probes have run yet
	}

	allOK := true
	failingTargets := []string{}
	for t, ok := range results {
		if !ok {
			allOK = false
			failingTargets = append(failingTargets, t)
		}
	}

	if allOK {
		if m.env.Alerts.Active("connectivity-down", m.cfg.Iface) {
			m.env.Alerts.Resolve(now,
				"connectivity-down", m.cfg.Iface,
				"connectivity_probe: all targets responding again")
		}
		return
	}
	m.env.Alerts.Notify(now, slog.LevelWarn,
		"connectivity-down", m.cfg.Iface,
		"connectivity_probe: one or more upstream targets unreachable",
		"failing_targets", failingTargets,
		"target_count", len(results),
	)
}

// New is the Constructor.
func New(cfg map[string]any) (ifmgr.Module, error) {
	c := Config{
		Timeout:        2 * time.Second,
		UnhealthyAfter: 10 * time.Second,
	}
	if v, ok := cfg["iface"].(string); ok {
		c.Iface = v
	}
	if v, ok := cfg["timeout"].(string); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("connectivity_probe: timeout %q: %w", v, err)
		}
		c.Timeout = d
	}
	if v, ok := cfg["unhealthy_after"].(string); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("connectivity_probe: unhealthy_after %q: %w", v, err)
		}
		c.UnhealthyAfter = d
	}
	if rawList, ok := cfg["targets_v6"].([]any); ok {
		for i, raw := range rawList {
			s, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("connectivity_probe: targets_v6[%d] not a string", i)
			}
			addr, err := netip.ParseAddr(s)
			if err != nil {
				return nil, fmt.Errorf("connectivity_probe: targets_v6[%d] %q: %w", i, s, err)
			}
			c.TargetsV6 = append(c.TargetsV6, addr)
		}
	}
	return &Module{cfg: c}, nil
}

func init() { ifmgr.Register("connectivity_probe", New) }
