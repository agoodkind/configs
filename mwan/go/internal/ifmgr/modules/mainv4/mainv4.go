//go:build linux

// Package mainv4 applies DHCPv4 lease state to the watched interface and
// to the main routing table. This is the lxc-failover-backup analogue of
// oobv4 (which applies to a separate OOB table for vault).
//
// Use this when the daemon owns DHCPv4 for the iface and you want the
// lease to drive the iface's primary v4 configuration. Pair with
// ifmgr.iface.<name>.dhcp_v4 = true.
//
// Registers as "mainv4". Selected by the lxc-failover-backup role when
// dhcp_v4 is enabled.
package mainv4

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

// Module owns the main-table v4 state for one iface.
type Module struct {
	cfg Config
	env *ifmgr.Env
	log *slog.Logger

	mu          sync.Mutex
	currentCIDR string
	currentGW   string
	lastBound   time.Time
}

// Config is the parsed [ifmgr.modules.mainv4] sub-config.
type Config struct {
	Iface string
}

// Name implements ifmgr.Module.
func (m *Module) Name() string { return "mainv4" }

// Init implements ifmgr.Module. Inert when DHCP is disabled so that the
// shared lxc-failover-backup role still works on hosts that do not own
// DHCPv4 (prod LXC 116 today). A no-op Init lets the role include this
// module unconditionally without breaking those hosts.
func (m *Module) Init(_ context.Context, env *ifmgr.Env) error {
	m.env = env
	m.log = env.Log.With("module", "mainv4", "iface", m.cfg.Iface)
	if env.DHCP == nil {
		// Inert when DHCPv4 is disabled. iface is unused in this mode, so
		// don't require it; lets roles include the module unconditionally
		// without forcing every host's config to declare a placeholder iface.
		m.log.Info("mainv4: Init (inert: dhcp_v4 is disabled)")
		return nil
	}
	if m.cfg.Iface == "" {
		return fmt.Errorf("mainv4: iface is required when dhcp_v4 is enabled")
	}
	m.log.Info("mainv4: Init (active)")
	return nil
}

// Reconcile implements ifmgr.Module. v4 reacts to lease events.
func (m *Module) Reconcile(_ context.Context, _ *slog.Logger) error {
	return nil
}

// OnKernelEvent implements ifmgr.Module.
func (m *Module) OnKernelEvent(_ context.Context, _ *slog.Logger, _ netif.Event) error {
	return nil
}

// OnDHCPLease implements ifmgr.Module. Applies BOUND leases to the iface
// addr and the main-table default route. Logs every transition. When
// inert (no DHCP client wired) this is never invoked because no lease
// events are produced; this method is a no-op in that case anyway.
func (m *Module) OnDHCPLease(
	ctx context.Context, log *slog.Logger, lease netif.LeaseInfo,
) error {
	if m.env.DHCP == nil {
		return nil
	}
	log = log.With("op", "lease-event", "state", lease.State.String())
	log.Debug("mainv4: lease event", "info", lease.String())
	switch lease.State {
	case netif.LeaseBound:
		return m.applyBound(ctx, log, lease)
	case netif.LeaseExpired:
		return m.applyExpired(ctx, log)
	}
	return nil
}

func (m *Module) applyBound(
	ctx context.Context, log *slog.Logger, lease netif.LeaseInfo,
) error {
	if lease.IP == nil {
		return fmt.Errorf("mainv4: lease BOUND without IP")
	}
	prefix := lease.PrefixLen
	if prefix <= 0 || prefix > 32 {
		log.Warn("mainv4: lease has unusable subnet mask, defaulting to /32",
			"prefix_len", lease.PrefixLen)
		prefix = 32
	}
	cidr := fmt.Sprintf("%s/%d", lease.IP.String(), prefix)

	if err := netif.ReconcileAddrs(ctx, log, m.cfg.Iface, []netif.AddrSpec{
		{CIDR: cidr, Family: "inet"},
	}); err != nil {
		return fmt.Errorf("apply lease addr %s: %w", cidr, err)
	}

	want := netif.RouteSpec{
		Family:  "inet",
		Dest:    "default",
		Dev:     m.cfg.Iface,
		TableID: unix.RT_TABLE_MAIN,
	}
	if lease.Gateway != nil {
		want.Via = lease.Gateway.String()
	}
	if err := netif.ReconcileTableDefault(ctx, log, want); err != nil {
		return fmt.Errorf("apply lease default route: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.currentCIDR != "" && m.currentCIDR != cidr {
		log.Info("mainv4: lease IP changed", "old", m.currentCIDR, "new", cidr)
	}
	if m.currentGW != "" && m.currentGW != want.Via {
		log.Info("mainv4: lease gateway changed", "old", m.currentGW, "new", want.Via)
	}
	m.currentCIDR = cidr
	m.currentGW = want.Via
	m.lastBound = time.Now()
	return nil
}

func (m *Module) applyExpired(ctx context.Context, log *slog.Logger) error {
	log.Warn("mainv4: lease expired; clearing main-table default v4")
	clear := netif.RouteSpec{
		Family: "inet", Dest: "default",
		Dev: m.cfg.Iface, TableID: unix.RT_TABLE_MAIN,
	}
	if err := netif.ReconcileTableDefault(ctx, log, clear); err != nil {
		return fmt.Errorf("clear main-table default v4: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentGW = ""
	return nil
}

// EvaluateAlerts implements ifmgr.Module. Lease-loss alerting is owned
// by ralost; this module just exposes lastBound.
func (m *Module) EvaluateAlerts(_ context.Context, _ *slog.Logger, _ time.Time) {
}

// LastBound exposes the last BOUND timestamp for ralost.
func (m *Module) LastBound() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastBound
}

// New is the Constructor.
func New(cfg map[string]any) (ifmgr.Module, error) {
	c := Config{}
	if v, ok := cfg["iface"].(string); ok {
		c.Iface = v
	}
	return &Module{cfg: c}, nil
}

func init() { ifmgr.Register("mainv4", New) }
