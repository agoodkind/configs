//go:build linux

package main

import (
	"net/netip"
	"testing"

	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/ifmgr/modules/health"
	"goodkind.io/mwan/internal/ifmgr/modules/npt"
	"goodkind.io/mwan/internal/ifmgr/modules/wanroutes"
	"goodkind.io/mwan/internal/wanconfig"
)

// wanconfigTestModuleConfigs mirrors what buildIfMgrModuleConfigs produces
// for the wan role from the rendered testbed config: three WANs on
// wan.routes, health probing all three, and the shared internal prefix on
// npt.
func wanconfigTestModuleConfigs() ifmgr.ModuleConfigSet {
	wans := []wanroutes.WAN{
		{
			WANRef: ifmgr.WANRef{Name: "att", Iface: "enatt0.3242"}, TableID: 100,
			FwMark: 1, FwMarkPrio: 10, FromPrio: 20, NptPrefix: "2001:db8:a::/60", V4Source: "",
		},
		{
			WANRef: ifmgr.WANRef{Name: "monkeybrains", Iface: "enmbrains0"}, TableID: 300,
			FwMark: 3, FwMarkPrio: 12, FromPrio: 22, NptPrefix: "", V4Source: "",
		},
		{
			WANRef: ifmgr.WANRef{Name: "webpass", Iface: "enwebpass0"}, TableID: 200,
			FwMark: 2, FwMarkPrio: 11, FromPrio: 21, NptPrefix: "2001:db8:b::/60", V4Source: "192.0.2.2",
		},
	}
	routesCfg := wanroutes.Config{
		InternalIface:   "eninternal0",
		OpnsenseEdgeV6:  "2001:db8:fe::2",
		InternalNetV4:   "192.0.2.0/29",
		HealthStateFile: "/run/health",
		WANs:            wans,
	}
	healthCfg := health.Config{}
	for _, wan := range wans {
		probedWAN := health.WAN{}
		probedWAN.WANRef = wan.WANRef
		healthCfg.WANs = append(healthCfg.WANs, probedWAN)
	}
	nptCfg := npt.Config{
		InternalPrefix: "3d06:bad:b01:210::/60",
		OpnsenseEdgeV6: "2001:db8:fe::2",
		MwanbrEdgeV6:   "2001:db8:fe::3",
		WANs:           []ifmgr.WANRef{},
	}
	return ifmgr.ModuleConfigSet{
		"wan.routes": routesCfg,
		"npt":        nptCfg,
		"health":     healthCfg,
	}
}

// TestGatewayFromModuleConfigs_ProjectsTheWANRole pins the projection the
// daemon publishes: every WAN becomes a member with the router's tier, a
// probe policy named after it, and the translation pair joined from the npt
// internal prefix and its own external prefix.
func TestGatewayFromModuleConfigs_ProjectsTheWANRole(t *testing.T) {
	t.Parallel()
	gateway, ok, err := gatewayFromModuleConfigs(wanconfigTestModuleConfigs())
	if err != nil {
		t.Fatalf("gatewayFromModuleConfigs: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want a gateway")
	}
	if gateway.InternalIface != "eninternal0" {
		t.Fatalf("InternalIface = %q", gateway.InternalIface)
	}

	internal := netip.MustParsePrefix("3d06:bad:b01:210::/60")
	want := []wanconfig.Member{
		{
			Name: "att", Iface: "enatt0.3242", Tier: 0, ProbePolicy: "att",
			NPTInternal: internal, NPTExternal: netip.MustParsePrefix("2001:db8:a::/60"),
		},
		{
			Name: "monkeybrains", Iface: "enmbrains0", Tier: 1, ProbePolicy: "monkeybrains",
			NPTInternal: netip.Prefix{}, NPTExternal: netip.Prefix{},
		},
		{
			Name: "webpass", Iface: "enwebpass0", Tier: 0, ProbePolicy: "webpass",
			NPTInternal: internal, NPTExternal: netip.MustParsePrefix("2001:db8:b::/60"),
		},
	}
	if len(gateway.Members) != len(want) {
		t.Fatalf("members = %d, want %d", len(gateway.Members), len(want))
	}
	for i, member := range gateway.Members {
		if member != want[i] {
			t.Fatalf("member[%d] = %+v, want %+v", i, member, want[i])
		}
	}

	// The projection must feed the tree builder without further shaping.
	if _, err := wanconfig.ConfigItems(gateway); err != nil {
		t.Fatalf("ConfigItems on the projected gateway: %v", err)
	}
}

// TestGatewayFromModuleConfigs_QuietOutsideTheWANRole pins that a role with
// no wan.routes config (every role but wan) yields no gateway and no error,
// so enabling the publish gate on such a host is a logged no-op.
func TestGatewayFromModuleConfigs_QuietOutsideTheWANRole(t *testing.T) {
	t.Parallel()
	_, ok, err := gatewayFromModuleConfigs(ifmgr.ModuleConfigSet{})
	if err != nil {
		t.Fatalf("gatewayFromModuleConfigs: %v", err)
	}
	if ok {
		t.Fatal("ok = true, want no gateway")
	}
}

// TestGatewayFromModuleConfigs_UnprobedMemberHasNoPolicy pins that a WAN
// absent from the health config gets no probe-policy value.
func TestGatewayFromModuleConfigs_UnprobedMemberHasNoPolicy(t *testing.T) {
	t.Parallel()
	configs := wanconfigTestModuleConfigs()
	configs["health"] = health.Config{}

	gateway, ok, err := gatewayFromModuleConfigs(configs)
	if err != nil || !ok {
		t.Fatalf("gatewayFromModuleConfigs: ok=%v err=%v", ok, err)
	}
	for _, member := range gateway.Members {
		if member.ProbePolicy != "" {
			t.Fatalf("member %s ProbePolicy = %q, want empty", member.Name, member.ProbePolicy)
		}
	}
}

// TestGatewayFromModuleConfigs_RejectsAnUnparsableTranslationPrefix pins
// the loud failure: a malformed prefix in the loaded config returns an
// error rather than a member silently missing its translation instance.
func TestGatewayFromModuleConfigs_RejectsAnUnparsableTranslationPrefix(t *testing.T) {
	t.Parallel()
	configs := wanconfigTestModuleConfigs()
	routesCfg, isRoutes := configs["wan.routes"].(wanroutes.Config)
	if !isRoutes {
		t.Fatal("wan.routes config missing from fixture")
	}
	routesCfg.WANs[0].NptPrefix = "not-a-prefix"
	configs["wan.routes"] = routesCfg

	_, _, err := gatewayFromModuleConfigs(configs)
	if err == nil {
		t.Fatal("err = nil, want a parse error")
	}
}
