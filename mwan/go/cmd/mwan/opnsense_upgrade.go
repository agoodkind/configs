package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/notify"
	"goodkind.io/mwan/internal/opnsense"
	"goodkind.io/mwan/internal/opnsense/upgrade"
	"goodkind.io/mwan/internal/opnsense/validate"
	"goodkind.io/mwan/internal/ops"
)

// upgradePhase enumerates `mwan opnsense upgrade <phase>` actions.
type upgradePhase string

const (
	upgradePhasePrepare  upgradePhase = "prepare"
	upgradePhaseExecute  upgradePhase = "execute"
	upgradePhaseValidate upgradePhase = "validate"
	upgradePhaseRollback upgradePhase = "rollback"
	upgradePhaseCommit   upgradePhase = "commit"
	upgradePhaseRun      upgradePhase = "run"
	upgradePhaseGC       upgradePhase = "gc"
	upgradePhaseReset    upgradePhase = "reset"
)

func upgradeUsage(out *os.File) {
	fmt.Fprintln(out, "usage: mwan opnsense upgrade <phase>")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Phases: prepare, execute, validate, rollback, commit, run, gc, reset")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Every input comes from [opnsense.upgrade] in /etc/mwan/config.toml.")
}

func runOPNsenseUpgradeCmd(args []string) int {
	if len(args) < 1 {
		upgradeUsage(os.Stderr)
		return 2
	}
	verb := upgradePhase(args[0])
	rest := args[1:]
	if len(rest) > 0 && (rest[0] == "-h" || rest[0] == "--help" || rest[0] == "help") {
		upgradeUsage(os.Stdout)
		return 0
	}
	if len(rest) > 0 {
		fmt.Fprintf(os.Stderr, "mwan opnsense upgrade %s: unexpected arguments: %v\n", verb, rest)
		return 2
	}
	switch verb {
	case upgradePhasePrepare,
		upgradePhaseExecute,
		upgradePhaseValidate,
		upgradePhaseRollback,
		upgradePhaseCommit,
		upgradePhaseRun,
		upgradePhaseGC:
		return runUpgradePhase(verb)
	case upgradePhaseReset:
		return runUpgradeReset()
	default:
		fmt.Fprintf(os.Stderr, "mwan opnsense upgrade: unknown phase %q\n", string(verb))
		upgradeUsage(os.Stderr)
		return 2
	}
}

// upgradeInputs is the resolved set of TOML-derived values every phase
// needs. Loading it once and validating every required field up-front
// guarantees the operator sees one clear error message instead of a
// half-completed run.
type upgradeInputs struct {
	VMID                string
	StateDir            string
	GRPCTarget          string
	Target              string
	ExecTimeout         time.Duration
	UpgradeTimeout      time.Duration
	PostRollbackWait    time.Duration
	DryRunExecute       bool
	UseBootEnvironment  bool
	AcceptPartial       bool
	KeepSnapshot        bool
	GCOlderThan         time.Duration
	ResetConfirm        bool
	DiffAgainst         string
	SettleAfterUpgrade  time.Duration
	APIKey              string
	APISecret           string
	BGPv4Neighbors      string
	BGPv6Neighbors      string
	OPNsenseLAN         string
	MWANOpnsenseSock    string
	MWANOpnsenseHostSck string
	OPNsenseSSH         string
	OPNsenseJump        string
	ProxmoxSSH          string
	LANClientSSH        string
	OPNsenseAddr        string
}

// buildRedial returns a context-free closure suitable for the
// upgrade.GRPCExecutor.Redial field. The closure runs in a goroutine
// that has no caller ctx (the executor invokes it on demand after a
// rollback drops the gRPC channel), so plain opnsense.Dial is the
// right primitive here.
func buildRedial(target string) func() (upgrade.OPNsenseRPCClient, error) {
	return func() (upgrade.OPNsenseRPCClient, error) {
		c, err := opnsense.Dial(target)
		if err != nil {
			slog.Error("opnsense upgrade: redial", "err", err, "target", target)
			return nil, fmt.Errorf("dial %s: %w", target, err)
		}
		return c.RPC(), nil
	}
}

func resolveUpgradeInputs() (upgradeInputs, *config.Config, error) {
	var ui upgradeInputs
	cfg, err := loadOpnsenseConfig()
	if err != nil {
		return ui, nil, err
	}
	vmid, err := requireUpgradeVMID(cfg)
	if err != nil {
		return ui, nil, err
	}
	stateDir, err := requireUpgradeStateDir(cfg)
	if err != nil {
		return ui, nil, err
	}
	grpcTarget, err := requireUpgradeGRPCTarget(cfg)
	if err != nil {
		return ui, nil, err
	}
	execTimeout, err := parseRequiredDuration(cfg.OPNsense.Upgrade.ExecTimeoutDuration, "[opnsense.upgrade].exec_timeout")
	if err != nil {
		return ui, nil, err
	}
	upgradeTimeout, err := parseRequiredDuration(cfg.OPNsense.Upgrade.UpgradeTimeoutDuration, "[opnsense.upgrade].upgrade_timeout")
	if err != nil {
		return ui, nil, err
	}
	postRollbackWait, err := parseRequiredDuration(cfg.OPNsense.Upgrade.PostRollbackWaitDuration, "[opnsense.upgrade].post_rollback_wait")
	if err != nil {
		return ui, nil, err
	}
	gcOlderThan, err := parseRequiredDuration(cfg.OPNsense.Upgrade.GCOlderThan, "[opnsense.upgrade].gc_older_than")
	if err != nil {
		return ui, nil, err
	}
	settle, err := parseRequiredDuration(cfg.OPNsense.Upgrade.Validate.SettleAfterUpgrade, "[opnsense.upgrade.validate].settle_after_upgrade")
	if err != nil {
		return ui, nil, err
	}
	ui = upgradeInputs{
		VMID:                vmid,
		StateDir:            stateDir,
		GRPCTarget:          grpcTarget,
		Target:              cfg.OPNsense.Upgrade.Target,
		ExecTimeout:         execTimeout,
		UpgradeTimeout:      upgradeTimeout,
		PostRollbackWait:    postRollbackWait,
		DryRunExecute:       cfg.OPNsense.Upgrade.DryRunExecute,
		UseBootEnvironment:  cfg.OPNsense.Upgrade.UseBootEnvironment,
		AcceptPartial:       cfg.OPNsense.Upgrade.AcceptPartial,
		KeepSnapshot:        cfg.OPNsense.Upgrade.KeepSnapshot,
		GCOlderThan:         gcOlderThan,
		ResetConfirm:        cfg.OPNsense.Upgrade.ResetConfirm,
		DiffAgainst:         cfg.OPNsense.Upgrade.DiffAgainst,
		SettleAfterUpgrade:  settle,
		APIKey:              cfg.OPNsense.Upgrade.Validate.APIKey,
		APISecret:           cfg.OPNsense.Upgrade.Validate.APISecret,
		BGPv4Neighbors:      cfg.OPNsense.Upgrade.Validate.BGPv4Neighbors,
		BGPv6Neighbors:      cfg.OPNsense.Upgrade.Validate.BGPv6Neighbors,
		OPNsenseLAN:         cfg.OPNsense.Upgrade.Validate.OPNsenseLAN,
		MWANOpnsenseSock:    cfg.OPNsense.Upgrade.Validate.MWANOpnsenseSocket,
		MWANOpnsenseHostSck: cfg.OPNsense.Upgrade.Validate.MWANOpnsenseHostSock,
		OPNsenseSSH:         cfg.OPNsense.Upgrade.OPNsenseSSH,
		OPNsenseJump:        cfg.OPNsense.Upgrade.OPNsenseJump,
		ProxmoxSSH:          cfg.OPNsense.Upgrade.ProxmoxSSH,
		LANClientSSH:        cfg.OPNsense.Upgrade.LANClientSSH,
		OPNsenseAddr:        cfg.OPNsense.Upgrade.OPNsenseAddr,
	}
	return ui, cfg, nil
}

func (ui upgradeInputs) toOptions() upgrade.Options {
	return upgrade.Options{
		VMID:                ui.VMID,
		Target:              ui.Target,
		StateDir:            ui.StateDir,
		DeployID:            "",
		Snapshot:            "",
		DryRunExecute:       ui.DryRunExecute,
		DryRunGC:            false,
		UseBootEnvironment:  ui.UseBootEnvironment,
		AcceptPartial:       ui.AcceptPartial,
		KeepSnapshot:        ui.KeepSnapshot,
		OlderThan:           ui.GCOlderThan,
		UpgradeTimeout:      ui.UpgradeTimeout,
		PostRollbackTimeout: ui.PostRollbackWait,
	}
}

// buildUpgradeDeps wires the production Deps. Executor and validator both ride
// the gRPC channel. The SSH-host fields are still passed into validator probes
// that talk to OPNsense over SSH.
func buildUpgradeDeps(cfg *config.Config, ui upgradeInputs) (upgrade.Deps, error) {
	logger := slog.Default()
	notifier := notify.FromConfig(cfg, logger, "mwan-opnsense-upgrade")
	realOps := ops.NewRealOps(cfg, logger)

	rpcCli, err := opnsense.Dial(ui.GRPCTarget)
	if err != nil {
		slog.Error("opnsense upgrade: dial", "err", err, "target", ui.GRPCTarget)
		return upgrade.Deps{}, fmt.Errorf("dial %s: %w", ui.GRPCTarget, err)
	}
	target := ui.GRPCTarget
	redial := buildRedial(target)
	exec := &upgrade.GRPCExecutor{
		RPC:                rpcCli.RPC(),
		ExecTimeoutSeconds: upgradeExecTimeoutSeconds(ui.ExecTimeout),
		Redial:             redial,
	}
	return upgrade.Deps{
		Snap:     realOps,
		Exec:     exec,
		Validate: newValidatorAdapter(ui),
		Notifier: notifier,
		Clock:    nil,
		Log:      logger,
	}, nil
}

// validatorAdapter satisfies upgrade.Validator by mapping the upgrade
// package's ValidateContext into a validate.Run call. All inputs come
// from the loaded TOML, not flags.
type validatorAdapter struct {
	ui upgradeInputs
}

func newValidatorAdapter(ui upgradeInputs) *validatorAdapter {
	return &validatorAdapter{ui: ui}
}

func (a *validatorAdapter) Validate(ctx context.Context, vctx upgrade.ValidateContext) (upgrade.ValidationResult, error) {
	vmidInt, err := strconv.Atoi(vctx.VMID)
	if err != nil {
		return emptyUpgradeValidation(), wrapErr(ctx, "validatorAdapter: parse vmid", err)
	}
	cfg := validate.Config{
		VMID:                   vmidInt,
		DeployID:               vctx.DeployID,
		StateDir:               vctx.StateDir,
		BGPv4Neighbors:         splitNonEmpty(a.ui.BGPv4Neighbors),
		BGPv6Neighbors:         splitNonEmpty(a.ui.BGPv6Neighbors),
		OPNsenseLAN:            a.ui.OPNsenseLAN,
		MWANOpnsenseSocket:     a.ui.MWANOpnsenseSock,
		MWANOpnsenseHostSocket: a.ui.MWANOpnsenseHostSck,
		APIAuth:                buildBasicAuth(a.ui.APIKey, a.ui.APISecret),
		SettleAfterUpgrade:     a.ui.SettleAfterUpgrade,
		SeverityFilter:         "",
	}
	rpcCli, err := opnsense.DialContext(ctx, a.ui.GRPCTarget)
	if err != nil {
		return emptyUpgradeValidation(), wrapErr(ctx, "validatorAdapter: dial "+a.ui.GRPCTarget, err)
	}
	env := &validate.GRPCEnv{
		RPC: rpcCli.RPC(),
		Fallback: &validate.ExecEnv{
			OPNsenseSSHHost:     a.ui.OPNsenseSSH,
			OPNsenseSSHJumpHost: a.ui.OPNsenseJump,
			ProxmoxSSHHost:      a.ui.ProxmoxSSH,
			LANClientSSHHost:    a.ui.LANClientSSH,
			OPNsenseAddr:        a.ui.OPNsenseAddr,
			HTTPClient:          nil,
			Clock:               nil,
		},
		ExecTimeoutSeconds: upgradeExecTimeoutSeconds(a.ui.ExecTimeout),
		Clock:              nil,
	}
	runCtx := ctx
	if a.ui.ExecTimeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, a.ui.ExecTimeout)
		defer cancel()
	}
	baseline, err := validate.Run(runCtx, cfg, nil, env)
	if err != nil {
		return emptyUpgradeValidation(), wrapErr(ctx, "validatorAdapter: validate.Run", err)
	}
	if baseline == nil {
		return emptyUpgradeValidation(), wrapErr(ctx, "validatorAdapter: nil baseline", errors.New("validate.Run returned nil"))
	}
	return upgrade.AggregateChecks(translateResults(baseline.Results)), nil
}

func buildBasicAuth(apiKey, apiSecret string) *validate.BasicAuth {
	if apiKey == "" && apiSecret == "" {
		return nil
	}
	return &validate.BasicAuth{Username: apiKey, Password: apiSecret}
}

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

func noteForResult(r validate.Result) string {
	if r.Outcome == validate.OutcomePass {
		return r.ParsedValue
	}
	if r.Message == "" {
		return string(r.Outcome)
	}
	return fmt.Sprintf("%s: %s", r.Outcome, r.Message)
}

func emptyUpgradeValidation() upgrade.ValidationResult {
	return upgrade.ValidationResult{Checks: nil, AllPass: false, AnyFail: false, Partial: false}
}

func splitNonEmpty(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func upgradeExecTimeoutSeconds(d time.Duration) int32 {
	if d <= 0 {
		return 0
	}
	rounded := (d + time.Second - 1) / time.Second
	const int32Max = int32(2147483647)
	if rounded > time.Duration(int32Max) {
		return int32Max
	}
	return int32(rounded)
}

func runUpgradePhase(phase upgradePhase) int {
	ui, cfg, err := resolveUpgradeInputs()
	if err != nil {
		return printAndExit("upgrade "+string(phase), err)
	}
	deps, err := buildUpgradeDeps(cfg, ui)
	if err != nil {
		return printAndExit("upgrade "+string(phase), err)
	}
	ctx := context.Background()
	opts := ui.toOptions()
	switch phase {
	case upgradePhasePrepare:
		st, err := upgrade.Prepare(ctx, deps, opts)
		if err != nil {
			return printAndExit("upgrade prepare", err)
		}
		fmt.Fprintf(os.Stdout, "phase=%s deploy_id=%s snapshot=%s\n", st.Phase, st.DeployID, st.Snapshot)
	case upgradePhaseExecute:
		st, err := upgrade.Execute(ctx, deps, opts)
		if err != nil {
			return printAndExit("upgrade execute", err)
		}
		fmt.Fprintf(os.Stdout, "phase=%s\n", st.Phase)
	case upgradePhaseValidate:
		return runUpgradeValidatePhase(ctx, deps, opts, ui)
	case upgradePhaseRollback:
		st, err := upgrade.Rollback(ctx, deps, opts)
		if err != nil {
			return printAndExit("upgrade rollback", err)
		}
		fmt.Fprintf(os.Stdout, "phase=%s snapshot=%s\n", st.Phase, st.Snapshot)
	case upgradePhaseCommit:
		st, err := upgrade.Commit(ctx, deps, opts)
		if err != nil {
			return printAndExit("upgrade commit", err)
		}
		fmt.Fprintf(os.Stdout, "phase=%s\n", st.Phase)
	case upgradePhaseRun:
		out, err := upgrade.Run(ctx, deps, opts)
		if err != nil {
			return printAndExit("upgrade run", err)
		}
		fmt.Fprintf(os.Stdout, "reached=%s auto_rollback=%t\n", out.Reached, out.AutoRollback)
	case upgradePhaseReset:
		// runUpgradePhase never receives upgradePhaseReset; the outer
		// dispatch in runOPNsenseUpgradeCmd routes reset to its own
		// helper. The case exists to satisfy the exhaustive linter.
		return printAndExit("upgrade", fmt.Errorf("internal: reset routed to phase switch"))
	case upgradePhaseGC:
		res, err := upgrade.GC(ctx, deps, opts)
		if err != nil {
			return printAndExit("upgrade gc", err)
		}
		fmt.Fprintf(os.Stdout, "deleted=%s skipped=%s\n",
			strings.Join(res.Deleted, ","), strings.Join(res.Skipped, ","))
	default:
		return printAndExit("upgrade", fmt.Errorf("internal: unhandled phase %q", phase))
	}
	return 0
}

// runUpgradeValidatePhase runs the orchestrator's validate step and
// also persists a standalone baseline so the operator can compare runs
// across deploys. The orchestrator path is the source of truth for the
// state machine; the baseline is the operator-facing artefact.
func runUpgradeValidatePhase(ctx context.Context, deps upgrade.Deps, opts upgrade.Options, ui upgradeInputs) int {
	st, res, err := upgrade.Validate(ctx, deps, opts)
	if err != nil {
		return printAndExit("upgrade validate", err)
	}
	fmt.Fprintf(os.Stdout, "phase=%s all_pass=%t partial=%t failing=%s\n",
		st.Phase, res.AllPass, res.Partial, strings.Join(st.FailingCheck, ","))

	if st.DeployID != "" {
		captureAndPrintBaseline(ctx, ui, st.DeployID)
	}
	return 0
}

// captureAndPrintBaseline drives the standalone baseline capture and
// emits the operator-facing summary lines. Soft failures are warned
// and swallowed because the orchestrator's verdict is the contract;
// the baseline is a side-channel artefact.
func captureAndPrintBaseline(ctx context.Context, ui upgradeInputs, deployID string) {
	baseline, runErr := standaloneBaseline(ctx, ui, deployID)
	if runErr != nil {
		slog.WarnContext(ctx, "upgrade validate: standalone baseline failed", "err", runErr)
		return
	}
	vmid, parseErr := strconv.Atoi(ui.VMID)
	if parseErr != nil {
		slog.WarnContext(ctx, "upgrade validate: vmid parse", "err", parseErr, "vmid", ui.VMID)
		return
	}
	if saveErr := validate.SaveBaseline(ui.StateDir, vmid, deployID, validate.PreBaselineFilename, baseline); saveErr != nil {
		slog.WarnContext(ctx, "upgrade validate: save baseline", "err", saveErr)
		return
	}
	validate.SortResultsByID(baseline.Results)
	counts := validate.CountByOutcome(baseline.Results)
	fmt.Fprintf(os.Stdout, "baseline: path=%s pass=%d fail=%d skip=%d error=%d\n",
		validate.ArtefactPath(ui.StateDir, vmid, deployID),
		counts[validate.OutcomePass],
		counts[validate.OutcomeFail],
		counts[validate.OutcomeSkip],
		counts[validate.OutcomeError])
	if ui.DiffAgainst == "" {
		return
	}
	prior, loadErr := validate.LoadBaseline(ui.DiffAgainst)
	if loadErr != nil {
		slog.WarnContext(ctx, "upgrade validate: load prior baseline", "err", loadErr, "path", ui.DiffAgainst)
		return
	}
	report := validate.Diff(prior, baseline)
	fmt.Fprintf(os.Stdout, "diff: verdict=%s entries=%d\n", report.Verdict, len(report.Entries))
}

// standaloneBaseline runs validate.Run end-to-end and returns the
// captured Baseline. The validatorAdapter inside upgrade.Validate
// already aggregated check results into a ValidationResult, but the
// adapter discards the baseline metadata (capture time, plugin set,
// pf rule counts) needed for cross-deploy diffs.
func standaloneBaseline(ctx context.Context, ui upgradeInputs, deployID string) (*validate.Baseline, error) {
	vmid, err := strconv.Atoi(ui.VMID)
	if err != nil {
		slog.ErrorContext(ctx, "standaloneBaseline: parse vmid", "err", err, "vmid", ui.VMID)
		return nil, fmt.Errorf("parse vmid %q: %w", ui.VMID, err)
	}
	rpcCli, err := opnsense.DialContext(ctx, ui.GRPCTarget)
	if err != nil {
		slog.ErrorContext(ctx, "standaloneBaseline: dial", "err", err, "target", ui.GRPCTarget)
		return nil, fmt.Errorf("dial %s: %w", ui.GRPCTarget, err)
	}
	cfg := validate.Config{
		VMID:                   vmid,
		DeployID:               deployID,
		StateDir:               ui.StateDir,
		BGPv4Neighbors:         splitNonEmpty(ui.BGPv4Neighbors),
		BGPv6Neighbors:         splitNonEmpty(ui.BGPv6Neighbors),
		OPNsenseLAN:            ui.OPNsenseLAN,
		MWANOpnsenseSocket:     ui.MWANOpnsenseSock,
		MWANOpnsenseHostSocket: ui.MWANOpnsenseHostSck,
		APIAuth:                buildBasicAuth(ui.APIKey, ui.APISecret),
		SettleAfterUpgrade:     ui.SettleAfterUpgrade,
		SeverityFilter:         "",
	}
	env := &validate.GRPCEnv{
		RPC: rpcCli.RPC(),
		Fallback: &validate.ExecEnv{
			OPNsenseSSHHost:     ui.OPNsenseSSH,
			OPNsenseSSHJumpHost: ui.OPNsenseJump,
			ProxmoxSSHHost:      ui.ProxmoxSSH,
			LANClientSSHHost:    ui.LANClientSSH,
			OPNsenseAddr:        ui.OPNsenseAddr,
			HTTPClient:          nil,
			Clock:               nil,
		},
		ExecTimeoutSeconds: upgradeExecTimeoutSeconds(ui.ExecTimeout),
		Clock:              nil,
	}
	baseline, err := validate.Run(ctx, cfg, nil, env)
	if err != nil {
		slog.ErrorContext(ctx, "standaloneBaseline: validate.Run", "err", err)
		return nil, fmt.Errorf("validate.Run: %w", err)
	}
	if baseline == nil {
		return nil, errors.New("validate.Run returned nil baseline")
	}
	if baseline.SchemaVersion == 0 {
		return nil, errors.New("baseline missing schema version")
	}
	return baseline, nil
}

// runUpgradeReset is the only phase whose semantics differ from a
// straight phase transition: it computes a plan, prints it, and only
// applies it when [opnsense.upgrade].reset_confirm is true. Reading the
// confirm bit from TOML means an operator who wants to apply a reset
// flips it in their config and re-runs, mirroring the old --confirm
// flag behaviour.
func runUpgradeReset() int {
	ui, cfg, err := resolveUpgradeInputs()
	if err != nil {
		return printAndExit("upgrade reset", err)
	}
	// Reset only needs the Snapshotter, not the validator or executor.
	realOps := ops.NewRealOps(cfg, slog.Default())
	deps := upgrade.Deps{
		Snap:     realOps,
		Exec:     nil,
		Validate: nil,
		Notifier: nil,
		Clock:    nil,
		Log:      slog.Default(),
	}
	plan, err := upgrade.Reset(context.Background(), deps, upgrade.ResetOptions{
		VMID:     ui.VMID,
		StateDir: ui.StateDir,
		DeployID: "",
	})
	if err != nil {
		return printAndExit("upgrade reset", err)
	}
	if plan.NothingToDo {
		fmt.Fprintln(os.Stdout, "nothing to do")
		return 0
	}
	if !ui.ResetConfirm {
		printResetPlan(os.Stdout, plan)
		fmt.Fprintln(os.Stdout, "")
		fmt.Fprintln(os.Stdout, "set [opnsense.upgrade].reset_confirm = true in /etc/mwan/config.toml to apply.")
		return 2
	}
	if err := upgrade.ResetExecute(context.Background(), deps, plan); err != nil {
		return printAndExit("upgrade reset", err)
	}
	fmt.Fprintln(os.Stdout, "reset complete")
	return 0
}

func printResetPlan(w *os.File, plan upgrade.Plan) {
	fmt.Fprintf(w, "reset plan for vmid=%s deploy_id=%s (dry run):\n", plan.VMID, plan.DeployID)
	if len(plan.SnapshotsToDelete) == 0 {
		fmt.Fprintln(w, "  snapshots to delete: (none)")
	} else {
		fmt.Fprintln(w, "  snapshots to delete:")
		for _, s := range plan.SnapshotsToDelete {
			fmt.Fprintf(w, "    - %s\n", s)
		}
	}
	if plan.RollbackTarget == "" {
		fmt.Fprintln(w, "  rollback target: (none)")
	} else {
		fmt.Fprintf(w, "  rollback target: %s\n", plan.RollbackTarget)
	}
	if plan.StatePath == "" {
		fmt.Fprintln(w, "  state.json to remove: (none)")
	} else {
		fmt.Fprintf(w, "  state.json to remove: %s\n", plan.StatePath)
	}
}
