//go:build linux

package oob

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const (
	ipAddrShowTimeout    = 5 * time.Second
	ipAddrMutateTimeout  = 5 * time.Second
	ipRouteShowTimeout   = 5 * time.Second
	ipRouteMutateTimeout = 5 * time.Second
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

// CurrentAddr is one parsed inet/inet6 address from `ip addr show <iface>`.
type CurrentAddr struct {
	CIDR   string
	Family string
}

// ReconcileAddrs ensures every spec in desired is present on iface.
// Foreign addresses are NOT removed; the daemon only adds what it owns.
// (Removal of unmanaged addresses is out of scope; trust the operator.)
func ReconcileAddrs(
	ctx context.Context, runner IPRunner, log *slog.Logger,
	iface string, desired []AddrSpec,
) error {
	log = log.With("component", "addrs", "iface", iface)
	current, err := listAddrs(ctx, runner, iface)
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
		if err := addAddr(ctx, runner, iface, w); err != nil {
			return fmt.Errorf("add addr %s: %w", w.CIDR, err)
		}
	}
	return nil
}

// listAddrs returns parsed addresses on iface, both families.
func listAddrs(
	ctx context.Context, runner IPRunner, iface string,
) ([]CurrentAddr, error) {
	out, err := runner.Run(ctx, ipAddrShowTimeout, "-br", "addr", "show", "dev", iface)
	if err != nil {
		return nil, err
	}
	return parseAddrBriefList(string(out)), nil
}

// parseAddrBriefList parses `ip -br addr show dev <iface>` lines:
//
//	mbrains  UP  158.247.70.13/26 192.168.1.2/24 fe80::.../64
//
// Each whitespace-separated token after the state is an address with /prefix.
// Pure function for unit-testing without exec.
func parseAddrBriefList(text string) []CurrentAddr {
	var out []CurrentAddr
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// fields: [iface state addr1 addr2 ...]
		if len(fields) < 3 {
			continue
		}
		for _, addr := range fields[2:] {
			if !strings.Contains(addr, "/") {
				continue
			}
			fam := "inet"
			if strings.Contains(addr, ":") {
				fam = "inet6"
			}
			out = append(out, CurrentAddr{CIDR: addr, Family: fam})
		}
	}
	return out
}

// normalizeCIDR lower-cases the address half so comparisons are stable
// across IPv6 representations the kernel may return.
func normalizeCIDR(cidr string) string {
	return strings.ToLower(cidr)
}

func addAddr(
	ctx context.Context, runner IPRunner, iface string, a AddrSpec,
) error {
	flag := "-6"
	if a.family() == "inet" {
		flag = "-4"
	}
	_, err := runner.Run(ctx, ipAddrMutateTimeout,
		flag, "addr", "replace", a.CIDR, "dev", iface,
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
	Table   string // routing table name (e.g. "oob")
	Metric  int    // optional; 0 omits the metric
}

// CurrentRoute is one parsed entry from `ip route show table <t>`.
type CurrentRoute struct {
	Dest   string
	Via    string
	Dev    string
	Metric int
}

// ReconcileTableDefault ensures the table contains exactly the desired
// default route. If the current default's gateway differs, it's replaced.
// If no default exists, it's added. Other entries in the table are not
// touched. Pass "" to clear the default route from the table.
func ReconcileTableDefault(
	ctx context.Context, runner IPRunner, log *slog.Logger,
	want RouteSpec,
) error {
	log = log.With("component", "route",
		"family", want.Family, "table", want.Table)
	cur, err := getTableDefault(ctx, runner, want.Family, want.Table)
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
		return delTableDefault(ctx, runner, want.Family, want.Table)
	}

	if cur != nil && cur.Via == want.Via && cur.Dev == want.Dev {
		log.Debug("route: default already correct")
		return nil
	}

	log.Info("route: replacing default in table",
		"new_via", want.Via, "new_dev", want.Dev,
		"old", cur,
	)
	return replaceTableDefault(ctx, runner, want)
}

func getTableDefault(
	ctx context.Context, runner IPRunner, family, table string,
) (*CurrentRoute, error) {
	flag := "-6"
	if family == "inet" {
		flag = "-4"
	}
	out, err := runner.Run(ctx, ipRouteShowTimeout,
		flag, "route", "show", "table", table, "default",
	)
	if err != nil {
		// "FIB table does not exist" means the table has zero entries
		// for this family. The kernel auto-creates the table the first
		// time a route is added to it, so we treat this as "no default
		// present" rather than a hard error.
		if strings.Contains(strings.ToLower(err.Error()), "fib table does not exist") {
			return nil, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "default") {
			continue
		}
		return parseRouteLine(line), nil
	}
	return nil, nil
}

// parseRouteLine parses a single `ip [-6] route` line:
//
//	default via fe80::f61e:57ff:fe06:4983 dev mbrains metric 1024 pref medium
//
// Pure function for unit-testing.
func parseRouteLine(line string) *CurrentRoute {
	fields := strings.Fields(line)
	if len(fields) < 1 {
		return nil
	}
	r := &CurrentRoute{Dest: fields[0]}
	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case "via":
			if i+1 < len(fields) {
				r.Via = fields[i+1]
				i++
			}
		case "dev":
			if i+1 < len(fields) {
				r.Dev = fields[i+1]
				i++
			}
		case "metric":
			if i+1 < len(fields) {
				if m, err := strconv.Atoi(fields[i+1]); err == nil {
					r.Metric = m
				}
				i++
			}
		}
	}
	return r
}

func replaceTableDefault(
	ctx context.Context, runner IPRunner, want RouteSpec,
) error {
	flag := "-6"
	if want.Family == "inet" {
		flag = "-4"
	}
	args := []string{flag, "route", "replace", "default", "via", want.Via,
		"dev", want.Dev, "table", want.Table}
	if want.Metric > 0 {
		args = append(args, "metric", strconv.Itoa(want.Metric))
	}
	_, err := runner.Run(ctx, ipRouteMutateTimeout, args...)
	return err
}

func delTableDefault(
	ctx context.Context, runner IPRunner, family, table string,
) error {
	flag := "-6"
	if family == "inet" {
		flag = "-4"
	}
	_, err := runner.Run(ctx, ipRouteMutateTimeout,
		flag, "route", "del", "default", "table", table,
	)
	return err
}

// FindMainRADefault returns the RA-learned default route via iface in the
// main IPv6 routing table, if any. This is the source-of-truth gateway the
// daemon mirrors into the oob table.
func FindMainRADefault(
	ctx context.Context, runner IPRunner, iface string,
) (*CurrentRoute, error) {
	out, err := runner.Run(ctx, ipRouteShowTimeout,
		"-6", "route", "show", "default", "dev", iface, "proto", "ra",
	)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "default") {
			continue
		}
		return parseRouteLine(line), nil
	}
	return nil, nil
}