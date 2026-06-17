//go:build linux

package main

import (
	"errors"
	"reflect"
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
