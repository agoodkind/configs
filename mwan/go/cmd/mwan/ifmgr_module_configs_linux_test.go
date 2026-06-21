//go:build linux

package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodkind.io/mwan/internal/config"
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

func TestBuildWANRoutesConfig(t *testing.T) {
	t.Parallel()

	cfg, err := buildWANRoutesConfig(&config.IfMgrWANRoutesSection{
		InternalIface:   "vmbr250",
		OpnsenseWanLL:   "fe80::1",
		OpnsenseEdgeV6:  "3d06:bad:b01:201::1",
		InternalPrefix:  "3d06:bad:b01::/60",
		InternalNetV4:   "10.250.250.0/29",
		HealthStateFile: "/var/run/mwan-health.state",
		ShadowMode:      true,
		WAN: []config.IfMgrWANRoutesWANSection{
			{
				Name:       "att",
				Iface:      "att0",
				TableID:    100,
				FwMark:     1,
				FwMarkPrio: 100,
				FromPrio:   55,
				NptPrefix:  "3d06:bad:b01:1100::/56",
			},
			{
				Name:       "webpass",
				Iface:      "webpass0",
				TableID:    200,
				FwMark:     2,
				FwMarkPrio: 200,
				FromPrio:   56,
				NptPrefix:  "3d06:bad:b01:2200::/56",
				V4Source:   "203.0.113.2",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildWANRoutesConfig returned error: %v", err)
	}

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
				Name:       "att",
				Iface:      "att0",
				TableID:    100,
				FwMark:     1,
				FwMarkPrio: 100,
				FromPrio:   55,
				NptPrefix:  "3d06:bad:b01:1100::/56",
			},
			{
				Name:       "webpass",
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
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("buildWANRoutesConfig mismatch\ngot:  %#v\nwant: %#v", cfg, want)
	}
}

func TestBuildWANRoutesConfigNilSection(t *testing.T) {
	t.Parallel()

	cfg, err := buildWANRoutesConfig(nil)
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

	set, err := buildIfMgrModuleConfigs(modulesWithUnresolvableUIDRule(), "wan")
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

	_, err := buildIfMgrModuleConfigs(modulesWithUnresolvableUIDRule(), "oob")
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

	if _, err := buildIfMgrModuleConfigs(config.IfMgrModulesSection{}, "bogus"); err == nil {
		t.Fatal("buildIfMgrModuleConfigs with an unknown role must error")
	}
}
