//go:build linux

package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/ifmgr"
	npt "goodkind.io/mwan/internal/ifmgr/modules/npt"
	wanroutes "goodkind.io/mwan/internal/ifmgr/modules/wanroutes"
)

func TestBuildPolicyRuleUIDRangeUsesStaticRange(t *testing.T) {
	t.Parallel()

	rule := config.IfMgrPolicyRuleSection{UIDRange: "997-997"}
	got, err := buildPolicyRuleUIDRange(rule, func(string) (string, error) {
		return "", errors.New("lookup should not run")
	})
	if err != nil {
		t.Fatalf("buildPolicyRuleUIDRange returned error: %v", err)
	}
	if got != "997-997" {
		t.Fatalf("buildPolicyRuleUIDRange returned %q, want %q", got, "997-997")
	}
}

func TestBuildPolicyRuleUIDRangeUsesUser(t *testing.T) {
	t.Parallel()

	rule := config.IfMgrPolicyRuleSection{UIDUser: "cloudflared-oob"}
	got, err := buildPolicyRuleUIDRange(rule, func(username string) (string, error) {
		if username != "cloudflared-oob" {
			t.Fatalf("lookup username = %q, want %q", username, "cloudflared-oob")
		}
		return "997", nil
	})
	if err != nil {
		t.Fatalf("buildPolicyRuleUIDRange returned error: %v", err)
	}
	if got != "997-997" {
		t.Fatalf("buildPolicyRuleUIDRange returned %q, want %q", got, "997-997")
	}
}

func TestBuildPolicyRuleUIDRangeRejectsConflictingSelectors(t *testing.T) {
	t.Parallel()

	rule := config.IfMgrPolicyRuleSection{
		UIDRange: "997-997",
		UIDUser:  "cloudflared-oob",
	}
	_, err := buildPolicyRuleUIDRange(rule, func(string) (string, error) {
		return "997", nil
	})
	if err == nil {
		t.Fatal("buildPolicyRuleUIDRange returned nil error")
	}
}

func TestBuildPolicyRuleUIDRangeRejectsInvalidUID(t *testing.T) {
	t.Parallel()

	rule := config.IfMgrPolicyRuleSection{UIDUser: "cloudflared-oob"}
	_, err := buildPolicyRuleUIDRange(rule, func(string) (string, error) {
		return "not-a-number", nil
	})
	if err == nil {
		t.Fatal("buildPolicyRuleUIDRange returned nil error")
	}
}

func TestBuildHostIPv6PolicyConfig(t *testing.T) {
	t.Parallel()

	cfg, err := buildHostIPv6PolicyConfig(&config.IfMgrHostIPv6PolicySection{
		MissingIfaceGracePeriod: "3m",
		Interface: []config.IfMgrHostIPv6PolicyIfaceSection{
			{
				Name:             "vmbr0",
				AcceptRA:         2,
				AutoConf:         true,
				AcceptRADefRtr:   true,
				SolicitRA:        true,
				CleanupRADefault: false,
			},
			{
				Name:             "vmbr4",
				AcceptRA:         0,
				AutoConf:         false,
				AcceptRADefRtr:   false,
				SolicitRA:        false,
				CleanupRADefault: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("buildHostIPv6PolicyConfig returned error: %v", err)
	}
	if got := cfg.MissingIfaceGracePeriod; got != 3*time.Minute {
		t.Fatalf("MissingIfaceGracePeriod = %s, want %s", got, 3*time.Minute)
	}
	if len(cfg.Policies) != 2 {
		t.Fatalf("policy count = %d, want 2", len(cfg.Policies))
	}
	if got := cfg.Policies[0].Name; got != "vmbr0" {
		t.Fatalf("first policy iface = %q, want %q", got, "vmbr0")
	}
	if got := cfg.Policies[1].CleanupRADefault; !got {
		t.Fatal("second policy should clean denied RA defaults")
	}
}

// sharedWANForTest is the [ifmgr] shared per-WAN foundation both module builders
// read: the WAN map ([ifmgr.wan.<name>]) with each WAN's full config (iface plus
// the routing slots wan_routes owns), plus the shared edge addresses and internal
// prefix on [ifmgr] itself. One home per WAN; modules read the fields they need.
func sharedWANForTest() config.IfMgrSection {
	return config.IfMgrSection{
		InternalPrefix: "3d06:bad:b01::/60",
		OpnsenseEdgeV6: "3d06:bad:b01:201::1",
		MwanbrEdgeV6:   "3d06:bad:b01:200::1",
		WAN: map[string]config.IfMgrWANEntry{
			"att": {
				Iface:      "att0",
				TableID:    100,
				FwMark:     1,
				FwMarkPrio: 100,
				FromPrio:   55,
				NptPrefix:  "3d06:bad:b01:1100::/56",
			},
			"webpass": {
				Iface:      "webpass0",
				TableID:    200,
				FwMark:     2,
				FwMarkPrio: 200,
				FromPrio:   56,
				NptPrefix:  "3d06:bad:b01:2200::/56",
				V4Source:   "203.0.113.2",
			},
		},
	}
}

// ifmgrForTest is sharedWANForTest with the given modules attached, for the
// role-scoped buildIfMgrModuleConfigs tests.
func ifmgrForTest(mods config.IfMgrModulesSection) config.IfMgrSection {
	s := sharedWANForTest()
	s.Modules = mods
	return s
}

// TestBuildWANRefs pins that the generic per-WAN builder turns the shared
// [ifmgr.wan] map into the sorted per-WAN list (identity plus routing fields)
// and the shared prefixes every module builder reuses.
func TestBuildWANRefs(t *testing.T) {
	t.Parallel()

	got := buildWANRefs(sharedWANForTest())
	want := sharedWANInputs{
		InternalPrefix: "3d06:bad:b01::/60",
		OpnsenseEdgeV6: "3d06:bad:b01:201::1",
		MwanbrEdgeV6:   "3d06:bad:b01:200::1",
		WANs: []sharedWAN{
			{
				WANRef:     ifmgr.WANRef{Name: "att", Iface: "att0"},
				TableID:    100,
				FwMark:     1,
				FwMarkPrio: 100,
				FromPrio:   55,
				NptPrefix:  "3d06:bad:b01:1100::/56",
			},
			{
				WANRef:     ifmgr.WANRef{Name: "webpass", Iface: "webpass0"},
				TableID:    200,
				FwMark:     2,
				FwMarkPrio: 200,
				FromPrio:   56,
				NptPrefix:  "3d06:bad:b01:2200::/56",
				V4Source:   "203.0.113.2",
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildWANRefs mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestBuildWANRoutesConfig(t *testing.T) {
	t.Parallel()

	shared := buildWANRefs(sharedWANForTest())
	cfg, err := buildWANRoutesConfig(shared, &config.IfMgrWANRoutesSection{
		InternalIface:   "vmbr250",
		OpnsenseWanLL:   "fe80::1",
		InternalNetV4:   "10.250.250.0/29",
		HealthStateFile: "/var/run/mwan-health.state",
		ShadowMode:      true,
	})
	if err != nil {
		t.Fatalf("buildWANRoutesConfig returned error: %v", err)
	}

	// The per-WAN routing data comes from the shared [ifmgr.wan.<name>] map
	// (sharedWANForTest), not a wan_routes-local list.
	want := wanroutes.Config{
		InternalIface:   "vmbr250",
		OpnsenseWanLL:   "fe80::1",
		OpnsenseEdgeV6:  "3d06:bad:b01:201::1",
		InternalPrefix:  "3d06:bad:b01::/60",
		InternalNetV4:   "10.250.250.0/29",
		HealthStateFile: "/var/run/mwan-health.state",
		ShadowMode:      true,
		WANs: []wanroutes.WAN{
			{
				WANRef:     ifmgr.WANRef{Name: "att", Iface: "att0"},
				TableID:    100,
				FwMark:     1,
				FwMarkPrio: 100,
				FromPrio:   55,
				NptPrefix:  "3d06:bad:b01:1100::/56",
			},
			{
				WANRef:     ifmgr.WANRef{Name: "webpass", Iface: "webpass0"},
				TableID:    200,
				FwMark:     2,
				FwMarkPrio: 200,
				FromPrio:   56,
				NptPrefix:  "3d06:bad:b01:2200::/56",
				V4Source:   "203.0.113.2",
			},
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("buildWANRoutesConfig mismatch\ngot:  %#v\nwant: %#v", cfg, want)
	}
}

func TestBuildWANRoutesConfigNilSection(t *testing.T) {
	t.Parallel()

	cfg, err := buildWANRoutesConfig(buildWANRefs(sharedWANForTest()), nil)
	if err != nil {
		t.Fatalf("buildWANRoutesConfig returned error: %v", err)
	}
	if !reflect.DeepEqual(cfg, wanroutes.Config{}) {
		t.Fatalf("buildWANRoutesConfig nil = %#v, want zero Config", cfg)
	}
}

// modulesWithUnresolvableUIDRule is a [ifmgr.modules] section that carries a
// policy_rules rule referencing a user that does not exist on the build host,
// plus a wan_routes section. It models the production MWAN VM config, where the
// shared config.toml carries an oob policy_rules rule (cloudflared-oob, a
// hypervisor-host user) even though the VM only runs the wan role.
func modulesWithUnresolvableUIDRule() config.IfMgrModulesSection {
	return config.IfMgrModulesSection{
		PolicyRules: &config.IfMgrPolicyRulesSection{
			Rule: []config.IfMgrPolicyRuleSection{
				{
					Family:   "inet6",
					Priority: 5,
					UIDUser:  "mwan-test-no-such-user",
					Table:    "oob",
					TableID:  500,
				},
			},
		},
		WANRoutes: &config.IfMgrWANRoutesSection{InternalIface: "enmwanbr0"},
	}
}

// TestBuildIfMgrModuleConfigsWANRoleSkipsPolicyRules is the regression test for
// the mwan-ifmgr@wan crash-loop. The wan role must build only wan_routes, so it
// never resolves the policy_rules uid_user (which would fail on a host lacking
// that user) even when the shared config carries that rule.
func TestBuildIfMgrModuleConfigsWANRoleSkipsPolicyRules(t *testing.T) {
	t.Parallel()

	set, err := buildIfMgrModuleConfigs(ifmgrForTest(modulesWithUnresolvableUIDRule()), "wan")
	if err != nil {
		t.Fatalf("buildIfMgrModuleConfigs(wan) returned error: %v", err)
	}
	if _, ok := set["policy_rules"]; ok {
		t.Fatal("wan role must not build a policy_rules config")
	}
	if _, ok := set["wan_routes"]; !ok {
		t.Fatal("wan role must build a wan_routes config")
	}
}

// TestBuildIfMgrModuleConfigsOOBRoleBuildsPolicyRules pins that the oob role
// does build policy_rules (and surfaces the uid lookup failure), so the
// role-scoped build does not silently drop a module the role actually runs.
func TestBuildIfMgrModuleConfigsOOBRoleBuildsPolicyRules(t *testing.T) {
	t.Parallel()

	_, err := buildIfMgrModuleConfigs(ifmgrForTest(modulesWithUnresolvableUIDRule()), "oob")
	if err == nil {
		t.Fatal("oob role must build policy_rules and surface the uid lookup failure")
	}
	if !strings.Contains(err.Error(), "policy_rules") {
		t.Fatalf("oob build error = %q, want it to mention policy_rules", err)
	}
}

// TestBuildIfMgrModuleConfigsUnknownRole confirms an unknown role is rejected
// rather than silently producing an empty config set.
func TestBuildIfMgrModuleConfigsUnknownRole(t *testing.T) {
	t.Parallel()

	if _, err := buildIfMgrModuleConfigs(config.IfMgrSection{}, "bogus"); err == nil {
		t.Fatal("buildIfMgrModuleConfigs with an unknown role must error")
	}
}

// TestBuildNPTConfig pins that the npt builder joins the shared [ifmgr.wan]
// prefixes and WAN identity list with the npt section's shadow toggle. This is
// what makes MwanbrEdgeV6 a real consumer of the shared field.
func TestBuildNPTConfig(t *testing.T) {
	t.Parallel()

	shared := buildWANRefs(sharedWANForTest())
	cfg := buildNPTConfig(shared, &config.IfMgrNPTSection{ShadowMode: true})

	want := npt.Config{
		ShadowMode:     true,
		InternalPrefix: "3d06:bad:b01::/60",
		OpnsenseEdgeV6: "3d06:bad:b01:201::1",
		MwanbrEdgeV6:   "3d06:bad:b01:200::1",
		WANs: []ifmgr.WANRef{
			{Name: "att", Iface: "att0"},
			{Name: "webpass", Iface: "webpass0"},
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("buildNPTConfig mismatch\ngot:  %#v\nwant: %#v", cfg, want)
	}
}

// TestBuildNPTConfigNilSection checks a nil npt section still yields the shared
// prefixes and WAN list with shadow off, so the module builds even when only
// [ifmgr.wan] is present.
func TestBuildNPTConfigNilSection(t *testing.T) {
	t.Parallel()

	cfg := buildNPTConfig(buildWANRefs(sharedWANForTest()), nil)
	if cfg.ShadowMode {
		t.Fatal("nil npt section must default ShadowMode to false")
	}
	if cfg.MwanbrEdgeV6 != "3d06:bad:b01:200::1" {
		t.Fatalf("MwanbrEdgeV6 = %q, want the shared value", cfg.MwanbrEdgeV6)
	}
	if len(cfg.WANs) != 2 {
		t.Fatalf("WAN count = %d, want 2 from the shared list", len(cfg.WANs))
	}
}

// TestBuildIfMgrModuleConfigsWANRoleBuildsBoth confirms the wan role now yields
// both the wan_routes and npt module configs from one shared config.
func TestBuildIfMgrModuleConfigsWANRoleBuildsBoth(t *testing.T) {
	t.Parallel()

	modules := config.IfMgrModulesSection{
		WANRoutes: &config.IfMgrWANRoutesSection{InternalIface: "enmwanbr0"},
		NPT:       &config.IfMgrNPTSection{ShadowMode: true},
	}
	set, err := buildIfMgrModuleConfigs(ifmgrForTest(modules), "wan")
	if err != nil {
		t.Fatalf("buildIfMgrModuleConfigs(wan) returned error: %v", err)
	}
	if _, ok := set["wan_routes"]; !ok {
		t.Fatal("wan role must build a wan_routes config")
	}
	nptCfg, ok := set["npt"]
	if !ok {
		t.Fatal("wan role must build an npt config")
	}
	if _, ok := nptCfg.(npt.Config); !ok {
		t.Fatalf("npt config type = %T, want npt.Config", nptCfg)
	}
}

// TestIfMgrWANConfigRoundTrips parses a config.toml snippet exactly as the
// template renders it (the shared prefixes on [ifmgr], keyed [ifmgr.wan.<name>]
// tables carrying each WAN's full config, and the module-wide
// [ifmgr.modules.wan_routes] scalars) and drives it through
// buildIfMgrModuleConfigs. A render-vs-schema mismatch that the struct-built
// fixtures cannot catch (for example the keyed WAN map failing to populate,
// which crash-looped mwan-ifmgr@wan with "iface is required") fails here instead
// of in production.
func TestIfMgrWANConfigRoundTrips(t *testing.T) {
	t.Parallel()

	const configTOML = `
[ifmgr]
role = "wan"
internal_prefix = "3d06:bad:b01:210::/60"
opnsense_edge_v6 = "3d06:bad:b01:201::2"
mwanbr_edge_v6 = "3d06:bad:b01:201::3"

[ifmgr.wan.att]
iface = "enatt0"
table_id = 100
fw_mark = 1
fw_mark_prio = 100
from_prio = 55
npt_prefix = "3d06:bad:b01:2300::/60"

[ifmgr.wan.webpass]
iface = "enwebpass0"
table_id = 200
fw_mark = 2
fw_mark_prio = 200
from_prio = 56
npt_prefix = "3d06:bad:b01:2200::/60"
v4_source = "10.240.204.2"

[ifmgr.modules.wan_routes]
internal_iface = "enmwanbr0"
shadow_mode = false

[ifmgr.modules.npt]
shadow_mode = true
`
	var cfg config.Config
	if err := toml.Unmarshal([]byte(configTOML), &cfg); err != nil {
		t.Fatalf("toml.Unmarshal: %v", err)
	}
	// The keyed [ifmgr.wan.<name>] tables must populate the WAN map with each
	// WAN's full config, and the shared prefixes must land on [ifmgr] itself.
	if got := len(cfg.IfMgr.WAN); got != 2 {
		t.Fatalf("[ifmgr.wan] map size = %d, want 2 (render/schema mismatch)", got)
	}
	if got := cfg.IfMgr.WAN["att"].Iface; got != "enatt0" {
		t.Fatalf("cfg.IfMgr.WAN[att].Iface = %q, want enatt0", got)
	}
	if got := cfg.IfMgr.WAN["att"].TableID; got != 100 {
		t.Fatalf("cfg.IfMgr.WAN[att].TableID = %d, want 100 (routing field did not fold in)", got)
	}
	if cfg.IfMgr.InternalPrefix != "3d06:bad:b01:210::/60" {
		t.Fatalf("internal_prefix did not parse onto [ifmgr]: %q", cfg.IfMgr.InternalPrefix)
	}

	set, err := buildIfMgrModuleConfigs(cfg.IfMgr, "wan")
	if err != nil {
		t.Fatalf("buildIfMgrModuleConfigs(wan) from parsed config: %v", err)
	}
	wr, ok := set["wan_routes"].(wanroutes.Config)
	if !ok {
		t.Fatalf("wan_routes config missing or wrong type: %T", set["wan_routes"])
	}
	byName := map[string]wanroutes.WAN{}
	for _, w := range wr.WANs {
		byName[w.Name] = w
	}
	if byName["att"].Iface != "enatt0" || byName["webpass"].Iface != "enwebpass0" {
		t.Fatalf("wan_routes ifaces did not resolve from [ifmgr.wan]: %#v", byName)
	}
	if byName["webpass"].V4Source != "10.240.204.2" || byName["att"].TableID != 100 {
		t.Fatalf("wan_routes routing fields did not resolve from [ifmgr.wan]: %#v", byName)
	}
	if _, ok := set["npt"]; !ok {
		t.Fatal("wan role must build an npt config from the round-tripped config")
	}
}
