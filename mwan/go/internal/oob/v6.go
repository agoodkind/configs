//go:build linux

package oob

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"goodkind.io/mwan/internal/netif"
)

const rdisc6Timeout = 10 * time.Second

// V6Manager owns the IPv6 side of mbrains: the static OOB address, RA
// solicitation, and synchronization of the RA-learned default into the
// `oob` routing table.
type V6Manager struct {
	cfg    V6Config
	runner netif.IPRunner
	log    *slog.Logger

	mu          sync.Mutex
	lastRAGW    string    // last RA-learned default gateway in main table
	lastRASeen  time.Time // last successful observation of RA default
	lastSLAACPx string    // last observed SLAAC prefix (for renumber detect)
}

// V6Config configures one V6Manager.
type V6Config struct {
	Iface     string // mbrains
	OOBAddr   string // "3d06:bad:b01:ff::1/128"
	OOBTable  string // table name (kept for log readability; no longer authoritative)
	OOBTableID int   // numeric routing table ID (e.g. 500); used by netlink path
}

// NewV6Manager constructs (does not start) a V6Manager.
func NewV6Manager(
	runner netif.IPRunner, log *slog.Logger, cfg V6Config,
) *V6Manager {
	return &V6Manager{
		cfg:    cfg,
		runner: runner,
		log:    log.With("component", "v6", "iface", cfg.Iface),
	}
}

// Reconcile ensures the static OOB address is present, that an RA has been
// solicited recently, and that the OOB table contains the RA-learned default.
// Idempotent. Safe to call from both the periodic ticker and event handlers.
func (m *V6Manager) Reconcile(ctx context.Context) error {
	log := m.log.With("op", "reconcile")

	// Ensure the static OOB v6 address is present on mbrains.
	if err := netif.ReconcileAddrs(ctx, m.runner, log, m.cfg.Iface, []netif.AddrSpec{
		{CIDR: m.cfg.OOBAddr, Family: "inet6"},
	}); err != nil {
		return fmt.Errorf("reconcile OOB v6 addr: %w", err)
	}

	// If we don't have an RA-learned default yet, nudge with rdisc6.
	cur, err := netif.FindMainRADefault(ctx, m.runner, m.cfg.Iface)
	if err != nil {
		return fmt.Errorf("find main RA default: %w", err)
	}
	if cur == nil {
		log.Debug("v6: no RA default in main, soliciting")
		if err := SolicitRA(ctx, log, m.cfg.Iface); err != nil {
			log.Warn("v6: rdisc6 solicit failed (non-fatal)", "err", err)
		}
		// Re-check after solicit; brief delay to let kernel install RA.
		time.Sleep(500 * time.Millisecond)
		cur, err = netif.FindMainRADefault(ctx, m.runner, m.cfg.Iface)
		if err != nil {
			return fmt.Errorf("find main RA default after solicit: %w", err)
		}
	}

	return m.syncOOBDefault(ctx, log, cur)
}

// HandleRouteEvent reacts to a route add/del event from the kernel monitor.
// If the event concerns the RA-learned default via mbrains, refresh the OOB
// table to match.
func (m *V6Manager) HandleRouteEvent(ctx context.Context, ev netif.Event) error {
	if ev.Family != "inet6" || ev.Iface != m.cfg.Iface {
		return nil
	}
	if ev.Dest != "default" {
		return nil
	}
	log := m.log.With("op", "route-event", "kind", ev.Kind.String(), "via", ev.Via)
	log.Debug("v6: route event for mbrains default")

	cur, err := netif.FindMainRADefault(ctx, m.runner, m.cfg.Iface)
	if err != nil {
		return fmt.Errorf("find main RA default after event: %w", err)
	}
	return m.syncOOBDefault(ctx, log, cur)
}

// syncOOBDefault writes the desired default into the oob table. If cur is
// nil, the daemon clears the OOB default (no upstream visible). Updates
// internal state used by the alerts subsystem.
func (m *V6Manager) syncOOBDefault(
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

	if err := netif.ReconcileTableDefault(ctx, m.runner, log, want); err != nil {
		return fmt.Errorf("reconcile oob default: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if cur == nil {
		log.Debug("v6: no RA default; oob table cleared")
		// keep lastRAGW to remember the last-known-good for alerts
	} else {
		if m.lastRAGW != "" && m.lastRAGW != cur.Via {
			log.Info("v6: RA gateway changed",
				"old", m.lastRAGW, "new", cur.Via)
		}
		m.lastRAGW = cur.Via
		m.lastRASeen = time.Now()
	}
	return nil
}

// HandleAddrEvent watches for SLAAC address arrivals on mbrains. Used by the
// alerts subsystem to detect MB renumbering us. Returns the new SLAAC prefix
// CIDR if we observed a renumber, or empty string otherwise.
func (m *V6Manager) HandleAddrEvent(ev netif.Event) (renumberedTo string) {
	if ev.Family != "inet6" || ev.Iface != m.cfg.Iface || ev.Kind != netif.EvAddrAdded {
		return ""
	}
	// Skip link-local addresses (always present, not interesting).
	if len(ev.CIDR) >= 4 && (ev.CIDR[:4] == "fe80" || ev.CIDR[:4] == "FE80") {
		return ""
	}
	// Skip our static OOB address.
	if ev.CIDR == m.cfg.OOBAddr {
		return ""
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastSLAACPx != "" && m.lastSLAACPx != ev.CIDR {
		m.log.Warn("v6: SLAAC renumber observed",
			"old", m.lastSLAACPx, "new", ev.CIDR)
		m.lastSLAACPx = ev.CIDR
		return ev.CIDR
	}
	m.lastSLAACPx = ev.CIDR
	return ""
}

// LastRASeen returns the time the daemon last observed an RA-learned default
// in the main table. Used by alerts to fire when MB upstream goes dark.
func (m *V6Manager) LastRASeen() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRASeen
}

// SolicitRA invokes rdisc6 to actively request a Router Advertisement on
// iface. Errors are non-fatal; the kernel will still receive unsolicited RAs
// on the next interval.
func SolicitRA(ctx context.Context, log *slog.Logger, iface string) error {
	cctx, cancel := context.WithTimeout(ctx, rdisc6Timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "rdisc6", "-1", iface)
	out, err := cmd.CombinedOutput()
	log.Debug("v6: rdisc6 result",
		"argv", []string{"rdisc6", "-1", iface},
		"err", err,
		"output", string(out),
	)
	if err != nil {
		return fmt.Errorf("rdisc6 %s: %w", iface, err)
	}
	return nil
}
