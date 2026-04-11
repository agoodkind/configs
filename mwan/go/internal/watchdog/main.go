// Package watchdog implements the mwan watchdog monitoring and auto-rollback subsystem.
package watchdog

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"goodkind.io/mwan/internal/alert"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
	"goodkind.io/mwan/internal/logging"
	"goodkind.io/mwan/internal/ops"
	"goodkind.io/mwan/internal/redteam"
	"goodkind.io/mwan/internal/version"
)

// watchdogFlags holds the parsed command-line flags for the watchdog subcommand.
type watchdogFlags struct {
	dryRun       bool
	redTeam      string
	redTeamLive  bool
	redTeamIters int
}

// parseFlags parses command-line flags and handles --list-scenarios.
// Returns the parsed flags. May call os.Exit(0) for --list-scenarios.
func parseFlags() watchdogFlags {
	dryRun := flag.Bool(
		"dry-run", false,
		"Skip destructive ops and emails; log decisions only",
	)
	redTeam := flag.String(
		"red-team", "",
		"Run a fault-injection scenario (implies --dry-run)",
	)
	redTeamLive := flag.Bool(
		"red-team-live", false,
		"Run red-team fault injection WITHOUT --dry-run (real VM operations)",
	)
	redTeamIters := flag.Int(
		"red-team-iterations", 10,
		"Number of loop iterations in red-team mode",
	)
	listScenarios := flag.Bool(
		"list-scenarios", false,
		"List available red-team scenarios and exit",
	)
	flag.Parse()

	if *listScenarios {
		fmt.Println("Available red-team scenarios:")
		fmt.Println()
		for name, preset := range redteam.Presets {
			fmt.Printf("  %-22s %s\n", name, preset.Description)
		}
		fmt.Println()
		fmt.Println(
			"Usage: mwan watchdog --red-team <scenario> " +
				"[--red-team-live] [--red-team-iterations N]",
		)
		os.Exit(0)
	}

	f := watchdogFlags{
		dryRun:       *dryRun,
		redTeam:      *redTeam,
		redTeamLive:  *redTeamLive,
		redTeamIters: *redTeamIters,
	}
	if f.redTeam != "" && !f.redTeamLive {
		f.dryRun = true
	}
	if os.Getenv("DRY_RUN") == "1" {
		f.dryRun = true
	}
	return f
}

// buildOpsLayer creates the logger, email sender, and operations layer,
// applying dry-run and red-team wrappers as needed.
func buildOpsLayer(cfg *config.Config, f watchdogFlags) (*slog.Logger, ops.SysOps, error) {
	logger, lerr := logging.New(logging.Config{
		TextLogFile: cfg.Watchdog.LogFile,
		JSONLogFile: cfg.Watchdog.JSONLogFile,
	}, version.BuildVersionString())
	if lerr != nil {
		return nil, nil, fmt.Errorf("logger init: %w", lerr)
	}
	logger.Info("mwan watchdog starting", "version", version.BuildVersionString())

	emailSender := email.NewSender(cfg.Email.SMTP2GOAPIKey, cfg.Email.From, cfg.Email.BindIface, "mwan-watchdog", logger)

	var baseOps ops.SysOps = ops.NewRealOps(cfg, emailSender)

	if f.dryRun {
		logger.Info("[MODE] Dry-run enabled; destructive ops will be logged only")
		baseOps = &dryRunOps{inner: baseOps, log: logger}
	}

	if f.redTeam != "" {
		preset, ok := redteam.Presets[f.redTeam]
		if !ok {
			return nil, nil, fmt.Errorf("unknown scenario %q (use --list-scenarios)", f.redTeam)
		}
		logger.Info(
			"[MODE] Red-team scenario",
			"scenario", f.redTeam,
			"description", preset.Description,
			"live", f.redTeamLive,
		)
		baseOps = redteam.NewOps(baseOps, preset, logger)
	}

	return logger, baseOps, nil
}

// Run is the entry point for the watchdog subcommand.
// It sets up flag parsing, logger, operations layer, and coordinates the watchdog loop.
func Run(cfg *config.Config) error {
	f := parseFlags()

	logger, baseOps, err := buildOpsLayer(cfg, f)
	if err != nil {
		return err
	}

	// Override config for red-team mode
	if f.redTeam != "" {
		cfg.Watchdog.MaxIterations = f.redTeamIters
		cfg.Watchdog.CheckIntervalHealthy = 1
		cfg.Watchdog.CheckIntervalDegraded = 1
		cfg.Watchdog.ConnectivityTimeoutSeconds = 3
		cfg.Watchdog.PostRollbackGraceSeconds = 2
		cfg.Watchdog.AlertCooldownSeconds = 0
	}

	// Set up signal handling
	coord := &alert.Coord{}
	mainCtx, cancelMain := context.WithCancel(context.Background())

	sigCtx, stopSig := signal.NotifyContext(
		context.Background(), syscall.SIGTERM, syscall.SIGINT,
	)
	go func() {
		<-sigCtx.Done()
		if coord.IsRollingBack() {
			logger.Info(
				"Signal received during rollback; deferring until rollback completes",
			)
			coord.OnSignalDuringRollback()
			stopSig()
			return
		}
		logger.Info("Signal received; shutting down")
		stopSig()
		cancelMain()
	}()

	// Create watchdog instance and run
	w := &watchdog{
		cfg:     cfg,
		ops:     baseOps,
		coord:   coord,
		limiter: alert.NewLimiter(cfg.Watchdog.AlertCooldownSeconds),
		log:     logger,
	}

	// Try to extract channel tracker for diagnostics
	w.tracker = extractTracker(baseOps)

	w.run(mainCtx)
	return nil
}

func extractTracker(baseOps ops.SysOps) *ops.ChannelTracker {
	switch v := baseOps.(type) {
	case *ops.RealOps:
		return v.ExtractTracker()
	case *dryRunOps:
		return extractTracker(v.inner)
	case *redteam.Ops:
		// Red team wraps another ops, try to extract from that
		// Since redteam.Ops.inner is private, we can't access it directly.
		// Return nil in this case.
		return nil
	default:
		return nil
	}
}
