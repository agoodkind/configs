package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"goodkind.io/mwan/internal/opnsense/upgrade"
	"goodkind.io/mwan/internal/opnsense/validate"
)

// validateRunner is the concrete validate.Run shape, factored into a
// function variable so tests can substitute a stub. Production code
// uses validate.Run directly.
type validateRunner func(
	ctx context.Context,
	cfg validate.Config,
	baseline *validate.Baseline,
	env validate.Env,
) (*validate.Baseline, error)

// validatorAdapter implements upgrade.Validator by translating the
// upgrade package's ValidateContext into the MWAN-153 validate.Run
// surface. The adapter holds the operator-supplied transport flags
// (SSH hosts, API auth, BGP neighbors) so the upgrade subcommand can
// drive the same matrix as `mwan opnsense-validate`.
//
// Translation map:
//   - upgrade.ValidateContext.VMID (string) -> validate.Config.VMID (int)
//   - upgrade.ValidateContext.{StateDir,DeployID} -> validate.Config.{StateDir,DeployID}
//   - validate.Baseline.Results []validate.Result -> upgrade.ValidationResult.Checks []upgrade.CheckResult
type validatorAdapter struct {
	flags  upgradeFlags
	runner validateRunner
}

// newValidatorAdapter constructs the production adapter wired to
// validate.Run.
func newValidatorAdapter(f upgradeFlags) *validatorAdapter {
	return &validatorAdapter{flags: f, runner: validate.Run}
}

// Validate satisfies upgrade.Validator. It builds a validate.Config
// and validate.Env from the adapter's flags, invokes the runner, and
// translates the resulting Baseline into a ValidationResult that the
// upgrade state machine can consume.
func (a *validatorAdapter) Validate(
	ctx context.Context,
	vctx upgrade.ValidateContext,
) (upgrade.ValidationResult, error) {
	vmidInt, err := strconv.Atoi(vctx.VMID)
	if err != nil {
		slog.ErrorContext(ctx, "validatorAdapter: parse vmid",
			"err", err, "vmid", vctx.VMID)
		return emptyUpgradeValidation(),
			fmt.Errorf("validatorAdapter: parse vmid %q: %w", vctx.VMID, err)
	}

	cfg := validate.Config{
		VMID:                   vmidInt,
		DeployID:               vctx.DeployID,
		StateDir:               vctx.StateDir,
		BGPv4Neighbors:         splitNonEmpty(a.flags.bgpV4Neighbors),
		BGPv6Neighbors:         splitNonEmpty(a.flags.bgpV6Neighbors),
		OPNsenseLAN:            a.flags.opnsenseLAN,
		MWANOpnsenseSocket:     a.flags.mwanSocket,
		MWANOpnsenseHostSocket: a.flags.mwanHostSocket,
		APIAuth:                buildAPIAuth(a.flags),
		SettleAfterUpgrade:     a.flags.settleAfter,
		SeverityFilter:         "",
	}

	env, err := defaultEnvFactory().build(envTransportConfig{
		Transport:           a.flags.envTransport,
		GRPCTarget:          a.flags.envGRPCTarget,
		OPNsenseSSHHost:     a.flags.opnsenseSSHHost,
		OPNsenseSSHJumpHost: a.flags.opnsenseJumpHost,
		ProxmoxSSHHost:      a.flags.proxmoxSSHHost,
		LANClientSSHHost:    a.flags.lanClientSSH,
		OPNsenseAddr:        a.flags.opnsenseAddr,
	})
	if err != nil {
		slog.ErrorContext(ctx, "validatorAdapter: build env",
			"err", err, "transport", string(a.flags.envTransport))
		return emptyUpgradeValidation(),
			fmt.Errorf("validatorAdapter: build env: %w", err)
	}

	baseline, err := a.runner(ctx, cfg, nil, env)
	if err != nil {
		slog.ErrorContext(ctx, "validatorAdapter: validate.Run",
			"err", err, "vmid", vctx.VMID, "deploy_id", vctx.DeployID)
		return emptyUpgradeValidation(),
			fmt.Errorf("validatorAdapter: validate.Run: %w", err)
	}
	if baseline == nil {
		err := fmt.Errorf("validatorAdapter: validate.Run returned nil baseline")
		slog.ErrorContext(ctx, "validatorAdapter: nil baseline",
			"err", err, "vmid", vctx.VMID)
		return emptyUpgradeValidation(), err
	}
	return upgrade.AggregateChecks(translateResults(baseline.Results)), nil
}

// buildAPIAuth materialises the BasicAuth pair only when at least one
// of the credential fields is populated. Empty strings produce a nil
// pointer so the validate package's nil-auth code path runs.
func buildAPIAuth(f upgradeFlags) *validate.BasicAuth {
	if f.apiKey == "" && f.apiSecret == "" {
		return nil
	}
	return &validate.BasicAuth{Username: f.apiKey, Password: f.apiSecret}
}

// translateResults maps validate.Result records onto the simpler
// upgrade.CheckResult shape consumed by the upgrade state machine.
// Pass=true iff the validate Outcome is OutcomePass; OutcomeSkip and
// OutcomeError are surfaced as Pass=false with a Note describing the
// reason. The upgrade phase selector treats any non-pass as a failed
// check, which is the documented behaviour in MWAN-152 design 5.
func translateResults(results []validate.Result) []upgrade.CheckResult {
	checks := make([]upgrade.CheckResult, 0, len(results))
	for _, r := range results {
		checks = append(checks, upgrade.CheckResult{
			Name: r.CheckID,
			Pass: r.Outcome == validate.OutcomePass,
			Note: noteForResult(r),
		})
	}
	return checks
}

// noteForResult composes a short human-facing note for the upgrade
// check record. Pass results carry the parsed value; non-pass results
// carry the message and outcome so the email body and audit log have
// enough context.
func noteForResult(r validate.Result) string {
	if r.Outcome == validate.OutcomePass {
		return r.ParsedValue
	}
	if r.Message == "" {
		return string(r.Outcome)
	}
	return fmt.Sprintf("%s: %s", r.Outcome, r.Message)
}

// emptyUpgradeValidation returns a fully-zero ValidationResult so
// adapter error paths return a populated value (exhaustruct).
func emptyUpgradeValidation() upgrade.ValidationResult {
	return upgrade.ValidationResult{Checks: nil, AllPass: false, AnyFail: false, Partial: false}
}
