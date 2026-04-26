//go:build linux

package oob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"goodkind.io/mwan/internal/netif"
)

// Daemon ties V6Manager, V4Manager, DHCPClient, AlertManager, and the
// kernel monitor into one reconcile loop.
type Daemon struct {
	cfg     DaemonConfig
	log     *slog.Logger
	v6      *V6Manager
	v4      *V4Manager
	rules   []netif.DesiredRule
	alerts  *AlertManager
	monitor *netif.Monitor
	dhcp    *netif.DHCPClient

	mu        sync.Mutex
	startedAt time.Time
}

// DaemonConfig captures all timing knobs and the sub-configs.
type DaemonConfig struct {
	ReconcileInterval time.Duration
	V6                V6Config
	V4                V4Config
	Alerts            AlertConfig
	DHCP              netif.DHCPConfig
	Rules             []netif.DesiredRule
}

// NewDaemon constructs (does not start) a Daemon.
func NewDaemon(log *slog.Logger, cfg DaemonConfig) *Daemon {
	d := &Daemon{
		cfg:    cfg,
		log:    log,
		v6:     NewV6Manager(log, cfg.V6),
		v4:     NewV4Manager(log, cfg.V4),
		rules:  cfg.Rules,
		alerts: NewAlertManager(log, cfg.Alerts),
	}
	return d
}

// Run starts every goroutine and blocks until ctx is cancelled. The
// caller is responsible for signal handling (SIGINT/SIGTERM cancel ctx).
// On exit, kernel state is left in place: the daemon does NOT remove the
// addresses, rules, or routes it installed, so a daemon restart does not
// flap the OOB tunnel.
func (d *Daemon) Run(ctx context.Context) error {
	d.mu.Lock()
	d.startedAt = time.Now()
	d.mu.Unlock()

	d.log.Info("oob: daemon starting",
		"iface", d.cfg.V6.Iface,
		"oob_table", d.cfg.V6.OOBTable,
		"reconcile_interval", d.cfg.ReconcileInterval.String(),
	)

	// Start the kernel route/address monitor and the DHCP client.
	d.monitor = netif.NewMonitor(ctx, d.log, netif.MonitorConfig{
		Iface: d.cfg.V6.Iface,
	})
	d.dhcp = netif.StartDHCPClient(ctx, d.log, d.cfg.DHCP)

	// Initial reconcile: rules first (so OOB egress works as soon as
	// addresses arrive), then v6 (which solicits RA, populates oob default).
	traceID := newTraceID()
	initLog := d.log.With("trace", traceID, "phase", "initial-reconcile")
	if err := d.reconcileAll(ctx, initLog); err != nil {
		initLog.Error("oob: initial reconcile failed (will retry on next tick)",
			"err", err)
	}

	tick := time.NewTicker(d.cfg.ReconcileInterval)
	defer tick.Stop()

	d.log.Info("oob: entering main loop")
	for {
		select {
		case <-ctx.Done():
			d.log.Info("oob: context cancelled; daemon exiting (kernel state preserved)")
			return nil

		case ev, ok := <-d.monitor.Events:
			if !ok {
				d.log.Warn("oob: monitor events channel closed")
				continue
			}
			tid := newTraceID()
			evLog := d.log.With("trace", tid, "phase", "kernel-event",
				"kind", ev.Kind.String(), "iface", ev.Iface)
			d.handleKernelEvent(ctx, evLog, ev)

		case lease, ok := <-d.dhcp.Events:
			if !ok {
				d.log.Warn("oob: dhcp events channel closed")
				continue
			}
			tid := newTraceID()
			ldLog := d.log.With("trace", tid, "phase", "dhcp-event",
				"state", lease.State.String())
			d.handleDHCPEvent(ctx, ldLog, lease)

		case <-tick.C:
			tid := newTraceID()
			tLog := d.log.With("trace", tid, "phase", "periodic-reconcile")
			tLog.Debug("oob: tick")
			if err := d.reconcileAll(ctx, tLog); err != nil {
				tLog.Error("oob: periodic reconcile failed", "err", err)
			}
			d.alerts.EvaluateRA(d.v6.LastRASeen(), time.Now())
			d.alerts.EvaluateV4(d.v4.LastBound(), time.Now())
		}
	}
}

// reconcileAll runs the full reconcile sequence: rules, v6 addr+default,
// then re-evaluates alerts. v4 reconcile is event-driven only.
func (d *Daemon) reconcileAll(ctx context.Context, log *slog.Logger) error {
	if err := netif.ReconcileRules(ctx, log, d.rules); err != nil {
		return err
	}
	if err := d.v6.Reconcile(ctx); err != nil {
		return err
	}
	return nil
}

func (d *Daemon) handleKernelEvent(
	ctx context.Context, log *slog.Logger, ev netif.Event,
) {
	switch ev.Kind {
	case netif.EvRouteAdded, netif.EvRouteDeleted:
		if err := d.v6.HandleRouteEvent(ctx, ev); err != nil {
			log.Warn("oob: v6 route event handling failed", "err", err)
		}
	case netif.EvAddrAdded:
		if newPx := d.v6.HandleAddrEvent(ev); newPx != "" {
			d.alerts.NotifyRenumber("(prior)", newPx)
		}
	case netif.EvAddrDeleted:
		log.Debug("oob: addr-deleted event observed (no action)")
	}
}

func (d *Daemon) handleDHCPEvent(
	ctx context.Context, log *slog.Logger, lease netif.LeaseInfo,
) {
	if err := d.v4.HandleLease(ctx, lease); err != nil {
		log.Warn("oob: v4 lease handling failed", "err", err)
	}
}

// newTraceID returns a short random hex string used as a per-iteration
// correlation identifier in log records.
func newTraceID() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
