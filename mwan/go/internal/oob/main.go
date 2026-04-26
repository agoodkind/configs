//go:build linux

package oob

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
	"goodkind.io/mwan/internal/logging"
	"goodkind.io/mwan/internal/version"
)

// oobFlags holds command-line flags for the oob subcommand.
type oobFlags struct {
	dryRun bool
	debug  bool
}

// Run is the entry point for `mwan oob`. It parses flags, builds a logger
// (with email handler chained in), constructs the Daemon, and blocks until
// SIGINT/SIGTERM cancels its context.
func Run(cfg *config.Config) error {
	f := parseFlags()

	// CLI flag overrides config-file setting.
	dryRun := cfg.OOB.DryRun || f.dryRun
	debug := cfg.OOB.Debug || f.debug || os.Getenv("MWAN_DEBUG_LOGGING") != ""

	if err := config.Validate(cfg, "oob", dryRun); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	logger, err := buildLogger(cfg, debug)
	if err != nil {
		return fmt.Errorf("logger init: %w", err)
	}
	logger.Info("mwan oob starting",
		"version", version.BuildVersionString(),
		"dry_run", dryRun,
		"debug", debug,
	)

	dcfg, err := buildDaemonConfig(cfg)
	if err != nil {
		return fmt.Errorf("build daemon config: %w", err)
	}

	runner := NewExecIPRunner(logger, dryRun)
	daemon := NewDaemon(runner, logger, dcfg)

	ctx, stop := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer stop()

	go logShutdownReason(ctx, logger)

	return daemon.Run(ctx)
}

// parseFlags parses command-line flags for the oob subcommand.
func parseFlags() oobFlags {
	dryRun := flag.Bool(
		"dry-run", false,
		"Log mutating ip commands without applying; useful before live cutover",
	)
	debug := flag.Bool(
		"debug", false,
		"Enable verbose debug logging (also via MWAN_DEBUG_LOGGING=1 or [oob] debug = true)",
	)
	flag.Parse()
	return oobFlags{dryRun: *dryRun, debug: *debug}
}

// buildLogger constructs the unified logger for the daemon. Mirrors the
// pattern in internal/watchdog/main.go so OOB events flow into the same
// channels (journal/files/email) as the rest of mwan.
func buildLogger(cfg *config.Config, debug bool) (*slog.Logger, error) {
	lc := logging.Config{
		TextLogFile: cfg.OOB.LogFile,
		JSONLogFile: cfg.OOB.JSONLogFile,
	}
	if cfg.Email.SMTP2GOAPIKey != "" && cfg.Email.AlertEmail != "" {
		emailSender := email.NewSender(
			cfg.Email.SMTP2GOAPIKey, cfg.Email.From,
			cfg.Email.BindIface, "mwan-oob",
			slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
		)
		lc.EmailSend = emailSender.Send
		lc.EmailTo = cfg.Email.AlertEmail
		lc.EmailMinLevel = cfg.Email.MinLevel
		if lc.EmailMinLevel == "" {
			// OOB defaults higher than ERROR-only watchdog default; we want
			// WARN-and-above to email.
			lc.EmailMinLevel = "WARN"
		}
		lc.EmailCooldown = cfg.Email.Cooldown
	}
	logger, err := logging.New(lc, version.BuildVersionString())
	if err != nil {
		return nil, err
	}
	if debug {
		// The factory's stdout handler already runs at LevelDebug. Just
		// note the choice so it's visible in journal.
		logger.Info("oob: debug logging enabled")
	}
	return logger, nil
}

// buildDaemonConfig converts the user-facing TOML section into the daemon's
// strongly-typed internal config. Validation has already run.
func buildDaemonConfig(cfg *config.Config) (DaemonConfig, error) {
	rec, err := time.ParseDuration(cfg.OOB.ReconcileInterval)
	if err != nil {
		return DaemonConfig{}, fmt.Errorf("parse reconcile_interval: %w", err)
	}
	raLost, err := time.ParseDuration(cfg.OOB.RALostAlertAfter)
	if err != nil {
		return DaemonConfig{}, fmt.Errorf("parse ra_lost_alert_after: %w", err)
	}
	dhcpInit, err := time.ParseDuration(cfg.OOB.DHCPInitialBackoff)
	if err != nil {
		return DaemonConfig{}, fmt.Errorf("parse dhcp_initial_backoff: %w", err)
	}
	dhcpMax, err := time.ParseDuration(cfg.OOB.DHCPMaxBackoff)
	if err != nil {
		return DaemonConfig{}, fmt.Errorf("parse dhcp_max_backoff: %w", err)
	}

	uidRange := strconv.Itoa(cfg.OOB.CloudflaredUID) + "-" + strconv.Itoa(cfg.OOB.CloudflaredUID)
	srcAddr := stripPrefix(cfg.OOB.OOBV6Addr) // "3d06:bad:b01:ff::1/128" -> "3d06:bad:b01:ff::1"

	return DaemonConfig{
		ReconcileInterval: rec,
		V6: V6Config{
			Iface:    cfg.OOB.MbrainsIface,
			OOBAddr:  cfg.OOB.OOBV6Addr,
			OOBTable: cfg.OOB.OOBTableName,
		},
		V4: V4Config{
			Iface:    cfg.OOB.MbrainsIface,
			OOBTable: cfg.OOB.OOBTableName,
		},
		Alerts: AlertConfig{
			RALostAfter:      raLost,
			V4LeaseLostAfter: 30 * time.Minute, // default, configurable later
			RepeatEvery:      30 * time.Minute,
		},
		DHCP: DHCPConfig{
			Iface:          cfg.OOB.MbrainsIface,
			InitialBackoff: dhcpInit,
			MaxBackoff:     dhcpMax,
		},
		Rules: []DesiredRule{
			{
				Family:   "inet6",
				Priority: cfg.OOB.OOBUIDRulePriority,
				UIDRange: uidRange,
				Table:    cfg.OOB.OOBTableName,
			},
			{
				Family:   "inet6",
				Priority: cfg.OOB.OOBSrcRulePriority,
				From:     srcAddr,
				Table:    cfg.OOB.OOBTableName,
			},
			// IPv4 uid rule too: cloudflared-oob may send IPv4 traffic (e.g.
			// to Cloudflare edge IPv4 addresses if IPv6 is briefly down).
			{
				Family:   "inet",
				Priority: cfg.OOB.OOBUIDRulePriority,
				UIDRange: uidRange,
				Table:    cfg.OOB.OOBTableName,
			},
		},
	}, nil
}

// stripPrefix returns the address half of a CIDR, or the input unchanged
// if no slash is present.
func stripPrefix(cidr string) string {
	for i, c := range cidr {
		if c == '/' {
			return cidr[:i]
		}
	}
	return cidr
}

// logShutdownReason logs the signal that triggered ctx cancellation so the
// journal records why the daemon exited.
func logShutdownReason(ctx context.Context, log *slog.Logger) {
	<-ctx.Done()
	log.Info("oob: shutdown signal received", "err", ctx.Err())
}
