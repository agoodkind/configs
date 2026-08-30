//go:build linux

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/ifmgr/modules/cloudflaredtap"
	"goodkind.io/mwan/internal/ifmgr/modules/health"
	"goodkind.io/mwan/internal/ifmgr/modules/npt"
	"goodkind.io/mwan/internal/ifmgr/modules/oobv4"
	"goodkind.io/mwan/internal/ifmgr/modules/oobv6"
	"goodkind.io/mwan/internal/ifmgr/modules/wanroutes"
	"goodkind.io/mwan/internal/wanconfig"
	"goodkind.io/mwan/internal/wanstate"
	"goodkind.io/mwan/internal/yangpub"
)

// wanconfigPublishTimeout bounds the startup publish so a stalled
// datastore cannot delay the daemon's main loop.
const wanconfigPublishTimeout = 10 * time.Second

// wanconfigSurface is the running management surface: the snapshot store
// the modules write, the open datastore connection whose provider
// registrations serve reads, the agent poller feeding BGP state, and the
// notifier streaming transitions.
type wanconfigSurface struct {
	store *wanstate.Store
	pub   yangpub.Publisher
	log   *slog.Logger
	stop  context.CancelFunc
	// senderDone closes when the notifier's sender goroutine has
	// returned. Close waits on it before releasing the connection,
	// because a send still inside the binding when the connection is
	// freed crashes the process.
	senderDone <-chan struct{}
}

// Close stops the poller and the notifier, waits for the sender to leave
// the binding, and releases the datastore connection with its
// registrations. Safe on a surface whose parts partly failed. The wait
// is bounded: a send blocks at most for sysrepo's internal subscriber
// timeout.
func (s *wanconfigSurface) Close() {
	if s.stop != nil {
		s.stop()
	}
	if s.senderDone != nil {
		<-s.senderDone
	}
	if s.pub != nil {
		if err := s.pub.Close(); err != nil {
			s.log.Error("wanconfig: datastore close failed", "err", err)
		}
	}
}

// startWanconfigSurface publishes the configuration the daemon just
// loaded into the wanconfig management datastore and registers the
// operational providers that serve live state, when this host's config
// turns the gate on and the role carries the wan modules. It returns nil
// when the host serves no surface. Every failure is logged and swallowed:
// describing the system is not a precondition for running it, so the
// daemon starts identically whether or not the surface came up.
func startWanconfigSurface(
	ctx context.Context,
	logger *slog.Logger,
	cfg *config.Config,
	moduleConfigs ifmgr.ModuleConfigSet,
) *wanconfigSurface {
	if !cfg.Wanconfig.Publish {
		return nil
	}
	log := logger.With("component", "wanconfig")

	gateway, ok, err := gatewayFromModuleConfigs(cfg, moduleConfigs)
	if err != nil {
		log.ErrorContext(ctx, "wanconfig: projection from module configs failed; running without a management surface", "err", err)
		return nil
	}
	if !ok {
		log.InfoContext(ctx, "wanconfig: publish enabled but this role carries no wan config; nothing to publish")
		return nil
	}

	pub, err := yangpub.New(log)
	if err != nil {
		log.ErrorContext(ctx, "wanconfig: datastore unavailable; running without a management surface", "err", err)
		return nil
	}

	publishCtx, cancel := context.WithTimeout(ctx, wanconfigPublishTimeout)
	defer cancel()
	// Publish logs its own failure detail; the daemon carries on either way.
	_ = wanconfig.Publish(publishCtx, log, runningReplacer{pub: pub}, gateway)
	// Ownership is startup work like the publish, so it carries the same
	// bound: the daemon's main loop must not wait on the datastore.
	ownCtx, ownCancel := context.WithTimeout(ctx, wanconfigPublishTimeout)
	ownPublishedModules(ownCtx, log, pub)
	ownCancel()

	store := wanstate.New()
	surfaceCtx, stopSurface := context.WithCancel(ctx)
	surface := &wanconfigSurface{
		store:      store,
		pub:        pub,
		log:        log,
		stop:       stopSurface,
		senderDone: nil,
	}
	// The notifier observes the store before any module writes, so the
	// first real transition already streams.
	notifier := newSurfaceNotifier(log, gateway)
	store.Observe(notifier)
	surface.senderDone = startNotifierSender(surfaceCtx, log, notifier, pub)
	if err := registerLiveStateProviders(ctx, log, pub, store, gateway); err != nil {
		log.ErrorContext(ctx, "wanconfig: provider registration failed; serving configuration only", "err", err)
		return surface
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(surfaceCtx,
					"wanconfig: agent poller panicked; routing-session state goes stale",
					"err", fmt.Sprint(recovered))
			}
		}()
		pollAgentBGP(surfaceCtx, log, cfg, store)
	}()
	log.InfoContext(ctx, "wanconfig: live-state providers registered",
		"members", len(gateway.Members))
	return surface
}

// daemonSettings projects the daemon settings the loaded configuration
// carries: the rollback watchdog's policy from the monolith config, and
// the out-of-band and tap module configs when this role runs them. A
// section the configuration does not carry stays absent, so the tree
// never invents values another host owns.
func daemonSettings(cfg *config.Config, configs ifmgr.ModuleConfigSet) wanconfig.DaemonSettings {
	settings := wanconfig.DaemonSettings{
		Watchdog: watchdogSettings(cfg),
		OOB:      oobSettings(configs),
		Tap:      tapSettings(configs),
	}
	return settings
}

// watchdogSettings projects the watchdog section. The rendered section
// always names the unit it drives, so the name doubles as presence.
func watchdogSettings(cfg *config.Config) wanconfig.WatchdogSettings {
	var none wanconfig.WatchdogSettings
	if cfg == nil || cfg.Watchdog.ServiceName == "" {
		return none
	}
	watchdog := wanconfig.WatchdogSettings{
		Present:                      true,
		DeployWindowMinutes:          clampUint16(cfg.Watchdog.DeployWindowMinutes),
		ConnectivityTimeoutSeconds:   clampUint16(cfg.Watchdog.ConnectivityTimeoutSeconds),
		CheckIntervalHealthySeconds:  clampUint16(cfg.Watchdog.CheckIntervalHealthy),
		CheckIntervalDegradedSeconds: clampUint16(cfg.Watchdog.CheckIntervalDegraded),
		PostRollbackGraceSeconds:     clampUint16(cfg.Watchdog.PostRollbackGraceSeconds),
		AlertCooldownSeconds:         clampUint16(cfg.Watchdog.AlertCooldownSeconds),
		DeployGracePeriodSeconds:     clampUint16(cfg.Watchdog.DeployGracePeriodSeconds),
		MaxRollbackAttempts:          clampUint8(cfg.Watchdog.MaxRollbackAttempts),
		SnapshotHealthyThreshold:     clampUint16(cfg.Watchdog.SnapshotHealthyThreshold),
		MaxKnownGoodSnapshots:        clampUint8(cfg.Watchdog.MaxKnownGoodSnapshots),
		PingTargets:                  nil,
	}
	// The watchdog probes exactly these two targets, one per family.
	for _, raw := range []string{cfg.Network.PingTargetIPv6, cfg.Network.PingTargetIPv4} {
		if raw == "" {
			continue
		}
		address, err := netip.ParseAddr(raw)
		if err != nil {
			slog.Warn("wanconfig: watchdog ping target unparsable; not published",
				"value", raw, "err", err)
			continue
		}
		watchdog.PingTargets = append(watchdog.PingTargets, address)
	}
	return watchdog
}

// oobSettings projects the out-of-band module configs this role runs.
func oobSettings(configs ifmgr.ModuleConfigSet) wanconfig.OOBSettings {
	var oob wanconfig.OOBSettings
	if v6, ok := configs["oobv6"].(oobv6.Config); ok && v6.Iface != "" {
		oob.V6Present = true
		oob.V6Iface = v6.Iface
		oob.V6TableID = clampUint32(v6.OOBTableID)
		oob.ManageSLAACRule = v6.ManageSLAACRule
		oob.SLAACRulePriority = clampUint32(v6.SLAACRulePriority)
		if v6.OOBAddr != "" {
			address, err := netip.ParseAddr(v6.OOBAddr)
			if err != nil {
				slog.Warn("wanconfig: oob v6 address unparsable; not published",
					"value", v6.OOBAddr, "err", err)
			} else {
				oob.V6Addr = address
			}
		}
	}
	if v4, ok := configs["oobv4"].(oobv4.Config); ok && v4.Iface != "" {
		oob.V4Present = true
		oob.V4Iface = v4.Iface
		oob.V4TableID = clampUint32(v4.OOBTableID)
	}
	return oob
}

// tapSettings projects the tunnel-tap module config this role runs.
func tapSettings(configs ifmgr.ModuleConfigSet) wanconfig.TapSettings {
	var tap wanconfig.TapSettings
	if section, ok := configs["cloudflared_tap"].(cloudflaredtap.Config); ok && section.Unit != "" {
		tap.Present = true
		tap.Unit = section.Unit
		tap.DowngradePatterns = append([]string(nil), section.DowngradePatterns...)
	}
	return tap
}

// clampUint16 narrows a config int onto the model's leaf range.
func clampUint16(value int) uint16 {
	if value < 0 {
		return 0
	}
	if value > int(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(value)
}

// clampUint8 narrows a config int onto the model's leaf range.
func clampUint8(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > int(^uint8(0)) {
		return ^uint8(0)
	}
	return uint8(value)
}

// clampUint32 narrows a config int onto the model's leaf range. The
// comparison widens to uint64 so the bound stays representable where
// int is 32 bits.
func clampUint32(value int) uint32 {
	if value < 0 {
		return 0
	}
	if uint64(value) > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(value)
}

// runningReplacer adapts the sysrepo binding to the one capability the
// projection package asks for: replace the owned subtrees of the running
// datastore in a single transaction.
type runningReplacer struct {
	pub yangpub.Publisher
}

// ReplaceConfig implements wanconfig.Publisher over the binding.
func (r runningReplacer) ReplaceConfig(ctx context.Context, ownedPaths []string, items []wanconfig.Item) error {
	published := make([]yangpub.Item, 0, len(items))
	for _, item := range items {
		published = append(published, yangpub.Item{Path: item.Path, Value: item.Value})
	}
	if err := r.pub.ReplaceItems(ctx, yangpub.DatastoreRunning, ownedPaths, published); err != nil {
		slog.ErrorContext(ctx, "wanconfig: datastore replace failed",
			"items", len(published), "err", err)
		return fmt.Errorf("replace running config: %w", err)
	}
	return nil
}

// gatewayFromModuleConfigs projects the wan role's typed runtime module
// configs onto the gateway the surface publishes. It reads the same values
// the daemon is about to run with: the member list and translation prefixes
// from the wan.routes config, the internal translation prefix from the npt
// config, which members are probed from the health config, and the daemon
// settings the loaded monolith configuration carries. It returns ok=false
// when the set carries no wan.routes config, which is every role but wan;
// that is a quiet no-publish, not an error.
func gatewayFromModuleConfigs(cfg *config.Config, configs ifmgr.ModuleConfigSet) (wanconfig.Gateway, bool, error) {
	logger := slog.Default().With("component", "wanconfig")
	var none wanconfig.Gateway
	routesCfg, isRoutes := configs["wan.routes"].(wanroutes.Config)
	if !isRoutes || len(routesCfg.WANs) == 0 {
		return none, false, nil
	}

	internalPrefix := netip.Prefix{}
	if nptCfg, isNPT := configs["npt"].(npt.Config); isNPT && nptCfg.InternalPrefix != "" {
		parsed, err := netip.ParsePrefix(nptCfg.InternalPrefix)
		if err != nil {
			logger.Warn("wanconfig: internal prefix unparsable", "value", nptCfg.InternalPrefix, "err", err)
			return none, false, fmt.Errorf("wanconfig: internal prefix %q: %w", nptCfg.InternalPrefix, err)
		}
		internalPrefix = parsed
	}

	probed := map[string]bool{}
	if healthCfg, isHealth := configs["health"].(health.Config); isHealth {
		for _, wan := range healthCfg.WANs {
			probed[wan.Name] = true
		}
	}

	gateway := wanconfig.Gateway{
		InternalIface: routesCfg.InternalIface,
		Members:       make([]wanconfig.Member, 0, len(routesCfg.WANs)),
		Daemon:        daemonSettings(cfg, configs),
	}
	for _, wan := range routesCfg.WANs {
		member := wanconfig.Member{
			Name:        wan.Name,
			Iface:       wan.Iface,
			Tier:        wanroutes.TierOf(wan.Name),
			ProbePolicy: "",
			NPTInternal: netip.Prefix{},
			NPTExternal: netip.Prefix{},
		}
		if probed[wan.Name] {
			// The probe policy is named after the member: the health module
			// keys its per-member policy by the same name.
			member.ProbePolicy = wan.Name
		}
		if wan.NptPrefix != "" {
			external, err := netip.ParsePrefix(wan.NptPrefix)
			if err != nil {
				logger.Warn("wanconfig: member npt prefix unparsable",
					"member", wan.Name, "value", wan.NptPrefix, "err", err)
				return none, false, fmt.Errorf("wanconfig: member %s npt prefix %q: %w", wan.Name, wan.NptPrefix, err)
			}
			member.NPTInternal = internalPrefix
			member.NPTExternal = external
		}
		gateway.Members = append(gateway.Members, member)
	}
	return gateway, true, nil
}
