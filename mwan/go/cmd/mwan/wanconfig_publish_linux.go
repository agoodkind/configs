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
	"goodkind.io/mwan/internal/yangpub"
)

// wanconfigPublishTimeout bounds the startup publish so a stalled
// datastore cannot delay the daemon's main loop.
const wanconfigPublishTimeout = 10 * time.Second

// publishWanconfig publishes the configuration the daemon just loaded into
// the wanconfig management datastore, when this host's config turns the
// gate on and the role carries the wan modules. Every failure is logged
// and swallowed: describing the system is not a precondition for running
// it, so the daemon starts identically whether or not the publish landed.
func publishWanconfig(
	ctx context.Context,
	logger *slog.Logger,
	cfg *config.Config,
	moduleConfigs ifmgr.ModuleConfigSet,
) {
	if !cfg.Wanconfig.Publish {
		return
	}
	log := logger.With("component", "wanconfig")

	gateway, ok, err := gatewayFromModuleConfigs(moduleConfigs)
	if err != nil {
		log.ErrorContext(ctx, "wanconfig: projection from module configs failed; running without a management surface", "err", err)
		return
	}
	if !ok {
		log.InfoContext(ctx, "wanconfig: publish enabled but this role carries no wan config; nothing to publish")
		return
	}

	pub, err := yangpub.New(log)
	if err != nil {
		log.ErrorContext(ctx, "wanconfig: datastore unavailable; running without a management surface", "err", err)
		return
	}
	defer func() {
		if closeErr := pub.Close(); closeErr != nil {
			log.ErrorContext(ctx, "wanconfig: datastore close failed", "err", closeErr)
		}
	}()

	publishCtx, cancel := context.WithTimeout(ctx, wanconfigPublishTimeout)
	defer cancel()
	// Publish logs its own failure detail; the daemon carries on either way.
	_ = wanconfig.Publish(publishCtx, log, runningReplacer{pub: pub}, gateway)
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
