//go:build linux

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"golang.org/x/sys/unix"

	"goodkind.io/mwan/internal/bgp"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/notify"
)

func configurePlatformBGP(cfg *config.Config, speaker *bgp.Speaker, log *slog.Logger) {
	if len(cfg.BGP.Routers) == 0 {
		return
	}
	speaker.SetFIB(bgp.NewFIB(bgp.FIBConfig{
		Tables:        tablesFromConfig(cfg),
		InternalIface: fibInternalIface(cfg),
		Shadow:        cfg.BGP.RoutesShadowMode,
	}, log))
}

func startPlatformBGPAudit(
	cfg *config.Config,
	speaker *bgp.Speaker,
	notifier notify.Notifier,
	log *slog.Logger,
) func() {
	if len(cfg.BGP.Routers) == 0 {
		return nil
	}
	return startBGPAudit(speaker, notifier, log, realClock{})
}

func tablesFromConfig(cfg *config.Config) []int {
	tables := []int{unix.RT_TABLE_MAIN}
	wanNames := make([]string, 0, len(cfg.IfMgr.WAN))
	for wanName := range cfg.IfMgr.WAN {
		wanNames = append(wanNames, wanName)
	}
	sort.Strings(wanNames)
	for _, wanName := range wanNames {
		tables = append(tables, cfg.IfMgr.WAN[wanName].TableID)
	}
	return tables
}

func fibInternalIface(cfg *config.Config) string {
	if cfg.IfMgr.Modules.WAN == nil || cfg.IfMgr.Modules.WAN.Routes == nil {
		return ""
	}
	return cfg.IfMgr.Modules.WAN.Routes.InternalIface
}

func startBGPAudit(
	speaker *bgp.Speaker,
	notifier notify.Notifier,
	log *slog.Logger,
	clock clock,
) func() {
	ctx, cancel := context.WithCancel(context.Background())
	ticker := time.NewTicker(time.Minute)
	active := make(map[string]bgp.Violation)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(ctx, "BGP audit goroutine panicked", "err", fmt.Errorf("panic: %v", recovered))
			}
		}()
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				active = auditBGPRoutes(ctx, speaker, notifier, active, clock)
			}
		}
	}()
	return cancel
}

func auditBGPRoutes(
	ctx context.Context,
	speaker *bgp.Speaker,
	notifier notify.Notifier,
	active map[string]bgp.Violation,
	clock clock,
) map[string]bgp.Violation {
	violations := speaker.AuditAdjRibIn(ctx)
	current := make(map[string]bgp.Violation, len(violations))
	for _, violation := range violations {
		key := violation.Router + ":" + violation.Prefix.String()
		current[key] = violation
		notifier.Notify(ctx, notify.Event{
			Now:     clock.Now(),
			Level:   slog.LevelWarn,
			Kind:    "bgp-undeclared-prefix",
			Key:     key,
			Message: "BGP peer announced a prefix outside its allocation",
			Fields: []slog.Attr{
				slog.String("router", violation.Router),
				slog.String("peer", violation.Peer),
				slog.String("prefix", violation.Prefix.String()),
			},
		})
	}
	for key, violation := range active {
		if _, found := current[key]; found {
			continue
		}
		notifier.Resolve(
			ctx,
			"bgp-undeclared-prefix",
			key,
			"BGP peer no longer announces a prefix outside its allocation",
			slog.String("router", violation.Router),
			slog.String("peer", violation.Peer),
			slog.String("prefix", violation.Prefix.String()),
		)
	}
	return current
}
