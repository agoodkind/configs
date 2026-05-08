package main

import (
	"context"
	"errors"
	"flag"
	"testing"
	"time"

	"goodkind.io/mwan/internal/opnsense/upgrade"
	"goodkind.io/mwan/internal/opnsense/validate"
)

// TestParseUpgradeFlagsAcceptsDeployID confirms the --deploy-id flag is
// registered on the upgrade subcommand and lands in upgrade.Options.
func TestParseUpgradeFlagsAcceptsDeployID(t *testing.T) {
	t.Parallel()
	args := []string{"--vmid", "102", "--deploy-id", "20260507-test"}
	f, err := parseUpgradeFlags("opnsense-upgrade prepare", args)
	if err != nil {
		t.Fatalf("parseUpgradeFlags: unexpected error: %v", err)
	}
	if f.deployID != "20260507-test" {
		t.Errorf("deployID = %q, want %q", f.deployID, "20260507-test")
	}
	opts := f.toOptions()
	if opts.DeployID != "20260507-test" {
		t.Errorf("opts.DeployID = %q, want %q", opts.DeployID, "20260507-test")
	}
}

// TestParseUpgradeFlagsAcceptsValidatorFlags exercises the validator
// transport flags introduced for the MWAN-160 wiring.
func TestParseUpgradeFlagsAcceptsValidatorFlags(t *testing.T) {
	t.Parallel()
	args := []string{
		"--vmid", "102",
		"--opnsense-ssh", "router",
		"--proxmox-ssh", "vault",
		"--api-key", "k",
		"--api-secret", "s",
		"--bgp-v4-neighbors", "10.0.0.1,10.0.0.2",
		"--mwan-opnsense-socket", "/run/mwan-opnsense.sock",
		"--settle-after-upgrade", "0s",
	}
	f, err := parseUpgradeFlags("opnsense-upgrade run", args)
	if err != nil {
		t.Fatalf("parseUpgradeFlags: unexpected error: %v", err)
	}
	if f.opnsenseSSHHost != "router" {
		t.Errorf("opnsenseSSHHost = %q", f.opnsenseSSHHost)
	}
	if f.proxmoxSSHHost != "vault" {
		t.Errorf("proxmoxSSHHost = %q", f.proxmoxSSHHost)
	}
	if f.apiKey != "k" || f.apiSecret != "s" {
		t.Errorf("api credentials not parsed: key=%q secret=%q", f.apiKey, f.apiSecret)
	}
	if f.bgpV4Neighbors != "10.0.0.1,10.0.0.2" {
		t.Errorf("bgpV4Neighbors = %q", f.bgpV4Neighbors)
	}
	if f.mwanSocket != "/run/mwan-opnsense.sock" {
		t.Errorf("mwanSocket = %q", f.mwanSocket)
	}
	if f.settleAfter != 0 {
		t.Errorf("settleAfter = %v, want 0", f.settleAfter)
	}
}

// TestRegisterCommonFlagsRegistersDeployID is a focused regression test
// on the flag set itself; without --deploy-id registered, the runbook
// commands fail at parse time.
func TestRegisterCommonFlagsRegistersDeployID(t *testing.T) {
	t.Parallel()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var f upgradeFlags
	registerCommonFlags(fs, &f)
	if fs.Lookup("deploy-id") == nil {
		t.Fatal("--deploy-id flag not registered on upgrade flag set")
	}
}

// TestNewValidatorAdapterReturnsNonNil confirms buildUpgradeDeps no
// longer leaves Validate stubbed nil for the upgrade subcommand.
func TestNewValidatorAdapterReturnsNonNil(t *testing.T) {
	t.Parallel()
	a := newValidatorAdapter(upgradeFlags{})
	if a == nil {
		t.Fatal("newValidatorAdapter returned nil")
	}
	// Compile-time check that the adapter satisfies the interface.
	var _ upgrade.Validator = a
}

// TestValidatorAdapterTranslatesPassResults asserts that the adapter
// invokes the runner with values derived from upgrade.ValidateContext
// and translates pass results into the upgrade CheckResult shape.
func TestValidatorAdapterTranslatesPassResults(t *testing.T) {
	t.Parallel()
	var captured validate.Config
	var capturedEnv validate.Env
	a := &validatorAdapter{
		flags: upgradeFlags{
			opnsenseSSHHost: "router",
			bgpV4Neighbors:  "10.0.0.1",
			settleAfter:     30 * time.Second,
		},
		runner: func(_ context.Context, cfg validate.Config, _ *validate.Baseline, env validate.Env) (*validate.Baseline, error) {
			captured = cfg
			capturedEnv = env
			return &validate.Baseline{
				SchemaVersion: validate.BaselineSchemaVersion,
				Results: []validate.Result{
					{CheckID: "qga_responsive", Outcome: validate.OutcomePass, ParsedValue: "ok"},
					{CheckID: "bgp_v4_neighbor_established", Outcome: validate.OutcomePass, ParsedValue: "Established"},
				},
			}, nil
		},
	}
	got, err := a.Validate(context.Background(), upgrade.ValidateContext{
		VMID:     "102",
		Target:   "26.1.7",
		StateDir: "/var/lib/mwan/upgrades",
		DeployID: "20260507-test",
	})
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if !got.AllPass {
		t.Errorf("AllPass = false, want true; checks=%+v", got.Checks)
	}
	if got.AnyFail {
		t.Errorf("AnyFail = true, want false")
	}
	if len(got.Checks) != 2 {
		t.Fatalf("len(Checks) = %d, want 2", len(got.Checks))
	}
	if got.Checks[0].Name != "qga_responsive" || !got.Checks[0].Pass {
		t.Errorf("Checks[0] = %+v", got.Checks[0])
	}
	if captured.VMID != 102 {
		t.Errorf("captured.VMID = %d, want 102", captured.VMID)
	}
	if captured.DeployID != "20260507-test" {
		t.Errorf("captured.DeployID = %q, want %q", captured.DeployID, "20260507-test")
	}
	if captured.StateDir != "/var/lib/mwan/upgrades" {
		t.Errorf("captured.StateDir = %q", captured.StateDir)
	}
	if len(captured.BGPv4Neighbors) != 1 || captured.BGPv4Neighbors[0] != "10.0.0.1" {
		t.Errorf("captured.BGPv4Neighbors = %v", captured.BGPv4Neighbors)
	}
	if captured.SettleAfterUpgrade != 30*time.Second {
		t.Errorf("captured.SettleAfterUpgrade = %v", captured.SettleAfterUpgrade)
	}
	execEnv, ok := capturedEnv.(*validate.ExecEnv)
	if !ok {
		t.Fatalf("env type = %T, want *validate.ExecEnv", capturedEnv)
	}
	if execEnv.OPNsenseSSHHost != "router" {
		t.Errorf("env.OPNsenseSSHHost = %q, want %q", execEnv.OPNsenseSSHHost, "router")
	}
}

// TestValidatorAdapterTranslatesFailResults asserts that fail and skip
// outcomes map to Pass=false in the upgrade CheckResult shape, and
// that the message text is preserved on the Note field.
func TestValidatorAdapterTranslatesFailResults(t *testing.T) {
	t.Parallel()
	a := &validatorAdapter{
		flags: upgradeFlags{},
		runner: func(_ context.Context, _ validate.Config, _ *validate.Baseline, _ validate.Env) (*validate.Baseline, error) {
			return &validate.Baseline{
				SchemaVersion: validate.BaselineSchemaVersion,
				Results: []validate.Result{
					{CheckID: "pf_enabled", Outcome: validate.OutcomePass, ParsedValue: "Enabled"},
					{CheckID: "bgp_v6_neighbor_established", Outcome: validate.OutcomeFail, Message: "neighbor not established"},
					{CheckID: "captiveportal_zones", Outcome: validate.OutcomeSkip, Message: "no zones in baseline"},
				},
			}, nil
		},
	}
	got, err := a.Validate(context.Background(), upgrade.ValidateContext{
		VMID:     "102",
		StateDir: "/tmp/state",
		DeployID: "d1",
	})
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if got.AllPass {
		t.Errorf("AllPass = true, want false")
	}
	if !got.AnyFail {
		t.Errorf("AnyFail = false, want true")
	}
	if !got.Partial {
		t.Errorf("Partial = false, want true")
	}
	failByName := map[string]upgrade.CheckResult{}
	for _, c := range got.Checks {
		failByName[c.Name] = c
	}
	if !failByName["pf_enabled"].Pass {
		t.Errorf("pf_enabled should pass: %+v", failByName["pf_enabled"])
	}
	if failByName["bgp_v6_neighbor_established"].Pass {
		t.Errorf("bgp_v6_neighbor_established should not pass")
	}
	if failByName["bgp_v6_neighbor_established"].Note == "" {
		t.Errorf("expected non-empty Note for failed check")
	}
	if failByName["captiveportal_zones"].Pass {
		t.Errorf("skipped check should not be Pass=true")
	}
}

// TestValidatorAdapterRejectsInvalidVMID confirms the adapter surfaces
// a typed parse error rather than passing zero through to validate.Run.
func TestValidatorAdapterRejectsInvalidVMID(t *testing.T) {
	t.Parallel()
	a := &validatorAdapter{
		flags: upgradeFlags{},
		runner: func(_ context.Context, _ validate.Config, _ *validate.Baseline, _ validate.Env) (*validate.Baseline, error) {
			t.Fatal("runner should not be invoked when vmid parse fails")
			return nil, nil
		},
	}
	_, err := a.Validate(context.Background(), upgrade.ValidateContext{VMID: "not-a-number"})
	if err == nil {
		t.Fatal("expected error for invalid vmid, got nil")
	}
}

// TestValidatorAdapterPropagatesRunnerError confirms a runner error is
// wrapped and returned without crashing on a nil baseline.
func TestValidatorAdapterPropagatesRunnerError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("env: ssh down")
	a := &validatorAdapter{
		flags: upgradeFlags{},
		runner: func(_ context.Context, _ validate.Config, _ *validate.Baseline, _ validate.Env) (*validate.Baseline, error) {
			return nil, wantErr
		},
	}
	_, err := a.Validate(context.Background(), upgrade.ValidateContext{VMID: "102"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want chain containing %v", err, wantErr)
	}
}

// TestBuildAPIAuthOnlyPopulatesWhenSet confirms the auth helper does
// not fabricate a BasicAuth pair when no credentials are supplied.
func TestBuildAPIAuthOnlyPopulatesWhenSet(t *testing.T) {
	t.Parallel()
	if buildAPIAuth(upgradeFlags{}) != nil {
		t.Errorf("expected nil auth when credentials unset")
	}
	auth := buildAPIAuth(upgradeFlags{apiKey: "k", apiSecret: "s"})
	if auth == nil {
		t.Fatal("expected non-nil auth when credentials present")
	}
	if auth.Username != "k" || auth.Password != "s" {
		t.Errorf("auth = %+v", auth)
	}
}
