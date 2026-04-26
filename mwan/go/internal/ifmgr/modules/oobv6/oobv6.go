//go:build linux

// Package oobv6 implements the vault-oob IPv6 module: it ensures the
// static OOB v6 address is present on the watched iface, mirrors the
// RA-learned default from the main table into the OOB routing table,
// and feeds RA observation timestamps to the AlertManager.
//
// Registers itself as "oobv6" at init time. Selected by the vault-oob
// role in internal/ifmgr/roles.go.
package oobv6

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

// Module owns the OOB v6 state for one iface. One Module per Daemon.
type Module struct {
	cfg Config
	env *ifmgr.Env
	log *slog.Logger

	mu          sync.Mutex
	lastRAGW    string    // last RA-learned default gateway in main table
	lastRASeen  time.Time // last successful observation of RA default
	lastSLAACPx string    // last observed non-OOB global SLAAC prefix
}

// Config is the parsed [ifmgr.modules.oobv6] sub-config.
type Config struct {
	Iface      string // mbrains
	OOBAddr    string // "3d06:bad:b01:ff::1/128"
	OOBTableID int    // numeric routing table ID (e.g. 500)
}

// Name implements ifmgr.Module.
func (m *Module) Name() string { return "oobv6" }

// Init implements ifmgr.Module. Captures env and validates config.
func (m *Module) Init(_ context.Context, env *ifmgr.Env) error {
	m.env = env
	m.log = env.Log.With("module", "oobv6", "iface", m.cfg.Iface)
	m.log.Info("oobv6: Init", "oob_addr", m.cfg.OOBAddr, "oob_table_id", m.cfg.OOBTableID)
	if m.cfg.Iface == "" {
		return fmt.Errorf("oobv6: iface is required")
	}
	if m.cfg.OOBAddr == "" {
		return fmt.Errorf("oobv6: oob_addr is required")
	}
	if m.cfg.OOBTableID <= 0 {
		return fmt.Errorf("oobv6: oob_table_id must be > 0")
	}
	return nil
}

// Reconcile implements ifmgr.Module. Idempotent.
func (m *Module) Reconcile(ctx context.Context, log *slog.Logger) error {
	log = log.With("op", "reconcile")
	log.Debug("oobv6: Reconcile entry")

	// Ensure the static OOB v6 address is present.
	if err := netif.ReconcileAddrs(ctx, log, m.cfg.Iface, []netif.AddrSpec{
		{CIDR: m.cfg.OOBAddr, Family: "inet6"},
	}); err != nil {
		return fmt.Errorf("reconcile OOB v6 addr: %w", err)
	}

	// Find the RA-learned default in the main table.
	cur, err := netif.FindMainRADefault(ctx, m.cfg.Iface)
	if err != nil {
		return fmt.Errorf("find main RA default: %w", err)
	}

	// If absent and we have an RA client, send a Router Solicitation to nudge.
	if cur == nil && m.env.RA != nil {
		log.Debug("oobv6: no RA default in main, sending Router Solicitation")
		ra, sErr := m.env.RA.SolicitRA(ctx, 5*time.Second)
		log.Debug("oobv6: RS result", "got_ra", ra != nil, "err", sErr)
		// Re-check after solicit; brief delay to let kernel install RA.
		time.Sleep(500 * time.Millisecond)
		cur, err = netif.FindMainRADefault(ctx, m.cfg.Iface)
		if err != nil {
			return fmt.Errorf("find main RA default after solicit: %w", err)
		}
	}

	return m.syncOOBDefault(ctx, log, cur)
}

// syncOOBDefault writes the desired default into the oob table. If cur is
// nil, the daemon clears the OOB default (no upstream visible).
func (m *Module) syncOOBDefault(
	ctx context.Context, log *slog.Logger, cur *netif.CurrentRoute,
) error {
	want := netif.RouteSpec{
		Family:  "inet6",
		Dest:    "default",
		Dev:     m.cfg.Iface,
		TableID: m.cfg.OOBTableID,
	}
	if cur != nil {
		want.Via = cur.Via
	}

	if err := netif.ReconcileTableDefault(ctx, log, want); err != nil {
		return fmt.Errorf("reconcile oob default: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if cur == nil {
		log.Debug("oobv6: no RA default; oob table cleared")
	} else {
		if m.lastRAGW != "" && m.lastRAGW != cur.Via {
			log.Info("oobv6: RA gateway changed", "old", m.lastRAGW, "new", cur.Via)
		}
		m.lastRAGW = cur.Via
		m.lastRASeen = time.Now()
	}
	return nil
}

// OnKernelEvent implements ifmgr.Module. Reacts to default route changes
// on the watched iface and to non-OOB SLAAC arrivals (renumber detect).
func (m *Module) OnKernelEvent(
	ctx context.Context, log *slog.Logger, ev netif.Event,
) error {
	if ev.Iface != m.cfg.Iface {
		return nil
	}
	switch ev.Kind {
	case netif.EvRouteAdded, netif.EvRouteDeleted:
		if ev.Family != "inet6" || ev.Dest != "default" {
			return nil
		}
		log = log.With("op", "route-event", "kind", ev.Kind.String(), "via", ev.Via)
		log.Debug("oobv6: route event for mbrains default")
		cur, err := netif.FindMainRADefault(ctx, m.cfg.Iface)
		if err != nil {
			return fmt.Errorf("find main RA default after event: %w", err)
		}
		return m.syncOOBDefault(ctx, log, cur)
	case netif.EvAddrAdded:
		if ev.Family != "inet6" {
			return nil
		}
		// Skip link-local and our static OOB.
		if strings.HasPrefix(strings.ToLower(ev.CIDR), "fe80") || ev.CIDR == m.cfg.OOBAddr {
			return nil
		}
		m.mu.Lock()
		old := m.lastSLAACPx
		m.lastSLAACPx = ev.CIDR
		m.mu.Unlock()
		if old != "" && old != ev.CIDR {
			log.Warn("oobv6: SLAAC renumber observed", "old", old, "new", ev.CIDR)
			m.env.Alerts.Notify(time.Now(), slog.LevelWarn,
				"slaac-renumber", m.cfg.Iface,
				"oobv6: SLAAC prefix changed",
				"old", old, "new", ev.CIDR,
			)
		}
	}
	return nil
}

// OnDHCPLease implements ifmgr.Module. v6 module ignores DHCPv4.
func (m *Module) OnDHCPLease(_ context.Context, _ *slog.Logger, _ netif.LeaseInfo) error {
	return nil
}

// EvaluateAlerts implements ifmgr.Module. Resolves the slaac-renumber
// alert when the SLAAC prefix has been stable for some time. ra-lost is
// owned by the ralost module (separate concern, role-specific threshold).
func (m *Module) EvaluateAlerts(_ context.Context, _ *slog.Logger, _ time.Time) {
	// No alerts owned here; ralost handles RA-loss.
}

// LastRASeen exposes the last RA observation timestamp for the ralost
// module to consume. Cross-module communication via accessor methods,
// not via shared mutable state.
func (m *Module) LastRASeen() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRASeen
}

// New is the Constructor registered with ifmgr. Parses cfg into Config.
func New(cfg map[string]any) (ifmgr.Module, error) {
	c := Config{}
	if v, ok := cfg["iface"].(string); ok {
		c.Iface = v
	}
	if v, ok := cfg["oob_addr"].(string); ok {
		c.OOBAddr = v
	}
	switch v := cfg["oob_table_id"].(type) {
	case int:
		c.OOBTableID = v
	case int64:
		c.OOBTableID = int(v)
	}
	return &Module{cfg: c}, nil
}

func init() { ifmgr.Register("oobv6", New) }
