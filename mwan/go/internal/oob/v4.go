//go:build linux

package oob

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"goodkind.io/mwan/internal/netif"
)

// V4Manager owns the IPv4 side of mbrains: the DHCP-learned address bound
// to the interface, and the DHCP-learned default route placed in the `oob`
// routing table (so it does not compete with the main-table default).
//
// The DHCP client (DHCPClient) runs in its own goroutine and emits LeaseInfo
// events. V4Manager.HandleLease translates those events to kernel mutations
// via the IPRunner.
type V4Manager struct {
	cfg V4Config
	log *slog.Logger

	mu          sync.Mutex
	currentCIDR string    // last-applied address (e.g. "158.247.70.13/26")
	currentGW   string    // last-applied default gateway in oob table
	lastBound   time.Time // last time State==BOUND was observed
}

// V4Config configures a V4Manager.
type V4Config struct {
	Iface      string // mbrains
	OOBTable   string // table name (kept for log readability; no longer authoritative)
	OOBTableID int    // numeric routing table ID (e.g. 500); used by netlink path
}

// NewV4Manager constructs (does not start) a V4Manager.
func NewV4Manager(log *slog.Logger, cfg V4Config) *V4Manager {
	return &V4Manager{
		cfg: cfg,
		log: log.With("component", "v4", "iface", cfg.Iface),
	}
}

// HandleLease applies the kernel mutations implied by a LeaseInfo event:
//   - On BOUND: ensure the IP is on iface; ensure the OOB table default
//     route points to the lease gateway.
//   - On EXPIRED: remove the IP and clear the OOB v4 default. (Daemon
//     attempts re-acquire on its own; this method just reflects state.)
//   - Other states are debug-only.
func (m *V4Manager) HandleLease(ctx context.Context, lease netif.LeaseInfo) error {
	log := m.log.With("op", "lease-event", "state", lease.State.String())
	log.Debug("v4: lease event", "info", lease.String())

	switch lease.State {
	case netif.LeaseBound:
		return m.applyBound(ctx, log, lease)
	case netif.LeaseExpired:
		return m.applyExpired(ctx, log)
	}
	return nil
}

func (m *V4Manager) applyBound(
	ctx context.Context, log *slog.Logger, lease netif.LeaseInfo,
) error {
	if lease.IP == nil {
		return fmt.Errorf("lease BOUND without IP")
	}
	prefix := lease.PrefixLen
	if prefix <= 0 || prefix > 32 {
		log.Warn("v4: lease has unusable subnet mask, defaulting to /32",
			"prefix_len", lease.PrefixLen)
		prefix = 32
	}
	cidr := fmt.Sprintf("%s/%d", lease.IP.String(), prefix)

	// Address: replace ensures we end up with exactly this address.
	if err := netif.ReconcileAddrs(ctx, log, m.cfg.Iface, []netif.AddrSpec{
		{CIDR: cidr, Family: "inet"},
	}); err != nil {
		return fmt.Errorf("apply lease addr %s: %w", cidr, err)
	}

	// OOB table default. nil gateway clears.
	want := netif.RouteSpec{
		Family:  "inet",
		Dest:    "default",
		Dev:     m.cfg.Iface,
		TableID: m.cfg.OOBTableID,
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
		log.Info("v4: lease IP changed",
			"old", m.currentCIDR, "new", cidr)
	}
	if m.currentGW != "" && m.currentGW != want.Via {
		log.Info("v4: lease gateway changed",
			"old", m.currentGW, "new", want.Via)
	}
	m.currentCIDR = cidr
	m.currentGW = want.Via
	m.lastBound = time.Now()
	return nil
}

func (m *V4Manager) applyExpired(
	ctx context.Context, log *slog.Logger,
) error {
	log.Warn("v4: lease expired; clearing oob default v4")
	clear := netif.RouteSpec{
		Family: "inet", Dest: "default",
		Dev: m.cfg.Iface, TableID: m.cfg.OOBTableID,
		// Via empty -> ReconcileTableDefault will delete the existing entry.
	}
	if err := netif.ReconcileTableDefault(ctx, log, clear); err != nil {
		return fmt.Errorf("clear oob default v4: %w", err)
	}
	// Address removal is intentionally NOT done; the kernel keeps the lease
	// IP until DHCP grants a new one. Removing here would also kill any
	// long-lived TCP sessions anchored to it. The next BOUND event will
	// `replace` the address atomically.
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentGW = ""
	return nil
}

// LastBound returns the time of the most recent BOUND event, or zero if
// the daemon has never observed one.
func (m *V4Manager) LastBound() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastBound
}
