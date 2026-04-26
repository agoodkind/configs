//go:build linux

package netif

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	addrOpTimeout  = 5 * time.Second
	routeOpTimeout = 5 * time.Second
)

// AddrSpec is one IP address (CIDR notation) we want present on an iface.
// Family is derived from the CIDR ("inet" if it parses as IPv4, "inet6"
// otherwise; the caller can override).
type AddrSpec struct {
	CIDR   string
	Family string // "inet" or "inet6"; if empty, derived from CIDR
}

func (a AddrSpec) family() string {
	if a.Family != "" {
		return a.Family
	}
	if strings.Contains(a.CIDR, ":") {
		return "inet6"
	}
	return "inet"
}

// CurrentAddr is one address present on the interface, as observed via
// netlink AddrList. Mirrored from netlink.Addr but exposed as our own
// type so callers do not need to import vishvananda/netlink.
type CurrentAddr struct {
	CIDR   string
	Family string // "inet" or "inet6"
}

// ReconcileAddrs ensures every spec in desired is present on iface.
// Foreign addresses are NOT removed; the daemon only adds what it owns.
// (Removal of unmanaged addresses is out of scope; trust the operator.)
//
// Implementation uses vishvananda/netlink directly. The runner argument
// is retained for interface-level back-compat with callers that still
// pass it in step 3; it is not consulted for this operation.
func ReconcileAddrs(
	ctx context.Context, log *slog.Logger,
	iface string, desired []AddrSpec,
) error {
	log = log.With("component", "addrs", "iface", iface, "op", "reconcile")
	log.Debug("addrs: reconcile entry", "desired_count", len(desired))

	link, err := linkByName(log, iface)
	if err != nil {
		return err
	}

	current, err := listAddrsNetlink(log, link)
	if err != nil {
		return fmt.Errorf("list addrs: %w", err)
	}
	log.Debug("addrs: current snapshot",
		"count", len(current), "addrs", current)

	have := map[string]bool{}
	for _, c := range current {
		have[normalizeCIDR(c.CIDR)] = true
	}

	for _, w := range desired {
		key := normalizeCIDR(w.CIDR)
		if have[key] {
			log.Debug("addrs: already present", "addr", w.CIDR)
			continue
		}
		log.Info("addrs: adding", "addr", w.CIDR, "family", w.family())
		if err := addAddrNetlink(ctx, log, link, w); err != nil {
			return fmt.Errorf("add addr %s: %w", w.CIDR, err)
		}
	}
	log.Debug("addrs: reconcile complete", "applied", len(desired))
	return nil
}

// listAddrsNetlink returns parsed addresses on link, both families. Uses
// netlink.AddrList (RTM_GETADDR) directly. Logged at DEBUG with counts.
func listAddrsNetlink(log *slog.Logger, link netlink.Link) ([]CurrentAddr, error) {
	start := time.Now()
	addrs, err := netlink.AddrList(link, unix.AF_UNSPEC)
	dur := time.Since(start)
	log.Debug("addrs: AddrList",
		"link", link.Attrs().Name,
		"count", len(addrs),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		return nil, err
	}
	out := make([]CurrentAddr, 0, len(addrs))
	for _, a := range addrs {
		if a.IPNet == nil {
			continue
		}
		fam := "inet"
		if a.IP.To4() == nil {
			fam = "inet6"
		}
		out = append(out, CurrentAddr{CIDR: a.IPNet.String(), Family: fam})
	}
	return out, nil
}

// normalizeCIDR lower-cases the address half so comparisons are stable
// across IPv6 representations the kernel may return.
func normalizeCIDR(cidr string) string {
	return strings.ToLower(cidr)
}

// addAddrNetlink calls netlink.AddrReplace with a parsed CIDR. Returns nil
// if the address already exists at that prefix; a no-op AddrReplace returns
// nil from the kernel. Logged at DEBUG with parsed Addr struct.
func addAddrNetlink(
	ctx context.Context, log *slog.Logger, link netlink.Link, a AddrSpec,
) error {
	_ = ctx // netlink.AddrReplace is synchronous; ctx not consumable
	addr, err := netlink.ParseAddr(a.CIDR)
	if err != nil {
		return fmt.Errorf("parse %q: %w", a.CIDR, err)
	}
	start := time.Now()
	err = netlink.AddrReplace(link, addr)
	dur := time.Since(start)
	log.Debug("addrs: AddrReplace",
		"link", link.Attrs().Name,
		"cidr", addr.IPNet.String(),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	return err
}

// RouteSpec describes one route the daemon wants to keep present in a
// particular table. Currently only "default" is needed but the type is
// generic for future use.
type RouteSpec struct {
	Family  string // "inet" or "inet6"
	Dest    string // e.g. "default" or "::/0"
	Via     string // gateway address (link-local OK for inet6)
	Dev     string // outgoing interface
	TableID int    // routing table ID (e.g. 500 for "oob")
	Metric  int    // optional; 0 omits the metric
}

// CurrentRoute is one observed route entry. Mirrored from netlink.Route
// for callers who do not want to import vishvananda/netlink.
type CurrentRoute struct {
	Dest   string
	Via    string
	Dev    string
	Metric int
}

// ReconcileTableDefault ensures the table contains exactly the desired
// default route. If the current default's gateway differs, it's replaced.
// If no default exists, it's added. Other entries in the table are not
// touched. Pass want.Via == "" to clear the default route from the table.
func ReconcileTableDefault(
	ctx context.Context, log *slog.Logger,
	want RouteSpec,
) error {
	log = log.With("component", "route",
		"family", want.Family, "table_id", want.TableID, "op", "reconcile")
	log.Debug("route: reconcile entry", "want", want)

	cur, err := getTableDefaultNetlink(log, want.Family, want.TableID)
	if err != nil {
		return fmt.Errorf("read table default: %w", err)
	}
	log.Debug("route: current default in table",
		"current", cur, "want", want)

	if want.Via == "" {
		// Caller wants no default. Delete if present.
		if cur == nil {
			log.Debug("route: no default present and none wanted")
			return nil
		}
		log.Info("route: removing default from table",
			"old_via", cur.Via, "old_dev", cur.Dev)
		return delTableDefaultNetlink(ctx, log, want.Family, want.TableID)
	}

	if cur != nil && cur.Via == want.Via && cur.Dev == want.Dev {
		log.Debug("route: default already correct")
		return nil
	}

	log.Info("route: replacing default in table",
		"new_via", want.Via, "new_dev", want.Dev,
		"old", cur,
	)
	return replaceTableDefaultNetlink(ctx, log, want)
}

// getTableDefaultNetlink finds the default route in the named table for the
// given family. Returns (nil, nil) when no default route exists. Returns
// (nil, nil) for ENOENT-equivalent errors so callers can treat "table empty"
// as "no default present" (matches the prior shellout behavior).
func getTableDefaultNetlink(
	log *slog.Logger, family string, tableID int,
) (*CurrentRoute, error) {
	famConst := familyToNetlink(family)
	filter := &netlink.Route{Table: tableID}

	start := time.Now()
	routes, err := netlink.RouteListFiltered(famConst, filter, netlink.RT_FILTER_TABLE)
	dur := time.Since(start)
	log.Debug("route: RouteListFiltered (default lookup)",
		"family", family, "table_id", tableID,
		"count", len(routes),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		// Treat "table doesn't exist yet" as no-default.
		if errors.Is(err, syscall.ENOENT) {
			return nil, nil
		}
		return nil, err
	}

	for _, r := range routes {
		if !isDefaultRoute(r, famConst) {
			continue
		}
		cur, err := routeToCurrent(log, r)
		if err != nil {
			return nil, err
		}
		return cur, nil
	}
	return nil, nil
}

// isDefaultRoute reports whether r is the family's default route. For IPv4
// netlink represents default as Dst==nil with IPv4 prefix length 0. Same for
// v6. Some kernels report Dst as a 0.0.0.0/0 or ::/0 explicitly; both forms
// are accepted here.
func isDefaultRoute(r netlink.Route, family int) bool {
	if r.Dst == nil {
		return true
	}
	ones, _ := r.Dst.Mask.Size()
	if ones != 0 {
		return false
	}
	switch family {
	case unix.AF_INET:
		return r.Dst.IP.To4() != nil && r.Dst.IP.To4().Equal(net.IPv4zero)
	case unix.AF_INET6:
		return r.Dst.IP.To4() == nil && r.Dst.IP.Equal(net.IPv6unspecified)
	}
	return false
}

// routeToCurrent converts a netlink.Route to our CurrentRoute. The Via field
// becomes either the Gw IP or the multipath next-hop (we only consume single
// next-hop routes today; multipath returns the first one).
func routeToCurrent(log *slog.Logger, r netlink.Route) (*CurrentRoute, error) {
	cur := &CurrentRoute{
		Dest:   "default",
		Metric: r.Priority,
	}
	if r.Gw != nil {
		cur.Via = r.Gw.String()
	} else if len(r.MultiPath) > 0 && r.MultiPath[0].Gw != nil {
		cur.Via = r.MultiPath[0].Gw.String()
		log.Debug("route: multipath default observed; using first nexthop",
			"hop_count", len(r.MultiPath), "first_via", cur.Via)
	}
	if r.LinkIndex != 0 {
		link, err := netlink.LinkByIndex(r.LinkIndex)
		if err != nil {
			return nil, fmt.Errorf("LinkByIndex(%d): %w", r.LinkIndex, err)
		}
		cur.Dev = link.Attrs().Name
	}
	return cur, nil
}

// replaceTableDefaultNetlink installs (or replaces) a default route in the
// named table. Uses RouteReplace which is "add or update" in one syscall.
func replaceTableDefaultNetlink(
	ctx context.Context, log *slog.Logger, want RouteSpec,
) error {
	_ = ctx
	link, err := linkByName(log, want.Dev)
	if err != nil {
		return err
	}
	famConst := familyToNetlink(want.Family)

	gw := net.ParseIP(want.Via)
	if gw == nil {
		return fmt.Errorf("parse gateway %q: not a valid IP", want.Via)
	}

	r := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Table:     want.TableID,
		Gw:        gw,
		Family:    famConst,
		Priority:  want.Metric,
	}
	// Dst nil means "default" for the family.

	start := time.Now()
	err = netlink.RouteReplace(r)
	dur := time.Since(start)
	log.Debug("route: RouteReplace (default)",
		"family", want.Family, "table_id", want.TableID,
		"via", want.Via, "dev", want.Dev,
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	return err
}

// delTableDefaultNetlink removes the default route from the table. ENOENT
// (no such route) is swallowed and logged at DEBUG, matching the behaviour
// of the prior `ip route del` shellout which exited nonzero but the daemon
// treated as success when called via the wrapper.
func delTableDefaultNetlink(
	ctx context.Context, log *slog.Logger, family string, tableID int,
) error {
	_ = ctx
	famConst := familyToNetlink(family)

	r := &netlink.Route{
		Table:  tableID,
		Family: famConst,
		// Dst nil = default
	}
	start := time.Now()
	err := netlink.RouteDel(r)
	dur := time.Since(start)
	log.Debug("route: RouteDel (default)",
		"family", family, "table_id", tableID,
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil && (errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ESRCH)) {
		log.Debug("route: nothing to delete (already absent)")
		return nil
	}
	return err
}

// FindMainRADefault returns the RA-learned default route via iface in the
// main IPv6 routing table, if any. This is the source-of-truth gateway the
// daemon mirrors into the oob table.
func FindMainRADefault(
	ctx context.Context, iface string,
) (*CurrentRoute, error) {
	_ = ctx
	log := slog.Default().With("component", "route", "iface", iface, "op", "find-ra-default")
	log.Debug("route: FindMainRADefault entry")

	link, err := linkByName(log, iface)
	if err != nil {
		return nil, err
	}

	filter := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Table:     unix.RT_TABLE_MAIN,
		Protocol:  unix.RTPROT_RA,
	}
	mask := netlink.RT_FILTER_OIF | netlink.RT_FILTER_TABLE | netlink.RT_FILTER_PROTOCOL

	start := time.Now()
	routes, err := netlink.RouteListFiltered(unix.AF_INET6, filter, mask)
	dur := time.Since(start)
	log.Debug("route: RouteListFiltered (RA defaults)",
		"count", len(routes),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		return nil, err
	}

	for _, r := range routes {
		if !isDefaultRoute(r, unix.AF_INET6) {
			continue
		}
		return routeToCurrent(log, r)
	}
	return nil, nil
}

// linkByName wraps netlink.LinkByName with debug logging. Centralised so
// every call records the interface lookup.
func linkByName(log *slog.Logger, iface string) (netlink.Link, error) {
	start := time.Now()
	link, err := netlink.LinkByName(iface)
	dur := time.Since(start)
	log.Debug("link: LinkByName",
		"iface", iface,
		"index", linkIndex(link),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		return nil, fmt.Errorf("link %q: %w", iface, err)
	}
	return link, nil
}

func linkIndex(link netlink.Link) int {
	if link == nil {
		return 0
	}
	return link.Attrs().Index
}

// familyToNetlink converts our string family ("inet"/"inet6") into the
// AF_INET / AF_INET6 constant netlink expects.
func familyToNetlink(family string) int {
	if family == "inet" {
		return unix.AF_INET
	}
	return unix.AF_INET6
}
