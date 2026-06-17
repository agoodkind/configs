//go:build linux

package wanroutes

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

func TestDesiredState(t *testing.T) {
	t.Parallel()

	baseConfig := testConfig()
	baseGateways := testGateways()
	allHealthy := netif.HealthStates{
		wanNameATT:          netif.HealthStateHealthy,
		wanNameWebpass:      netif.HealthStateHealthy,
		wanNameMonkeybrains: netif.HealthStateHealthy,
	}

	cases := []struct {
		name       string
		cfg        Config
		gateways   gateways
		health     netif.HealthStates
		wantRules  []netif.DesiredRule
		wantRoutes []netif.RouteSpec
	}{
		{
			name:       "all WANs healthy with gateways",
			cfg:        baseConfig,
			gateways:   baseGateways,
			health:     allHealthy,
			wantRules:  allHealthyRules(baseConfig),
			wantRoutes: routesForGateways(baseConfig, baseGateways),
		},
		{
			name: "unhealthy WAN and missing gateway drop enabled rules",
			cfg:  baseConfig,
			gateways: gateways{
				wanNameATT:          baseGateways[wanNameATT],
				wanNameWebpass:      baseGateways[wanNameWebpass],
				wanNameMonkeybrains: {V4: "198.51.100.1"},
			},
			health: netif.HealthStates{
				wanNameATT:          netif.HealthStateHealthy,
				wanNameWebpass:      netif.HealthStateUnhealthy,
				wanNameMonkeybrains: netif.HealthStateHealthy,
			},
			wantRules: []netif.DesiredRule{
				fwmarkRule(familyV4, 100, 1, 100),
				fwmarkRule(familyV6, 100, 1, 100),
				fromRule(55, "3d06:bad:b01:1100::/56", 100),
				fwmarkRule(familyV4, 300, 3, 300),
			},
			wantRoutes: routesForGateways(baseConfig, gateways{
				wanNameATT:          baseGateways[wanNameATT],
				wanNameWebpass:      baseGateways[wanNameWebpass],
				wanNameMonkeybrains: {V4: "198.51.100.1"},
			}),
		},
		{
			name:     "both primaries unhealthy plus monkeybrains healthy adds fallback",
			cfg:      baseConfig,
			gateways: baseGateways,
			health: netif.HealthStates{
				wanNameATT:          netif.HealthStateUnhealthy,
				wanNameWebpass:      netif.HealthStateUnhealthy,
				wanNameMonkeybrains: netif.HealthStateHealthy,
			},
			wantRules: []netif.DesiredRule{
				fwmarkRule(familyV4, 300, 3, 300),
				fwmarkRule(familyV6, 300, 3, 300),
				fromRule(57, "3d06:bad:b01:3300::/56", 300),
				fallbackRule(familyV4, "vmbr250", 300),
				fallbackRule(familyV6, "vmbr250", 300),
			},
			wantRoutes: routesForGateways(baseConfig, baseGateways),
		},
		{
			name: "from-PD rule requires NPT prefix",
			cfg:  configWithoutWebpassNPT(baseConfig),
			gateways: gateways{
				wanNameATT:          baseGateways[wanNameATT],
				wanNameWebpass:      baseGateways[wanNameWebpass],
				wanNameMonkeybrains: baseGateways[wanNameMonkeybrains],
			},
			health: allHealthy,
			wantRules: []netif.DesiredRule{
				fwmarkRule(familyV4, 100, 1, 100),
				fwmarkRule(familyV6, 100, 1, 100),
				fromRule(55, "3d06:bad:b01:1100::/56", 100),
				fwmarkRule(familyV4, 200, 2, 200),
				fwmarkRule(familyV6, 200, 2, 200),
				fwmarkRule(familyV4, 300, 3, 300),
				fwmarkRule(familyV6, 300, 3, 300),
				fromRule(57, "3d06:bad:b01:3300::/56", 300),
			},
			wantRoutes: routesForGateways(configWithoutWebpassNPT(baseConfig), baseGateways),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotRules, gotRoutes := desiredState(tc.gateways, tc.health, tc.cfg)
			if !reflect.DeepEqual(gotRules, tc.wantRules) {
				t.Fatalf("rules mismatch\ngot:  %#v\nwant: %#v", gotRules, tc.wantRules)
			}
			if !reflect.DeepEqual(gotRoutes, tc.wantRoutes) {
				t.Fatalf("routes mismatch\ngot:  %#v\nwant: %#v", gotRoutes, tc.wantRoutes)
			}
		})
	}
}

func TestInitReturnsDisabledSentinelWhenWANsEmpty(t *testing.T) {
	t.Parallel()

	module, err := New(Config{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	initErr := module.Init(context.Background(), testEnv())
	if initErr == nil {
		t.Fatal("Init returned nil error for empty WANs, want ErrModuleDisabled")
	}
	if !errors.Is(initErr, ifmgr.ErrModuleDisabled) {
		t.Fatalf("Init returned err=%v, want errors.Is(err, ifmgr.ErrModuleDisabled)", initErr)
	}
}

func testConfig() Config {
	return Config{
		InternalIface:   "vmbr250",
		OpnsenseWanLL:   "fe80::1",
		OpnsenseEdgeV6:  "3d06:bad:b01:201::1",
		InternalPrefix:  "3d06:bad:b01::/60",
		InternalNetV4:   "10.250.250.0/29",
		HealthStateFile: "/run/mwan-health.state",
		WANs: []WAN{
			{
				Name:       wanNameATT,
				Iface:      "att0",
				TableID:    100,
				FwMark:     1,
				FwMarkPrio: 100,
				FromPrio:   55,
				NptPrefix:  "3d06:bad:b01:1100::/56",
			},
			{
				Name:       wanNameWebpass,
				Iface:      "webpass0",
				TableID:    200,
				FwMark:     2,
				FwMarkPrio: 200,
				FromPrio:   56,
				NptPrefix:  "3d06:bad:b01:2200::/56",
			},
			{
				Name:       wanNameMonkeybrains,
				Iface:      "mbrains0",
				TableID:    300,
				FwMark:     3,
				FwMarkPrio: 300,
				FromPrio:   57,
				NptPrefix:  "3d06:bad:b01:3300::/56",
			},
		},
	}
}

func testGateways() gateways {
	return gateways{
		wanNameATT: {
			V4: "192.0.2.1",
			V6: "fe80::a",
		},
		wanNameWebpass: {
			V4: "203.0.113.1",
			V6: "fe80::b",
		},
		wanNameMonkeybrains: {
			V4: "198.51.100.1",
			V6: "fe80::c",
		},
	}
}

func allHealthyRules(cfg Config) []netif.DesiredRule {
	return []netif.DesiredRule{
		fwmarkRule(familyV4, 100, 1, 100),
		fwmarkRule(familyV6, 100, 1, 100),
		fromRule(55, cfg.WANs[0].NptPrefix, 100),
		fwmarkRule(familyV4, 200, 2, 200),
		fwmarkRule(familyV6, 200, 2, 200),
		fromRule(56, cfg.WANs[1].NptPrefix, 200),
		fwmarkRule(familyV4, 300, 3, 300),
		fwmarkRule(familyV6, 300, 3, 300),
		fromRule(57, cfg.WANs[2].NptPrefix, 300),
	}
}

func routesForGateways(cfg Config, currentGateways gateways) []netif.RouteSpec {
	routes := make([]netif.RouteSpec, 0, len(cfg.WANs)*5+1)
	for _, wan := range cfg.WANs {
		wanGateways := currentGateways[wan.Name]
		if wanGateways.V4 != "" {
			routes = append(routes, route(familyV4, "default", wanGateways.V4, wan.Iface, wan.TableID, 0))
		}
		if wanGateways.V6 != "" {
			routes = append(routes, route(familyV6, "default", wanGateways.V6, wan.Iface, wan.TableID, 0))
		}
		routes = append(routes,
			route(familyV4, "10.250.250.0/29", "", "vmbr250", wan.TableID, 0),
			route(familyV6, "3d06:bad:b01:201::1/128", "", "vmbr250", wan.TableID, 0),
			route(familyV6, "3d06:bad:b01::/60", "fe80::1", "vmbr250", wan.TableID, 0),
		)
	}
	routes = append(routes, route(
		familyV6,
		"3d06:bad:b01::/60",
		"fe80::1",
		"vmbr250",
		unix.RT_TABLE_MAIN,
		mainInternalMetric,
	))
	return routes
}

func route(family string, dest string, via string, dev string, tableID int, metric int) netif.RouteSpec {
	return netif.RouteSpec{
		Family:  family,
		Dest:    dest,
		Via:     via,
		Dev:     dev,
		TableID: tableID,
		Metric:  metric,
	}
}

func fwmarkRule(family string, priority int, mark uint32, tableID int) netif.DesiredRule {
	return netif.DesiredRule{
		Family:   family,
		Priority: priority,
		Mark:     mark,
		TableID:  tableID,
	}
}

func fromRule(priority int, from string, tableID int) netif.DesiredRule {
	return netif.DesiredRule{
		Family:   familyV6,
		Priority: priority,
		From:     from,
		TableID:  tableID,
	}
}

func fallbackRule(family string, iifName string, tableID int) netif.DesiredRule {
	return netif.DesiredRule{
		Family:   family,
		Priority: fallbackPriority,
		IifName:  iifName,
		TableID:  tableID,
	}
}

func configWithoutWebpassNPT(cfg Config) Config {
	cfg.WANs = append([]WAN(nil), cfg.WANs...)
	cfg.WANs[1].NptPrefix = ""
	return cfg
}

func testEnv() *ifmgr.Env {
	return &ifmgr.Env{
		Iface: "vmbr250",
		Log: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})),
		Alerts: ifmgr.NewAlertManager(
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			ifmgr.AlertConfig{},
		),
	}
}
