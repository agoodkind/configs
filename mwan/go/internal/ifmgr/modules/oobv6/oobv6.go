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

	// installedSLAACAddr is the source address currently installed in the
	// SLAAC source-based ip rule (bare addr, no prefix). Empty means no
	// rule installed by this module. Tracked so we can clean up across
	// renumbers and on shutdown.
	installedSLAACAddr string
}

// Config is the parsed [ifmgr.modules.oobv6] sub-config.
type Config struct {
	Iface      string // mbrains
	OOBAddr    string // "3d06:bad:b01:ff::1/128"
	OOBTableID int    // numeric routing table ID (e.g. 500)

	// ManageSLAACRule, when true (default), keeps an `ip -6 rule from
	// <current-MB-SLAAC> lookup oob priority N` entry in sync with the
	// live MB SLAAC address on the iface. Without this rule, replies
	// sourced from the MB SLAAC fall through to the main table and
	// egress via the lower-metric default (e.g. vmbr0/OPNsense),
	// breaking off-site reachability to vault's public MB v6.
	ManageSLAACRule bool

	// SLAACRulePriority is the rule priority used for the SLAAC
	// source-based rule. Default 7 (one lower than rule 6 which pins
	// 3d06:bad:b01:ff::1 -> oob, so they don't collide).
	SLAACRulePriority int
}

func (Config) ModuleConfigName() string { return "oobv6" }

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

	if err := m.syncOOBDefault(ctx, log, cur); err != nil {
		return err
	}

	// Keep the source-based rule for the live MB SLAAC in sync. This is
	// what makes off-site v6 reach back to vault from any address mbrains
	// hands us, without depending on the address staying the same across
	// RA renumbers (MWAN-67).
	return m.reconcileSLAACSrcRule(ctx, log)
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
		// Re-run the SLAAC source-rule reconcile so the rule tracks the
		// new address within one event tick instead of one reconcile cycle.
		if err := m.reconcileSLAACSrcRule(ctx, log); err != nil {
			return fmt.Errorf("reconcile slaac src rule on AddrAdded: %w", err)
		}
	case netif.EvAddrDeleted:
		if ev.Family != "inet6" {
			return nil
		}
		if strings.HasPrefix(strings.ToLower(ev.CIDR), "fe80") || ev.CIDR == m.cfg.OOBAddr {
			return nil
		}
		// A non-OOB SLAAC address went away. The rule may now point at a
		// stale source; let reconcile remove it (or replace if another
		// SLAAC is still present).
		log.Debug("oobv6: SLAAC addr deleted, re-evaluating src rule",
			"addr", ev.CIDR)
		if err := m.reconcileSLAACSrcRule(ctx, log); err != nil {
			return fmt.Errorf("reconcile slaac src rule on AddrDeleted: %w", err)
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

// reconcileSLAACSrcRule installs an ip rule that points the live MB SLAAC
// source at the OOB routing table. Removes the rule when no SLAAC is
// present. Idempotent. No-op when ManageSLAACRule is false.
//
// State machine across reconciles:
//   - SLAAC absent, no rule installed     -> no-op
//   - SLAAC absent, rule installed        -> remove rule
//   - SLAAC present, none installed       -> install rule
//   - SLAAC present, same address rule    -> no-op
//   - SLAAC present, different address    -> remove old, install new
func (m *Module) reconcileSLAACSrcRule(ctx context.Context, log *slog.Logger) error {
	if !m.cfg.ManageSLAACRule {
		log.Debug("oobv6: SLAAC source-rule management disabled")
		return nil
	}

	log = log.With("op", "reconcile-slaac-src-rule")

	current, err := m.findCurrentGlobalSLAAC(ctx, log)
	if err != nil {
		return fmt.Errorf("find current SLAAC: %w", err)
	}

	m.mu.Lock()
	installed := m.installedSLAACAddr
	m.mu.Unlock()

	log.Debug("oobv6: SLAAC src rule state",
		"installed", installed, "current", current,
		"priority", m.cfg.SLAACRulePriority, "table_id", m.cfg.OOBTableID)

	if current == installed {
		// No-op for both "both empty" and "match" cases.
		return nil
	}

	// Need to change the rule. Remove any existing rule at our priority
	// first (covers stale-installed cleanup and the renumber case), then
	// install the new one if we have a current SLAAC.
	if installed != "" || current != "" {
		log.Info("oobv6: SLAAC source-rule update",
			"old", installed, "new", current,
			"priority", m.cfg.SLAACRulePriority)
	}

	if installed != "" {
		if err := netif.RemoveRuleAtPriority(
			ctx, log, "inet6", m.cfg.SLAACRulePriority,
		); err != nil {
			return fmt.Errorf("remove stale SLAAC src rule: %w", err)
		}
	}

	if current != "" {
		desired := []netif.DesiredRule{{
			Family:   "inet6",
			Priority: m.cfg.SLAACRulePriority,
			From:     current,
			TableID:  m.cfg.OOBTableID,
		}}
		if err := netif.ReconcileRules(ctx, log, desired); err != nil {
			return fmt.Errorf("install SLAAC src rule: %w", err)
		}
	}

	m.mu.Lock()
	m.installedSLAACAddr = current
	m.mu.Unlock()
	return nil
}

// findCurrentGlobalSLAAC returns the bare address (no /prefix) of the
// SLAAC-derived global IPv6 address on m.cfg.Iface. Returns "" if none.
//
// Filter chain (each must pass for an address to be considered):
//
//   - family is inet6
//   - not link-local (fe80::/10)
//   - not the static OOB address we manage ourselves
//   - IFA_F_PERMANENT NOT set: SLAAC autoconf addresses do not have this
//     flag, while manually-added addresses (`ip addr add`) do. This is the
//     defensive piece that distinguishes SLAAC from any extra manual
//     addrs the operator may have on the iface (e.g. service VIPs).
//   - IFA_F_TENTATIVE NOT set: the kernel is still doing DAD; address is
//     not yet usable as a source. Skip and let the next reconcile retry.
//
// In practice MB hands us exactly one SLAAC GUA at a time, so the first
// match wins. If multiple SLAAC addresses appeared we still pick the
// first one netlink returns (kernel ordering is stable across reconciles
// so this is deterministic).
func (m *Module) findCurrentGlobalSLAAC(
	ctx context.Context, log *slog.Logger,
) (string, error) {
	addrs, err := netif.ListAddrs(ctx, log, m.cfg.Iface)
	if err != nil {
		return "", err
	}
	oobBare := netif.StripPrefix(m.cfg.OOBAddr)
	for _, a := range addrs {
		if a.Family != "inet6" {
			continue
		}
		bare := netif.StripPrefix(a.CIDR)
		lower := strings.ToLower(bare)
		if strings.HasPrefix(lower, "fe80") || strings.HasPrefix(lower, "fe8") ||
			strings.HasPrefix(lower, "fe9") || strings.HasPrefix(lower, "fea") ||
			strings.HasPrefix(lower, "feb") {
			// link-local fe80::/10
			continue
		}
		if bare == oobBare {
			continue
		}
		if a.Flags&netif.IFAFPermanent != 0 {
			// Manually-added address (not SLAAC autoconf). Skip.
			continue
		}
		if a.Flags&netif.IFAFTentative != 0 {
			// Still doing DAD; not yet usable as source.
			continue
		}
		return bare, nil
	}
	return "", nil
}

// New is the Constructor registered with ifmgr. Defaults:
// ManageSLAACRule=true, SLAACRulePriority=7. Pass
// `manage_slaac_source_rule = false` in [ifmgr.modules.oobv6] to
// disable the rule entirely.
func New(cfg ifmgr.ModuleConfig) (ifmgr.Module, error) {
	c := Config{
		ManageSLAACRule:   true,
		SLAACRulePriority: 7,
	}
	if cfg != nil {
		typedConfig, ok := cfg.(Config)
		if !ok {
			return nil, fmt.Errorf("oobv6: invalid config type %T", cfg)
		}
		c = typedConfig
	}
	if c.SLAACRulePriority <= 0 || c.SLAACRulePriority >= 32766 {
		return nil, fmt.Errorf("oobv6: slaac_rule_priority out of range (1..32765): %d", c.SLAACRulePriority)
	}
	return &Module{cfg: c}, nil
}

func init() { ifmgr.Register("oobv6", New) }
