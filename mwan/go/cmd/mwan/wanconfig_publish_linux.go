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
	"goodkind.io/mwan/internal/ifmgr/modules/health"
	"goodkind.io/mwan/internal/ifmgr/modules/npt"
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
// registrations serve reads, and the agent poller feeding BGP state.
type wanconfigSurface struct {
	store      *wanstate.Store
	pub        yangpub.Publisher
	log        *slog.Logger
	stopPoller context.CancelFunc
}

// Close stops the poller and releases the datastore connection with its
// provider registrations. Safe on a surface whose parts partly failed.
func (s *wanconfigSurface) Close() {
	if s.stopPoller != nil {
		s.stopPoller()
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

	gateway, ok, err := gatewayFromModuleConfigs(moduleConfigs)
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
	ownPublishedModules(ctx, log, pub)

	store := wanstate.New()
	surface := &wanconfigSurface{
		store:      store,
		pub:        pub,
		log:        log,
		stopPoller: nil,
	}
	if err := registerLiveStateProviders(ctx, log, pub, store, gateway); err != nil {
		log.ErrorContext(ctx, "wanconfig: provider registration failed; serving configuration only", "err", err)
		return surface
	}
	pollerCtx, stopPoller := context.WithCancel(ctx)
	surface.stopPoller = stopPoller
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(pollerCtx,
					"wanconfig: agent poller panicked; routing-session state goes stale",
					"err", fmt.Sprint(recovered))
			}
		}()
		pollAgentBGP(pollerCtx, log, cfg, store)
	}()
	log.InfoContext(ctx, "wanconfig: live-state providers registered",
		"members", len(gateway.Members))
	return surface
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
// config, and which members are probed from the health config. It returns
// ok=false when the set carries no wan.routes config, which is every role
// but wan; that is a quiet no-publish, not an error.
func gatewayFromModuleConfigs(configs ifmgr.ModuleConfigSet) (wanconfig.Gateway, bool, error) {
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
