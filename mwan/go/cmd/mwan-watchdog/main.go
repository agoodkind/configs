// Build (from this directory):
//
//	GOOS=linux GOARCH=amd64 go build \
//	  -ldflags="-X main.gitCommit=$(git rev-parse --short HEAD) \
//	            -X 'main.gitDirty=$(git diff --quiet HEAD -- . && echo clean || echo dirty)'" \
//	  -o mwan-watchdog .
//
// Usage:
//
//	mwan-watchdog                          Normal monitoring mode
//	mwan-watchdog --dry-run                Real probes, skip destructive ops
//	mwan-watchdog --red-team <scenario>    Fault injection (implies --dry-run)
//	mwan-watchdog --list-scenarios         Show available red-team scenarios
//
// Primary path:   gRPC over virtio-vsock (mdlayher/vsock) to mwan-agent inside VM 113.
// Fallback path:  Proxmox REST API (agent/exec + agent/exec-status) when vsock unavailable.
// VM lifecycle:   qm CLI (stop / rollback / start), unchanged.
// Email:          send-email/mailer library (SMTP2GO HTTP API auto-detected from env).
// Network config: /etc/mwan-watchdog/network.toml (or $MWAN_NETWORK_CONFIG).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	dryRun := flag.Bool(
		"dry-run", false,
		"Skip destructive ops and emails; log decisions only",
	)
	redTeam := flag.String(
		"red-team", "",
		"Run a fault-injection scenario (implies --dry-run)",
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
		for name, preset := range redTeamPresets {
			fmt.Printf("  %-22s %s\n", name, preset.Description)
		}
		fmt.Println()
		fmt.Println(
			"Usage: mwan-watchdog --red-team <scenario> [--red-team-iterations N]",
		)
		os.Exit(0)
	}

	if *redTeam != "" {
		*dryRun = true
	}
	if os.Getenv("DRY_RUN") == "1" {
		*dryRun = true
	}

	cfg, err := loadConfig(!*dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan-watchdog: %v\n", err)
		os.Exit(1)
	}

	nc, err := loadNetworkConfig(cfg.NetworkConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan-watchdog: %v\n", err)
		os.Exit(1)
	}

	if *redTeam != "" {
		cfg.MaxIterations = *redTeamIters
		cfg.CheckIntervalHealthy = 1 * time.Second
		cfg.CheckIntervalDegraded = 1 * time.Second
		cfg.ConnectivityTimeoutSeconds = 3
		cfg.PostRollbackGraceSeconds = 2 * time.Second
		cfg.AlertCooldownSeconds = 0
	}

	logger, lerr := newWatchdogLogger(cfg)
	if lerr != nil {
		fmt.Fprintf(
			os.Stderr,
			"mwan-watchdog: logger init: %v\n",
			lerr,
		)
		os.Exit(1)
	}
	logger.Info("mwan-watchdog starting", "version", buildVersionString())

	var ops sysOps = newRealOps(cfg, nc)

	if *dryRun {
		logger.Info("[MODE] Dry-run enabled; destructive ops will be logged only")
		ops = &dryRunOps{inner: ops, log: logger}
	}

	if *redTeam != "" {
		preset, ok := redTeamPresets[*redTeam]
		if !ok {
			fmt.Fprintf(
				os.Stderr,
				"mwan-watchdog: unknown scenario %q (use --list-scenarios)\n",
				*redTeam,
			)
			os.Exit(1)
		}
		logger.Info(
			"[MODE] Red-team scenario",
			"scenario", *redTeam,
			"description", preset.Description,
		)
		ops = &redTeamOps{inner: ops, preset: preset, log: logger, nc: nc}
	}

	coord := &watchdogCoord{}
	mainCtx, cancelMain := context.WithCancel(context.Background())

	sigCtx, stopSig := signal.NotifyContext(
		context.Background(), syscall.SIGTERM, syscall.SIGINT,
	)
	go func() {
		<-sigCtx.Done()
		if coord.isRollingBack() {
			logger.Info(
				"Signal received during rollback; deferring until rollback completes",
			)
			coord.onSignalDuringRollback()
			stopSig()
			return
		}
		logger.Info("Signal received; shutting down")
		stopSig()
		cancelMain()
	}()

	w := &watchdog{
		cfg:     cfg,
		nc:      nc,
		ops:     ops,
		coord:   coord,
		limiter: newAlertLimiter(cfg.AlertCooldownSeconds),
		log:     logger,
	}
	w.tracker = extractTracker(ops)

	w.run(mainCtx)
}

func extractTracker(ops sysOps) *channelTracker {
	switch v := ops.(type) {
	case *realOps:
		return v.tracker
	case *dryRunOps:
		return extractTracker(v.inner)
	case *redTeamOps:
		return extractTracker(v.inner)
	default:
		return nil
	}
}
