//go:build linux

// Package bridgeprobe implements an alert-only module for the
// failover role: when the watched iface has been silent
// (no RA observed AND no DHCP-server reply) for longer than a
// configured threshold AND the slaac_health module has already
// escalated without recovery, emit a WARN alert that the host-side
// veth on the iface's bridge is suspected dangling.
//
// The container cannot fix this from inside; alert-only.
//
// Registers as "bridge_probe".
package bridgeprobe

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	internalclock "goodkind.io/mwan/internal/clock"
	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

// Module owns the bridge-suspected alert decision.
type Module struct {
	cfg   Config
	env   *ifmgr.Env
	log   *slog.Logger
	clock internalclock.Clock

	mu         sync.Mutex
	lastRA     time.Time
	lastDHCP   time.Time
	lastLinkUp time.Time
}

// Config is the parsed [ifmgr.modules.bridge_probe] sub-config.
type Config struct {
	Iface              string
	NoSignalAlertAfter time.Duration
}

// ModuleConfigName returns the registry key for this module's config block.
func (Config) ModuleConfigName() string { return "bridge_probe" }

// Name implements ifmgr.Module.
func (m *Module) Name() string { return "bridge_probe" }

// Init implements ifmgr.Module.
func (m *Module) Init(ctx context.Context, env *ifmgr.Env) error {
	m.env = env
	m.log = env.Log.With("module", "bridge_probe", "iface", m.cfg.Iface)
	if m.clock == nil {
		m.clock = internalclock.Real{}
	}
	m.log.InfoContext(ctx, "bridge_probe: Init",
		"no_signal_alert_after", m.cfg.NoSignalAlertAfter.String())
	if m.cfg.Iface == "" {
		return fmt.Errorf("bridge_probe: iface is required")
	}
	return nil
}

// Reconcile implements ifmgr.Module. No state to push; this is a passive
// observer that fires alerts on the periodic tick (handled in EvaluateAlerts).
func (m *Module) Reconcile(_ context.Context, _ *slog.Logger) error {
	return nil
}

// OnKernelEvent implements ifmgr.Module. Tracks the last time a RA-default
// was observed and whether the link is up.
func (m *Module) OnKernelEvent(_ context.Context, _ *slog.Logger, ev netif.Event) error {
	if ev.Iface != m.cfg.Iface {
		return nil
	}
	now := m.clock.Now()
	switch ev.Kind {
	case netif.EvRouteAdded:
		if ev.Family == "inet6" && ev.Dest == "default" {
			m.mu.Lock()
			m.lastRA = now
			m.mu.Unlock()
		}
	case netif.EvLinkUp:
		m.mu.Lock()
		m.lastLinkUp = now
		m.mu.Unlock()
	case netif.EvUnknown, netif.EvRouteDeleted, netif.EvAddrAdded, netif.EvAddrDeleted, netif.EvLinkDown:
		return nil
	}
	return nil
}

// OnDHCPLease implements ifmgr.Module. Tracks DHCP server-reply timing.
func (m *Module) OnDHCPLease(_ context.Context, _ *slog.Logger, lease netif.LeaseInfo) error {
	if lease.State == netif.LeaseBound {
		m.mu.Lock()
		m.lastDHCP = m.clock.Now()
		m.mu.Unlock()
	}
	return nil
}

// EvaluateAlerts implements ifmgr.Module. Fires bridge-suspected when:
//   - both lastRA and lastDHCP are stale beyond NoSignalAlertAfter, AND
//   - link is observed up, AND
//   - the slaac_health alert is currently active (so we know slaac_health
//     has already exhausted its self-heal options).
func (m *Module) EvaluateAlerts(ctx context.Context, _ *slog.Logger, now time.Time) {
	m.mu.Lock()
	lastRA := m.lastRA
	lastDHCP := m.lastDHCP
	lastUp := m.lastLinkUp
	m.mu.Unlock()

	thresh := m.cfg.NoSignalAlertAfter
	raStale := !lastRA.IsZero() && now.Sub(lastRA) > thresh
	dhcpStale := !lastDHCP.IsZero() && now.Sub(lastDHCP) > thresh
	linkObservedUp := !lastUp.IsZero()
	slaacActive := m.env.Alerts.Active("slaac-degraded", m.cfg.Iface)

	suspected := raStale && dhcpStale && linkObservedUp && slaacActive

	if suspected {
		m.env.Alerts.NotifyContext(ctx, now, slog.LevelWarn,
			"bridge-suspected-dangling", m.cfg.Iface,
			"bridge_probe: bridge-side veth attachment suspected dangling "+
				"(no RA, no DHCP, link observed up, SLAAC self-heal exhausted)",
			"last_ra", lastRA.Format(time.RFC3339),
			"last_dhcp", lastDHCP.Format(time.RFC3339),
			"threshold_s", int(thresh.Seconds()),
			"hint", "host-side: verify veth is attached to expected bridge",
		)
	} else if m.env.Alerts.Active("bridge-suspected-dangling", m.cfg.Iface) {
		m.env.Alerts.ResolveContext(ctx, now,
			"bridge-suspected-dangling", m.cfg.Iface,
			"bridge_probe: signal returned")
	}
}

// New is the Constructor.
func New(cfg ifmgr.ModuleConfig) (ifmgr.Module, error) {
	c := Config{
		Iface:              "",
		NoSignalAlertAfter: 120 * time.Second,
	}
	if cfg != nil {
		typedConfig, ok := cfg.(Config)
		if !ok {
			return nil, fmt.Errorf("bridge_probe: invalid config type %T", cfg)
		}
		c = typedConfig
	}
	return &Module{
		cfg:        c,
		env:        nil,
		log:        nil,
		clock:      nil,
		mu:         sync.Mutex{},
		lastRA:     time.Time{},
		lastDHCP:   time.Time{},
		lastLinkUp: time.Time{},
	}, nil
}

func init() { ifmgr.Register("bridge_probe", New) }
