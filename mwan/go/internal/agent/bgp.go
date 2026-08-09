package agent

import (
	"fmt"
	"log/slog"
	"net/netip"

	"goodkind.io/mwan/internal/bgp"
	"goodkind.io/mwan/internal/config"
)

func newBGPSpeaker(cfg *config.Config, log *slog.Logger) (*bgp.Speaker, error) {
	if !cfg.BGP.Enabled {
		return nil, nil
	}
	bgpCfg := bgp.Config{
		Enabled:          true,
		ASN:              cfg.BGP.ASN,
		RouterID:         cfg.BGP.RouterID,
		NextHopV6:        cfg.BGP.NextHopV6,
		KeepaliveSeconds: cfg.BGP.KeepaliveSeconds,
		HoldSeconds:      cfg.BGP.HoldSeconds,
		ListenPort:       cfg.BGP.ListenPort,
		Announce: bgp.AnnounceConfig{
			IPv4: cfg.BGP.Announce.IPv4,
			IPv6: cfg.BGP.Announce.IPv6,
		},
		GracefulRestart: bgp.GracefulRestartConfig{
			Enabled:             cfg.BGP.GracefulRestart.Enabled,
			RestartTime:         cfg.BGP.GracefulRestart.RestartTime,
			NotificationEnabled: cfg.BGP.GracefulRestart.NotificationEnabled,
		},
	}
	for _, neighbor := range cfg.BGP.Neighbors {
		bgpCfg.Neighbors = append(bgpCfg.Neighbors, bgp.NeighborConfig{Address: neighbor.Address})
	}
	for _, neighbor := range cfg.BGP.NeighborsV6 {
		bgpCfg.NeighborsV6 = append(bgpCfg.NeighborsV6, bgp.NeighborConfig{Address: neighbor.Address})
	}
	for _, configuredRouter := range cfg.BGP.Routers {
		router, err := newBGPRouter(configuredRouter, log)
		if err != nil {
			return nil, err
		}
		bgpCfg.Routers = append(bgpCfg.Routers, router)
	}
	return bgp.New(bgpCfg, log), nil
}

func newBGPRouter(configuredRouter config.BGPRouter, log *slog.Logger) (bgp.Router, error) {
	router := bgp.Router{
		Name:          configuredRouter.Name,
		AddressV4:     configuredRouter.AddressV4,
		AddressV6:     configuredRouter.AddressV6,
		AllocationsV6: make([]netip.Prefix, 0, len(configuredRouter.AllocationsV6)),
	}
	for _, allocationText := range configuredRouter.AllocationsV6 {
		allocation, err := netip.ParsePrefix(allocationText)
		if err != nil {
			log.Error(
				"parse BGP router allocation failed",
				"error", err,
				"router", configuredRouter.Name,
				"allocation", allocationText,
			)
			return bgp.Router{}, fmt.Errorf(
				"parse BGP router %q allocation %q: %w",
				configuredRouter.Name,
				allocationText,
				err,
			)
		}
		router.AllocationsV6 = append(router.AllocationsV6, allocation)
	}
	return router, nil
}
