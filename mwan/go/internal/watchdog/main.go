// Package watchdog implements the mwan watchdog monitoring and auto-rollback subsystem.
package watchdog

import (
	"context"
	"flag"
	"fmt"
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

// Run is the entry point for the watchdog subcommand.
// It sets up flag parsing, logger, operations layer, and coordinates the watchdog loop.
func Run(cfg *config.Config) error {
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

	if *redTeam != "" && !*redTeamLive {
		*dryRun = true
	}
	if os.Getenv("DRY_RUN") == "1" {
		*dryRun = true
	}

	// Create logger
	logger, lerr := logging.New(logging.Config{
		TextLogFile: cfg.Watchdog.LogFile,
		JSONLogFile: cfg.Watchdog.JSONLogFile,
	}, version.BuildVersionString())
	if lerr != nil {
		return fmt.Errorf("logger init: %w", lerr)
	}
	logger.Info("mwan watchdog starting", "version", version.BuildVersionString())

	// Create email sender
	emailSender := email.NewSender(cfg.Email.SMTP2GOAPIKey, cfg.Email.From, cfg.Email.BindIface, "mwan-watchdog", logger)

	// Create operations layer
	var baseOps ops.SysOps = ops.NewRealOps(cfg, emailSender)

	if *dryRun {
		logger.Info("[MODE] Dry-run enabled; destructive ops will be logged only")
		baseOps = &dryRunOps{inner: baseOps, log: logger}
	}

	if *redTeam != "" {
		preset, ok := redteam.Presets[*redTeam]
		if !ok {
			return fmt.Errorf("unknown scenario %q (use --list-scenarios)", *redTeam)
		}
		logger.Info(
			"[MODE] Red-team scenario",
			"scenario", *redTeam,
			"description", preset.Description,
			"live", *redTeamLive,
		)
		baseOps = redteam.NewOps(baseOps, preset, logger)
	}

	// Override config for red-team mode
	if *redTeam != "" {
		cfg.Watchdog.MaxIterations = *redTeamIters
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
