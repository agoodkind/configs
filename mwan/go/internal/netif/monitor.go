//go:build linux

package netif

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// EventKind classifies a kernel netlink event observed via `ip monitor`.
type EventKind int

const (
	// EvUnknown is emitted when the line did not match any known pattern.
	// Useful for debug visibility; daemon ignores these.
	EvUnknown EventKind = iota
	// EvRouteAdded fires when a `default` route via the watched iface is added.
	// Includes RA-learned routes (proto ra) and DHCP-installed routes.
	EvRouteAdded
	// EvRouteDeleted fires when such a default route is removed.
	EvRouteDeleted
	// EvAddrAdded fires when any address is added on the watched iface.
	// Used to detect SLAAC arrivals and renumber events.
	EvAddrAdded
	// EvAddrDeleted fires when an address is removed from the watched iface.
	EvAddrDeleted
)

func (k EventKind) String() string {
	switch k {
	case EvRouteAdded:
		return "route-added"
	case EvRouteDeleted:
		return "route-deleted"
	case EvAddrAdded:
		return "addr-added"
	case EvAddrDeleted:
		return "addr-deleted"
	default:
		return "unknown"
	}
}

// Event is one parsed netlink event the daemon should react to.
type Event struct {
	Kind   EventKind
	Family string // "inet" or "inet6"
	Iface  string
	Raw    string // verbatim line for debug
	// Route-specific fields (populated when Kind is EvRouteAdded/Deleted).
	Dest string
	Via  string
	// Addr-specific fields (populated when Kind is EvAddrAdded/Deleted).
	CIDR string
}

// MonitorConfig configures one Monitor instance.
type MonitorConfig struct {
	Iface         string
	RestartBackoff time.Duration // delay before restarting `ip monitor` after EOF/error
}

// Monitor is a long-lived consumer of `ip [-6] monitor route` and
// `ip [-6] monitor address` output for one interface. It emits parsed
// Events on Events. Callers must drain Events to avoid blocking the
// monitor goroutine.
type Monitor struct {
	cfg    MonitorConfig
	log    *slog.Logger
	Events chan Event
}

// NewMonitor returns a started Monitor. Cancel ctx to stop it cleanly.
func NewMonitor(
	ctx context.Context, log *slog.Logger, cfg MonitorConfig,
) *Monitor {
	if cfg.RestartBackoff == 0 {
		cfg.RestartBackoff = 2 * time.Second
	}
	m := &Monitor{
		cfg:    cfg,
		log:    log.With("component", "monitor", "iface", cfg.Iface),
		Events: make(chan Event, 64),
	}
	go m.runFamily(ctx, "inet6")
	go m.runFamily(ctx, "inet")
	return m
}

// runFamily runs one `ip [-4|-6] monitor` per family; each restart is logged
// at INFO so unexpected loops are visible.
func (m *Monitor) runFamily(ctx context.Context, family string) {
	flag := "-6"
	if family == "inet" {
		flag = "-4"
	}
	gName := fmt.Sprintf("monitor-%s", family)
	logger := m.log.With("goroutine", gName, "family", family)

	for {
		if ctx.Err() != nil {
			logger.Debug("monitor: context cancelled, exiting")
			return
		}
		logger.Info("monitor: starting ip monitor", "argv",
			[]string{"ip", flag, "monitor", "route", "address"})

		cmd := exec.CommandContext(ctx, "ip", flag, "monitor", "route", "address")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			logger.Error("monitor: stdout pipe", "err", err)
			sleepOrCancel(ctx, m.cfg.RestartBackoff)
			continue
		}
		cmd.Stderr = cmd.Stdout
		if err := cmd.Start(); err != nil {
			logger.Error("monitor: start", "err", err)
			sleepOrCancel(ctx, m.cfg.RestartBackoff)
			continue
		}

		m.consume(ctx, logger, family, stdout)

		// Wait for the process to fully exit so we don't leak.
		_ = cmd.Wait()
		logger.Warn("monitor: ip monitor exited; restarting after backoff",
			"backoff", m.cfg.RestartBackoff.String())
		sleepOrCancel(ctx, m.cfg.RestartBackoff)
	}
}

// consume reads stdout one line at a time and emits parsed Events.
// Returns when ctx is cancelled or stdout closes.
func (m *Monitor) consume(
	ctx context.Context, log *slog.Logger, family string, r io.Reader,
) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := strings.TrimRight(scanner.Text(), " \t")
		if line == "" {
			continue
		}
		log.Debug("monitor: raw line", "line", line)
		ev := parseMonitorLine(line, family, m.cfg.Iface)
		if ev.Kind == EvUnknown {
			continue
		}
		log.Debug("monitor: parsed event",
			"kind", ev.Kind.String(), "dest", ev.Dest, "via", ev.Via,
			"cidr", ev.CIDR)
		select {
		case m.Events <- ev:
		case <-ctx.Done():
			return
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		log.Warn("monitor: scanner err", "err", err)
	}
}

// parseMonitorLine extracts an Event from one line of `ip monitor` output.
// Returns Kind=EvUnknown when the line does not concern the watched iface
// or does not match a known pattern.
//
// Sample inputs:
//
//	"Deleted 2: mbrains    inet6 fe80::.../64 scope link"
//	"3: mbrains    inet 158.247.70.13/26 brd 158.247.70.63 scope global mbrains"
//	"Deleted default via fe80::f61e:57ff:fe06:4983 dev mbrains proto ra metric 1024 pref high"
//	"default via fe80::f61e:57ff:fe06:4983 dev mbrains proto ra metric 1024 pref high"
//
// Pure function for unit-testing without exec.
func parseMonitorLine(line, family, iface string) Event {
	ev := Event{Family: family, Iface: iface, Raw: line}
	deleted := false
	work := line
	if strings.HasPrefix(work, "Deleted ") {
		deleted = true
		work = strings.TrimPrefix(work, "Deleted ")
	}

	// Address line: starts with "<idx>: <iface>" then "inet"/"inet6".
	if isAddrLine(work) {
		idx := strings.Index(work, " inet")
		if idx < 0 {
			return ev
		}
		// Confirm iface match.
		head := strings.Fields(work[:idx])
		// head[0] = "N:" (or "N:") , head[1] = iface, ...
		if len(head) < 2 || strings.TrimRight(head[1], ":") != iface {
			return ev
		}
		rest := strings.Fields(work[idx:])
		// rest[0] = "inet" or "inet6", rest[1] = "ADDR/PREFIX"
		if len(rest) < 2 {
			return ev
		}
		ev.CIDR = rest[1]
		// Family from "inet"/"inet6" overrides what the caller passed.
		if rest[0] == "inet" {
			ev.Family = "inet"
		} else if rest[0] == "inet6" {
			ev.Family = "inet6"
		}
		if deleted {
			ev.Kind = EvAddrDeleted
		} else {
			ev.Kind = EvAddrAdded
		}
		return ev
	}

	// Route line: starts with "default" (or a network spec).
	// Only emit for default routes via the watched iface.
	if strings.HasPrefix(work, "default ") {
		fields := strings.Fields(work)
		r := parseRouteLine(strings.Join(fields, " "))
		if r == nil || r.Dev != iface {
			return ev
		}
		ev.Dest = r.Dest
		ev.Via = r.Via
		if deleted {
			ev.Kind = EvRouteDeleted
		} else {
			ev.Kind = EvRouteAdded
		}
		return ev
	}

	return ev
}

// isAddrLine returns true if the line looks like an address-add/del event
// (starts with "<digits>:").
func isAddrLine(s string) bool {
	for i, c := range s {
		if c == ':' && i > 0 {
			return true
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return false
}

// sleepOrCancel sleeps for d or until ctx is cancelled, whichever first.
func sleepOrCancel(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}