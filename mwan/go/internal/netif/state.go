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

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
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
	// Flags is the raw IFA_F_* bitmask from netlink (linux/if_addr.h).
	// Useful flags include IFA_F_PERMANENT (0x80) which is set when the
	// address was added administratively rather than via SLAAC autoconf.
	Flags int
}

// IFA_F_* flag constants from linux/if_addr.h, exposed so callers can
// classify addresses (SLAAC vs manually-added vs deprecated etc.) without
// importing the kernel headers or vishvananda/netlink. Matches the values
// returned in CurrentAddr.Flags.
const (
	IFAFTemporary      = 0x01
	IFAFNoDAD          = 0x02
	IFAFOptimistic     = 0x04
	IFAFDADFailed      = 0x08
	IFAFHomeAddress    = 0x10
	IFAFDeprecated     = 0x20
	IFAFTentative      = 0x40
	IFAFPermanent      = 0x80
	IFAFManageTempAddr = 0x100
	IFAFNoPrefixRoute  = 0x200
	IFAFMcAutoJoin     = 0x400
	IFAFStablePrivacy  = 0x800
)

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
	log.DebugContext(ctx, "addrs: reconcile entry", "desired_count", len(desired))

	link, err := linkByName(log, iface)
	if err != nil {
		log.WarnContext(ctx, "addrs: linkByName failed", "err", err)
		return err
	}

	current, err := listAddrsNetlink(log, link)
	if err != nil {
		log.WarnContext(ctx, "addrs: listAddrsNetlink failed", "err", err)
		return fmt.Errorf("list addrs: %w", err)
	}
	log.DebugContext(ctx, "addrs: current snapshot",
		"count", len(current), "addrs", current)

	have := map[string]bool{}
	for _, c := range current {
		have[normalizeCIDR(c.CIDR)] = true
	}

	for _, w := range desired {
		key := normalizeCIDR(w.CIDR)
		if have[key] {
			log.DebugContext(ctx, "addrs: already present", "addr", w.CIDR)
			continue
		}
		log.DebugContext(ctx, "addrs: adding", "addr", w.CIDR, "family", w.family())
		if err := addAddrNetlink(ctx, log, link, w); err != nil {
			log.WarnContext(ctx, "addrs: addAddrNetlink failed", "addr", w.CIDR, "err", err)
			return fmt.Errorf("add addr %s: %w", w.CIDR, err)
		}
	}
	log.DebugContext(ctx, "addrs: reconcile complete", "applied", len(desired))
	return nil
}

// ListAddrs returns every address (v4 and v6) currently assigned to iface.
// Wraps RTM_GETADDR via netlink. Returns ENODEV-style errors as nil link
// not found, matching ReconcileAddrs' shape.
func ListAddrs(ctx context.Context, log *slog.Logger, iface string) ([]CurrentAddr, error) {
	log = log.With("component", "addrs", "op", "list", "iface", iface)
	link, err := linkByName(log, iface)
	if err != nil {
		log.WarnContext(ctx, "addrs: linkByName failed", "err", err)
		return nil, fmt.Errorf("link %s: %w", iface, err)
	}
	return listAddrsNetlink(log, link)
}

// listAddrsNetlink returns parsed addresses on link, both families. Uses
// netlink.AddrList (RTM_GETADDR) directly. Logged at DEBUG with counts.
func listAddrsNetlink(log *slog.Logger, link netlink.Link) ([]CurrentAddr, error) {
	startTime := realClock{}.Now()
	addrs, err := netlink.AddrList(link, unix.AF_UNSPEC)
	dur := realClock{}.Now().Sub(startTime)
	log.Debug(
		"addrs: AddrList",
		"link", link.Attrs().Name,
		"count", len(addrs),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		log.Warn("addrs: AddrList failed", "link", link.Attrs().Name, "err", err)
		return nil, fmt.Errorf("AddrList(%s): %w", link.Attrs().Name, err)
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
		out = append(out, CurrentAddr{CIDR: a.IPNet.String(), Family: fam, Flags: a.Flags})
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
	addr, err := netlink.ParseAddr(a.CIDR)
	if err != nil {
		log.WarnContext(ctx, "addrs: ParseAddr failed", "cidr", a.CIDR, "err", err)
		return fmt.Errorf("parse %q: %w", a.CIDR, err)
	}
	start := realClock{}.Now()
	err = netlink.AddrReplace(link, addr)
	dur := realClock{}.Now().Sub(start)
	log.DebugContext(
		ctx, "addrs: AddrReplace",
		"link", link.Attrs().Name,
		"cidr", addr.IPNet.String(),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		log.WarnContext(ctx, "addrs: AddrReplace failed",
			"link", link.Attrs().Name, "cidr", addr.IPNet.String(), "err", err)
		return fmt.Errorf("AddrReplace(%s,%s): %w", link.Attrs().Name, addr.IPNet.String(), err)
	}
	return nil
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
	log.DebugContext(ctx, "route: reconcile entry", "want", want)

	cur, err := getTableDefaultNetlink(log, want.Family, want.TableID)
	if err != nil {
		log.WarnContext(ctx, "route: getTableDefaultNetlink failed", "err", err)
		return fmt.Errorf("read table default: %w", err)
	}
	log.DebugContext(ctx, "route: current default in table",
		"current", cur, "want", want)

	if want.Via == "" {
		// Caller wants no default. Delete if present.
		if cur == nil {
			log.DebugContext(ctx, "route: no default present and none wanted")
			return nil
		}
		log.DebugContext(ctx, "route: removing default from table",
			"old_via", cur.Via, "old_dev", cur.Dev)
		err = delTableDefaultNetlink(ctx, log, want.Family, want.TableID)
		if err != nil {
			log.WarnContext(ctx, "route: delTableDefaultNetlink failed", "err", err)
		}
		return err
	}

	if cur != nil && cur.Via == want.Via && cur.Dev == want.Dev {
		log.DebugContext(ctx, "route: default already correct")
		return nil
	}

	log.DebugContext(
		ctx, "route: replacing default in table",
		"new_via", want.Via, "new_dev", want.Dev,
		"old", cur,
	)
	err = replaceTableDefaultNetlink(ctx, log, want)
	if err != nil {
		log.WarnContext(ctx, "route: replaceTableDefaultNetlink failed", "err", err)
	}
	return err
}

// ReconcileTableRoute ensures the table contains the desired non-default
// prefix route. Other entries in the table are not touched.
func ReconcileTableRoute(
	ctx context.Context, log *slog.Logger,
	want RouteSpec,
) error {
	log = log.With("component", "route",
		"family", want.Family, "table_id", want.TableID, "op", "reconcile-prefix")
	log.DebugContext(ctx, "route: reconcile prefix entry", "want", want)
	return replaceTableRouteNetlink(ctx, log, want)
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

	start := realClock{}.Now()
	routes, err := netlink.RouteListFiltered(famConst, filter, netlink.RT_FILTER_TABLE)
	dur := realClock{}.Now().Sub(start)
	log.Debug(
		"route: RouteListFiltered (default lookup)",
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
		log.Warn("route: RouteListFiltered failed",
			"family", family, "table_id", tableID, "err", err)
		return nil, fmt.Errorf("RouteListFiltered(default,%s,%d): %w", family, tableID, err)
	}

	for _, r := range routes {
		if !isDefaultRoute(r, famConst) {
			continue
		}
		cur, err := routeToCurrent(log, r)
		if err != nil {
			return nil, fmt.Errorf("routeToCurrent(default,%s,%d): %w", family, tableID, err)
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

// routeToCurrent converts a netlink.Route to our CurrentRoute. Multipath routes
// use the first next hop because CurrentRoute models one gateway and interface.
func routeToCurrent(log *slog.Logger, r netlink.Route) (*CurrentRoute, error) {
	dest := "default"
	if r.Dst != nil {
		prefixLength, _ := r.Dst.Mask.Size()
		if prefixLength != 0 {
			dest = r.Dst.String()
		}
	}
	cur := &CurrentRoute{
		Dest:   dest,
		Via:    "",
		Dev:    "",
		Metric: r.Priority,
	}
	if r.Gw != nil {
		cur.Via = r.Gw.String()
	} else if len(r.MultiPath) > 0 && r.MultiPath[0].Gw != nil {
		cur.Via = r.MultiPath[0].Gw.String()
		log.Debug("route: multipath route observed; using first nexthop",
			"hop_count", len(r.MultiPath), "first_via", cur.Via)
	}
	linkIndex := r.LinkIndex
	if linkIndex == 0 && len(r.MultiPath) > 0 {
		linkIndex = r.MultiPath[0].LinkIndex
	}
	if linkIndex != 0 {
		link, err := netlink.LinkByIndex(linkIndex)
		if err != nil {
			log.Warn("route: LinkByIndex failed", "index", linkIndex, "err", err)
			return nil, fmt.Errorf("LinkByIndex(%d): %w", linkIndex, err)
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

	start := realClock{}.Now()
	err = netlink.RouteReplace(r)
	dur := realClock{}.Now().Sub(start)
	log.DebugContext(
		ctx, "route: RouteReplace (default)",
		"family", want.Family, "table_id", want.TableID,
		"via", want.Via, "dev", want.Dev,
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		log.WarnContext(ctx, "route: RouteReplace (default) failed",
			"family", want.Family, "table_id", want.TableID, "err", err)
		return fmt.Errorf("RouteReplace(default,%s,%d): %w", want.Family, want.TableID, err)
	}
	return nil
}

func replaceTableRouteNetlink(
	ctx context.Context, log *slog.Logger, want RouteSpec,
) error {
	_ = ctx
	link, err := linkByName(log, want.Dev)
	if err != nil {
		return err
	}
	route, err := buildTableRoute(log, want, link)
	if err != nil {
		return err
	}

	start := realClock{}.Now()
	err = netlink.RouteReplace(route)
	dur := realClock{}.Now().Sub(start)
	log.DebugContext(
		ctx,
		"route: RouteReplace (prefix)",
		"family", want.Family, "table_id", want.TableID,
		"dest", want.Dest, "via", want.Via, "dev", want.Dev,
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		log.WarnContext(ctx, "route: RouteReplace (prefix) failed",
			"family", want.Family, "table_id", want.TableID, "dest", want.Dest, "err", err)
		return fmt.Errorf("RouteReplace(prefix,%s,%d,%s): %w", want.Family, want.TableID, want.Dest, err)
	}
	return nil
}

func buildTableRoute(log *slog.Logger, want RouteSpec, link netlink.Link) (*netlink.Route, error) {
	_, dst, err := net.ParseCIDR(want.Dest)
	if err != nil {
		log.Warn("route: ParseCIDR failed", "dest", want.Dest, "err", err)
		return nil, fmt.Errorf("ParseCIDR(%q): %w", want.Dest, err)
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Table:     want.TableID,
		Dst:       dst,
		Family:    familyToNetlink(want.Family),
		Priority:  want.Metric,
	}
	if want.Via == "" {
		route.Scope = netlink.SCOPE_LINK
		return route, nil
	}

	gateway := net.ParseIP(want.Via)
	if gateway == nil {
		log.Warn("route: parse gateway failed", "gateway", want.Via)
		return nil, fmt.Errorf("parse gateway %q: not a valid IP", want.Via)
	}
	route.Gw = gateway
	return route, nil
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
	start := realClock{}.Now()
	err := netlink.RouteDel(r)
	dur := realClock{}.Now().Sub(start)
	log.DebugContext(
		ctx,
		"route: RouteDel (default)",
		"family", family, "table_id", tableID,
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil && (errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ESRCH)) {
		log.DebugContext(ctx, "route: nothing to delete (already absent)")
		return nil
	}
	if err != nil {
		log.WarnContext(ctx, "route: RouteDel (default) failed",
			"family", family, "table_id", tableID, "err", err)
		return fmt.Errorf("RouteDel(default,%s,%d): %w", family, tableID, err)
	}
	return nil
}

// FindMainRADefault returns the RA-learned default route via iface in the
// main IPv6 routing table, if any. This is the source-of-truth gateway the
// daemon mirrors into the oob table.
func FindMainRADefault(
	ctx context.Context, iface string,
) (*CurrentRoute, error) {
	_ = ctx
	log := slog.Default().With("component", "route", "iface", iface, "op", "find-ra-default")
	log.DebugContext(ctx, "route: FindMainRADefault entry")

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

	start := realClock{}.Now()
	routes, err := netlink.RouteListFiltered(unix.AF_INET6, filter, mask)
	dur := realClock{}.Now().Sub(start)
	log.DebugContext(
		ctx,
		"route: RouteListFiltered (RA defaults)",
		"count", len(routes),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		log.WarnContext(ctx, "route: RouteListFiltered (RA defaults) failed", "iface", iface, "err", err)
		return nil, fmt.Errorf("RouteListFiltered(ra-defaults,%s): %w", iface, err)
	}

	for _, r := range routes {
		if !isDefaultRoute(r, unix.AF_INET6) {
			continue
		}
		return routeToCurrent(log, r)
	}
	return nil, nil
}

// IfaceDefaultGateway returns the main-table default-route gateway for iface.
func IfaceDefaultGateway(family string, iface string) (string, error) {
	log := slog.Default().With(
		"component", "route", "iface", iface, "family", family, "op", "iface-default-gateway",
	)
	link, err := linkByName(log, iface)
	if err != nil {
		return "", err
	}

	famConst := familyToNetlink(family)
	filter := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Table:     unix.RT_TABLE_MAIN,
	}
	mask := netlink.RT_FILTER_OIF | netlink.RT_FILTER_TABLE

	start := realClock{}.Now()
	routes, err := netlink.RouteListFiltered(famConst, filter, mask)
	dur := realClock{}.Now().Sub(start)
	log.Debug(
		"route: RouteListFiltered (iface default gateway)",
		"count", len(routes),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return "", nil
		}
		log.Warn("route: RouteListFiltered (iface default gateway) failed",
			"iface", iface, "family", family, "err", err)
		return "", fmt.Errorf("RouteListFiltered(iface-default,%s,%s): %w", iface, family, err)
	}

	for _, route := range routes {
		if !isDefaultRoute(route, famConst) {
			continue
		}
		current, err := routeToCurrent(log, route)
		if err != nil {
			return "", fmt.Errorf("routeToCurrent(iface-default,%s,%s): %w", iface, family, err)
		}
		if current.Via != "" {
			return current.Via, nil
		}
	}
	return "", nil
}

// DeleteMainRADefaults removes every RA-learned IPv6 default route from the
// main table on iface. Returns the number of defaults deleted.
func DeleteMainRADefaults(
	ctx context.Context, log *slog.Logger, iface string,
) (int, error) {
	log = log.With("component", "route", "iface", iface, "op", "delete-ra-defaults")
	log.DebugContext(ctx, "route: DeleteMainRADefaults entry")

	link, err := linkByName(log, iface)
	if err != nil {
		return 0, err
	}

	filter := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Table:     unix.RT_TABLE_MAIN,
		Protocol:  unix.RTPROT_RA,
	}
	mask := netlink.RT_FILTER_OIF | netlink.RT_FILTER_TABLE | netlink.RT_FILTER_PROTOCOL

	start := realClock{}.Now()
	routes, err := netlink.RouteListFiltered(unix.AF_INET6, filter, mask)
	dur := realClock{}.Now().Sub(start)
	log.DebugContext(
		ctx,
		"route: RouteListFiltered (delete RA defaults)",
		"count", len(routes),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		log.WarnContext(ctx, "route: RouteListFiltered (delete RA defaults) failed", "iface", iface, "err", err)
		return 0, fmt.Errorf("RouteListFiltered(delete-ra-defaults,%s): %w", iface, err)
	}

	deletedCount := 0
	for _, route := range routes {
		if !isDefaultRoute(route, unix.AF_INET6) {
			continue
		}
		via := ""
		if route.Gw != nil {
			via = route.Gw.String()
		}
		route.Family = unix.AF_INET6
		delStart := realClock{}.Now()
		delErr := netlink.RouteDel(&route)
		delDur := realClock{}.Now().Sub(delStart)
		log.DebugContext(
			ctx,
			"route: RouteDel (RA default)",
			"via", via,
			"duration_ms", delDur.Milliseconds(),
			"err", delErr,
		)
		if delErr != nil {
			if errors.Is(delErr, syscall.ENOENT) || errors.Is(delErr, syscall.ESRCH) {
				continue
			}
			log.WarnContext(ctx, "route: RouteDel (RA default) failed", "via", via, "err", delErr)
			return deletedCount, fmt.Errorf("RouteDel(ra-default,%s): %w", via, delErr)
		}
		deletedCount++
	}
	return deletedCount, nil
}

// linkByName wraps netlink.LinkByName with debug logging. Centralised so
// every call records the interface lookup.
func linkByName(log *slog.Logger, iface string) (netlink.Link, error) {
	start := realClock{}.Now()
	link, err := netlink.LinkByName(iface)
	dur := realClock{}.Now().Sub(start)
	log.Debug(
		"link: LinkByName",
		"iface", iface,
		"index", linkIndex(link),
		"duration_ms", dur.Milliseconds(),
		"err", err,
	)
	if err != nil {
		log.Warn("link: LinkByName failed", "iface", iface, "err", err)
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
