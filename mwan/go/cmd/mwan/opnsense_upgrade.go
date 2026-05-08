package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/notify"
	"goodkind.io/mwan/internal/opnsense/upgrade"
	"goodkind.io/mwan/internal/ops"
)

// upgradePhase is the typed enum of opnsense-upgrade subcommand names.
// Using a typed string here satisfies the "switch on bare string"
// staticcheck-extra rule and keeps the dispatch list discoverable.
type upgradePhase string

const (
	upgradePhasePrepare  upgradePhase = "prepare"
	upgradePhaseExecute  upgradePhase = "execute"
	upgradePhaseValidate upgradePhase = "validate"
	upgradePhaseRollback upgradePhase = "rollback"
	upgradePhaseCommit   upgradePhase = "commit"
	upgradePhaseRun      upgradePhase = "run"
	upgradePhaseGC       upgradePhase = "gc"
	upgradePhaseHelp1    upgradePhase = "-h"
	upgradePhaseHelp2    upgradePhase = "--help"
	upgradePhaseHelp3    upgradePhase = "help"
)

// runOPNsenseUpgrade dispatches `mwan opnsense-upgrade <subcommand>`.
// It is the CLI surface for the per-phase entry points in
// internal/opnsense/upgrade. The dispatch is one switch by subcommand
// name; flag parsing lives per subcommand so flag sets stay tight.
func runOPNsenseUpgrade(args []string) error {
	if len(args) == 0 {
		printUpgradeUsage(os.Stderr)
		err := errors.New("opnsense-upgrade: subcommand required")
		slog.Error("opnsense-upgrade: subcommand required", "err", err)
		return err
	}
	sub := upgradePhase(args[0])
	rest := args[1:]
	switch sub {
	case upgradePhasePrepare:
		return runUpgradePrepare(rest)
	case upgradePhaseExecute:
		return runUpgradeExecute(rest)
	case upgradePhaseValidate:
		return runUpgradeValidate(rest)
	case upgradePhaseRollback:
		return runUpgradeRollback(rest)
	case upgradePhaseCommit:
		return runUpgradeCommit(rest)
	case upgradePhaseRun:
		return runUpgradeRun(rest)
	case upgradePhaseGC:
		return runUpgradeGC(rest)
	case upgradePhaseHelp1, upgradePhaseHelp2, upgradePhaseHelp3:
		printUpgradeUsage(os.Stdout)
		return nil
	default:
		printUpgradeUsage(os.Stderr)
		err := errors.New("opnsense-upgrade: unknown subcommand")
		slog.Error("opnsense-upgrade: unknown subcommand", "err", err)
		return err
	}
}

func printUpgradeUsage(w *os.File) {
	fmt.Fprintln(w, "usage: mwan opnsense-upgrade <prepare|execute|validate|rollback|commit|run|gc> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Per-phase entry points for the OPNsense upgrade rollback flow.")
	fmt.Fprintln(w, "Common flags:")
	fmt.Fprintln(w, "  --vmid <id>             VMID to operate on (required)")
	fmt.Fprintln(w, "  --target <ver>          Target OPNsense version (e.g. 26.7)")
	fmt.Fprintln(w, "  --state-dir <path>      State directory (default /var/lib/mwan/upgrades)")
	fmt.Fprintln(w, "  --deploy-id <id>        Deploy identifier (auto-generated when empty)")
	fmt.Fprintln(w, "  --snapshot <name>       Override snapshot name for rollback/commit")
	fmt.Fprintln(w, "  --dry-run-execute       Run opnsense-upgrade -c instead of the real upgrade")
	fmt.Fprintln(w, "  --use-boot-environment  Capture a bectl boot environment alongside the snapshot")
	fmt.Fprintln(w, "  --accept-partial        Treat partial-pass as a manual-decision state instead of fail")
	fmt.Fprintln(w, "  --keep-snapshot         Retain the snapshot during commit (sweep later via gc)")
	fmt.Fprintln(w, "  --older-than <dur>      gc threshold (default 168h)")
}

// upgradeFlags holds the flag values shared across phases. Each phase
// subcommand sets up its own flag set but reads the same struct so the
// surface stays uniform. The validator-related fields mirror the
// `mwan opnsense-validate` flag set so the upgrade phases can drive the
// same MWAN-153 check matrix without operators having to learn two
// flag surfaces.
type upgradeFlags struct {
	vmid               string
	target             string
	stateDir           string
	deployID           string
	snapshot           string
	dryRunExecute      bool
	useBootEnvironment bool
	acceptPartial      bool
	keepSnapshot       bool
	olderThan          time.Duration
	upgradeTimeout     time.Duration
	postRollbackWait   time.Duration

	// Validator transport flags. Mirror mwan opnsense-validate so
	// `mwan opnsense-upgrade {validate,run}` can run the same matrix.
	opnsenseSSHHost  string
	opnsenseJumpHost string
	proxmoxSSHHost   string
	lanClientSSH     string
	opnsenseAddr     string
	apiKey           string
	apiSecret        string
	bgpV4Neighbors   string
	bgpV6Neighbors   string
	opnsenseLAN      string
	mwanSocket       string
	mwanHostSocket   string
	settleAfter      time.Duration
}

func registerCommonFlags(fs *flag.FlagSet, f *upgradeFlags) {
	fs.StringVar(&f.vmid, "vmid", "", "VMID to operate on (required)")
	fs.StringVar(&f.target, "target", "", "Target OPNsense version")
	fs.StringVar(&f.stateDir, "state-dir", upgrade.DefaultStateDir, "Upgrade state directory")
	fs.StringVar(&f.deployID, "deploy-id", "", "Deploy identifier; auto-generated when empty")
	fs.StringVar(&f.snapshot, "snapshot", "", "Snapshot name override")
	fs.BoolVar(&f.dryRunExecute, "dry-run-execute", false, "Run opnsense-upgrade -c instead of the real upgrade")
	fs.BoolVar(&f.useBootEnvironment, "use-boot-environment", false, "Capture bectl boot environment if available")
	fs.BoolVar(&f.acceptPartial, "accept-partial", false, "Treat partial-pass validation as manual-decision state")
	fs.BoolVar(&f.keepSnapshot, "keep-snapshot", false, "Retain the upgrade snapshot during commit")
	fs.DurationVar(&f.olderThan, "older-than", upgrade.DefaultGCThreshold, "gc age threshold")
	fs.DurationVar(&f.upgradeTimeout, "upgrade-timeout", upgrade.DefaultUpgradeTimeout, "Watchdog timeout for the in-guest upgrade")
	fs.DurationVar(&f.postRollbackWait, "post-rollback-wait", upgrade.DefaultPostRollbackTimeout, "QGA liveness probe deadline after rollback")
	fs.StringVar(&f.opnsenseSSHHost, "opnsense-ssh", "", "ssh destination for the OPNsense guest (validator)")
	fs.StringVar(&f.opnsenseJumpHost, "opnsense-jump", "", "ssh ProxyJump for OPNsense (validator)")
	fs.StringVar(&f.proxmoxSSHHost, "proxmox-ssh", "", "ssh destination for the Proxmox host (validator)")
	fs.StringVar(&f.lanClientSSH, "lan-client-ssh", "", "ssh destination for a LAN client (validator data-plane probes)")
	fs.StringVar(&f.opnsenseAddr, "opnsense-addr", "", "host:port for HTTPS GETs against the OPNsense web UI (validator)")
	fs.StringVar(&f.apiKey, "api-key", "", "OPNsense API key (validator)")
	fs.StringVar(&f.apiSecret, "api-secret", "", "OPNsense API secret (validator)")
	fs.StringVar(&f.bgpV4Neighbors, "bgp-v4-neighbors", "", "comma-separated v4 BGP peer addresses (validator)")
	fs.StringVar(&f.bgpV6Neighbors, "bgp-v6-neighbors", "", "comma-separated v6 BGP peer addresses (validator)")
	fs.StringVar(&f.opnsenseLAN, "opnsense-lan", "", "OPNsense LAN address used by dig probes (validator)")
	fs.StringVar(&f.mwanSocket, "mwan-opnsense-socket", "", "unix socket path probed by the gRPC check (validator)")
	fs.StringVar(&f.mwanHostSocket, "mwan-opnsense-host-socket", "", "unix socket path the bridge listens on (validator)")
	fs.DurationVar(&f.settleAfter, "settle-after-upgrade", 5*time.Minute, "validator dwell time before DHCP-related checks run")
}

func parseUpgradeFlags(name string, args []string) (upgradeFlags, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var f upgradeFlags
	registerCommonFlags(fs, &f)
	if err := fs.Parse(args); err != nil {
		slog.Error("opnsense-upgrade: parse flags", "err", err, "subcommand", name)
		return f, fmt.Errorf("%s: parse flags: %w", name, err)
	}
	if err := requireVMID(f.vmid); err != nil {
		return f, err
	}
	return f, nil
}

func (f upgradeFlags) toOptions() upgrade.Options {
	return upgrade.Options{
		VMID:                f.vmid,
		Target:              f.target,
		StateDir:            f.stateDir,
		DeployID:            f.deployID,
		Snapshot:            f.snapshot,
		DryRunExecute:       f.dryRunExecute,
		UseBootEnvironment:  f.useBootEnvironment,
		AcceptPartial:       f.acceptPartial,
		KeepSnapshot:        f.keepSnapshot,
		OlderThan:           f.olderThan,
		UpgradeTimeout:      f.upgradeTimeout,
		PostRollbackTimeout: f.postRollbackWait,
	}
}

// buildUpgradeDeps wires the production Deps from the loaded config.
// The Validator is the MWAN-153 check matrix wrapped in
// validatorAdapter so the upgrade phases can drive it through the
// upgrade.Validator interface. Validator transport flags (SSH hosts,
// API auth, BGP neighbors) come from the upgrade subcommand's own flag
// set; see registerCommonFlags.
func buildUpgradeDeps(cfg *config.Config, f upgradeFlags) upgrade.Deps {
	logger := slog.Default()
	notifier := notify.FromConfig(cfg, logger, "mwan-opnsense-upgrade")
	realOps := ops.NewRealOps(cfg, logger)
	return upgrade.Deps{
		Snap:     realOps,
		Exec:     opsExecutorAdapter{ops: realOps},
		Validate: newValidatorAdapter(f),
		Notifier: notifier,
		Clock:    nil,
		Log:      logger,
	}
}

// opsExecutorAdapter bridges ops.SysOps.GuestExec to the
// upgrade.Executor surface. Keeping this thin means the upgrade
// package does not depend on internal/ops for testing.
type opsExecutorAdapter struct {
	ops ops.SysOps
}

func (a opsExecutorAdapter) GuestExec(ctx context.Context, vmid string, args ...string) (upgrade.GuestExecResult, error) {
	res, err := a.ops.GuestExec(ctx, vmid, args...)
	if err != nil {
		slog.ErrorContext(ctx, "opsExecutorAdapter.GuestExec", "err", err, "vmid", vmid)
		return upgrade.GuestExecResult{ExitCode: 0, Stdout: "", Stderr: ""}, fmt.Errorf("opsExecutorAdapter.GuestExec: %w", err)
	}
	return upgrade.GuestExecResult{ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: ""}, nil
}

func loadUpgradeConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("opnsense-upgrade: load config", "err", err)
		return nil, fmt.Errorf("opnsense-upgrade: load config: %w", err)
	}
	return cfg, nil
}

func runUpgradePrepare(args []string) error {
	f, err := parseUpgradeFlags("opnsense-upgrade prepare", args)
	if err != nil {
		return err
	}
	cfg, err := loadUpgradeConfig()
	if err != nil {
		return err
	}
	deps := buildUpgradeDeps(cfg, f)
	st, err := upgrade.Prepare(context.Background(), deps, f.toOptions())
	if err != nil {
		slog.Error("opnsense-upgrade prepare failed", "err", err)
		return fmt.Errorf("opnsense-upgrade prepare: %w", err)
	}
	fmt.Fprintf(os.Stdout, "phase=%s deploy_id=%s snapshot=%s\n", st.Phase, st.DeployID, st.Snapshot)
	return nil
}

func runUpgradeExecute(args []string) error {
	f, err := parseUpgradeFlags("opnsense-upgrade execute", args)
	if err != nil {
		return err
	}
	cfg, err := loadUpgradeConfig()
	if err != nil {
		return err
	}
	deps := buildUpgradeDeps(cfg, f)
	st, err := upgrade.Execute(context.Background(), deps, f.toOptions())
	if err != nil {
		slog.Error("opnsense-upgrade execute failed", "err", err)
		return fmt.Errorf("opnsense-upgrade execute: %w", err)
	}
	fmt.Fprintf(os.Stdout, "phase=%s\n", st.Phase)
	return nil
}

func runUpgradeValidate(args []string) error {
	f, err := parseUpgradeFlags("opnsense-upgrade validate", args)
	if err != nil {
		return err
	}
	cfg, err := loadUpgradeConfig()
	if err != nil {
		return err
	}
	deps := buildUpgradeDeps(cfg, f)
	st, res, err := upgrade.Validate(context.Background(), deps, f.toOptions())
	if err != nil {
		slog.Error("opnsense-upgrade validate failed", "err", err)
		return fmt.Errorf("opnsense-upgrade validate: %w", err)
	}
	fmt.Fprintf(os.Stdout, "phase=%s all_pass=%t partial=%t failing=%s\n",
		st.Phase, res.AllPass, res.Partial, strings.Join(st.FailingCheck, ","))
	return nil
}

func runUpgradeRollback(args []string) error {
	f, err := parseUpgradeFlags("opnsense-upgrade rollback", args)
	if err != nil {
		return err
	}
	cfg, err := loadUpgradeConfig()
	if err != nil {
		return err
	}
	deps := buildUpgradeDeps(cfg, f)
	st, err := upgrade.Rollback(context.Background(), deps, f.toOptions())
	if err != nil {
		slog.Error("opnsense-upgrade rollback failed", "err", err)
		return fmt.Errorf("opnsense-upgrade rollback: %w", err)
	}
	fmt.Fprintf(os.Stdout, "phase=%s snapshot=%s\n", st.Phase, st.Snapshot)
	return nil
}

func runUpgradeCommit(args []string) error {
	f, err := parseUpgradeFlags("opnsense-upgrade commit", args)
	if err != nil {
		return err
	}
	cfg, err := loadUpgradeConfig()
	if err != nil {
		return err
	}
	deps := buildUpgradeDeps(cfg, f)
	st, err := upgrade.Commit(context.Background(), deps, f.toOptions())
	if err != nil {
		slog.Error("opnsense-upgrade commit failed", "err", err)
		return fmt.Errorf("opnsense-upgrade commit: %w", err)
	}
	fmt.Fprintf(os.Stdout, "phase=%s\n", st.Phase)
	return nil
}

func runUpgradeRun(args []string) error {
	f, err := parseUpgradeFlags("opnsense-upgrade run", args)
	if err != nil {
		return err
	}
	cfg, err := loadUpgradeConfig()
	if err != nil {
		return err
	}
	deps := buildUpgradeDeps(cfg, f)
	out, err := upgrade.Run(context.Background(), deps, f.toOptions())
	if err != nil {
		slog.Error("opnsense-upgrade run failed", "err", err)
		return fmt.Errorf("opnsense-upgrade run: %w", err)
	}
	fmt.Fprintf(os.Stdout, "reached=%s auto_rollback=%t\n", out.Reached, out.AutoRollback)
	return nil
}

func runUpgradeGC(args []string) error {
	f, err := parseUpgradeFlags("opnsense-upgrade gc", args)
	if err != nil {
		return err
	}
	cfg, err := loadUpgradeConfig()
	if err != nil {
		return err
	}
	deps := buildUpgradeDeps(cfg, f)
	res, err := upgrade.GC(context.Background(), deps, f.toOptions())
	if err != nil {
		slog.Error("opnsense-upgrade gc failed", "err", err)
		return fmt.Errorf("opnsense-upgrade gc: %w", err)
	}
	fmt.Fprintf(os.Stdout, "deleted=%s skipped=%s\n",
		strings.Join(res.Deleted, ","), strings.Join(res.Skipped, ","))
	return nil
}

func requireVMID(vmid string) error {
	if vmid == "" {
		err := errors.New("opnsense-upgrade: --vmid is required")
		slog.Error("opnsense-upgrade: --vmid is required", "err", err)
		return err
	}
	return nil
}
