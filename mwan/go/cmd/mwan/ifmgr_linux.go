//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/logging"
	"goodkind.io/mwan/internal/version"

	// Side-effect imports: each module package's init() registers itself
	// with the ifmgr registry. Roles are resolved by name in roles.go.
	_ "goodkind.io/mwan/internal/ifmgr/modules/bridgeprobe"
	_ "goodkind.io/mwan/internal/ifmgr/modules/cloudflaredtap"
	_ "goodkind.io/mwan/internal/ifmgr/modules/connprobe"
	_ "goodkind.io/mwan/internal/ifmgr/modules/mainv4"
	_ "goodkind.io/mwan/internal/ifmgr/modules/oobv4"
	_ "goodkind.io/mwan/internal/ifmgr/modules/oobv6"
	_ "goodkind.io/mwan/internal/ifmgr/modules/policyrules"
	_ "goodkind.io/mwan/internal/ifmgr/modules/ralost"
	_ "goodkind.io/mwan/internal/ifmgr/modules/slaachealth"
	_ "goodkind.io/mwan/internal/ifmgr/modules/wghealth"
)

// runIfMgr is the entry point for the `mwan ifmgr` subcommand.
// Parses flags, builds a slog logger with the email handler chain, and
// hands off to ifmgr.Daemon.
func runIfMgr(cfg *config.Config) error {
	flags := parseIfMgrFlags()

	role := cfg.IfMgr.Role
	if flags.role != "" {
		role = flags.role
	}
	if role == "" {
		return fmt.Errorf("ifmgr: role required (set [ifmgr].role in config or pass --role)")
	}

	logger, err := buildIfMgrLogger(cfg, flags.debug)
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	logger.Info("ifmgr: starting",
		"build", version.BuildVersionString(),
		"role", role,
		"dry_run", flags.dryRun,
		"known_roles", ifmgr.KnownRoles(),
	)

	dcfg, err := buildIfMgrDaemonConfig(cfg, role)
	if err != nil {
		return fmt.Errorf("build daemon config: %w", err)
	}

	d, err := ifmgr.NewDaemon(logger, dcfg)
	if err != nil {
		return fmt.Errorf("new daemon: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := d.Run(ctx); err != nil {
		return fmt.Errorf("ifmgr daemon: %w", err)
	}
	logShutdownReason(ctx, logger)
	return nil
}

type ifmgrFlags struct {
	role   string
	debug  bool
	dryRun bool
}

func parseIfMgrFlags() ifmgrFlags {
	fs := flag.NewFlagSet("ifmgr", flag.ContinueOnError)
	role := fs.String("role", "", "ifmgr role (overrides cfg.IfMgr.Role; valid: see --help)")
	debug := fs.Bool("debug", false, "enable DEBUG logging")
	dryRun := fs.Bool("dry-run", false, "log mutating ops instead of applying (TODO: not yet plumbed to netif)")
	_ = fs.Parse(os.Args[1:])
	return ifmgrFlags{role: *role, debug: *debug, dryRun: *dryRun}
}

func buildIfMgrLogger(cfg *config.Config, debug bool) (*slog.Logger, error) {
	lc := logging.Config{
		TextLogFile: cfg.IfMgr.LogFile,
		JSONLogFile: cfg.IfMgr.JSONLogFile,
	}
	if cfg.Email.SMTP2GOAPIKey != "" && cfg.Email.AlertEmail != "" {
		sender := email.NewSender(
			cfg.Email.SMTP2GOAPIKey, cfg.Email.From,
			cfg.Email.BindIface, "mwan-ifmgr",
			slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
		)
		lc.EmailSend = sender.Send
		lc.EmailTo = cfg.Email.AlertEmail
		lc.EmailMinLevel = cfg.Email.MinLevel
		if lc.EmailMinLevel == "" {
			lc.EmailMinLevel = "WARN"
		}
		lc.EmailCooldown = cfg.Email.Cooldown
	}
	logger, err := logging.New(lc, version.BuildVersionString())
	if err != nil {
		return nil, err
	}
	if debug || cfg.IfMgr.Debug {
		logger.Info("ifmgr: debug logging enabled")
	}
	return logger, nil
}

// logShutdownReason logs the signal that triggered ctx cancellation.
func logShutdownReason(ctx context.Context, log *slog.Logger) {
	if err := ctx.Err(); err != nil {
		log.Info("ifmgr: shutdown", "reason", err.Error())
	}
}

// buildIfMgrDaemonConfig translates cfg.IfMgr (the parsed TOML) into the
// shape ifmgr.Daemon expects. Per-module sub-configs are passed through
// as-is (map[string]any) so the modules' constructors do their own
// strict parsing.
func buildIfMgrDaemonConfig(cfg *config.Config, role string) (ifmgr.DaemonConfig, error) {
	ifaceName := ""
	enableDHCP := false
	enableRA := false
	var dhcpInit, dhcpMax time.Duration

	// The [ifmgr.iface.<name>] section names the iface and toggles DHCP/RA.
	// We expect exactly one iface per ifmgr instance today.
	for name, iface := range cfg.IfMgr.Iface {
		if ifaceName != "" {
			return ifmgr.DaemonConfig{}, fmt.Errorf(
				"ifmgr: multi-iface not supported yet (saw %q and %q)",
				ifaceName, name)
		}
		ifaceName = iface.Name
		if ifaceName == "" {
			ifaceName = name
		}
		enableDHCP = iface.DHCPv4
		enableRA = iface.RASolicit
		if iface.DHCPInitialBackoff != "" {
			d, err := time.ParseDuration(iface.DHCPInitialBackoff)
			if err != nil {
				return ifmgr.DaemonConfig{}, fmt.Errorf("ifmgr.iface.%s.dhcp_initial_backoff: %w", name, err)
			}
			dhcpInit = d
		}
		if iface.DHCPMaxBackoff != "" {
			d, err := time.ParseDuration(iface.DHCPMaxBackoff)
			if err != nil {
				return ifmgr.DaemonConfig{}, fmt.Errorf("ifmgr.iface.%s.dhcp_max_backoff: %w", name, err)
			}
			dhcpMax = d
		}
	}
	if ifaceName == "" {
		return ifmgr.DaemonConfig{}, fmt.Errorf("ifmgr: no [ifmgr.iface.<name>] section found")
	}

	rec := 60 * time.Second
	if cfg.IfMgr.ReconcileInterval != "" {
		d, err := time.ParseDuration(cfg.IfMgr.ReconcileInterval)
		if err != nil {
			return ifmgr.DaemonConfig{}, fmt.Errorf("ifmgr.reconcile_interval: %w", err)
		}
		rec = d
	}

	return ifmgr.DaemonConfig{
		Role:              role,
		Iface:             ifaceName,
		ReconcileInterval: rec,
		EnableDHCP:        enableDHCP,
		DHCPInitial:       dhcpInit,
		DHCPMax:           dhcpMax,
		EnableRA:          enableRA,
		AlertRepeatEvery:  30 * time.Minute,
		ModuleConfigs:     cfg.IfMgr.Modules,
	}, nil
}
