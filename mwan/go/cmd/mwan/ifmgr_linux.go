//go:build linux

// Package main wires the Linux-only `mwan ifmgr` command entrypoint.
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
	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/logging"
	"goodkind.io/mwan/internal/notify"
	"goodkind.io/mwan/internal/tracing"
	"goodkind.io/mwan/internal/version"

	// Side-effect imports: each module package's init() registers itself
	// with the ifmgr registry. Roles are resolved by name in roles.go.
	_ "goodkind.io/mwan/internal/ifmgr/modules/bridgeprobe"
	_ "goodkind.io/mwan/internal/ifmgr/modules/cloudflaredtap"
	_ "goodkind.io/mwan/internal/ifmgr/modules/connprobe"
	_ "goodkind.io/mwan/internal/ifmgr/modules/hostipv6policy"
	_ "goodkind.io/mwan/internal/ifmgr/modules/mainv4"
	_ "goodkind.io/mwan/internal/ifmgr/modules/npt"
	_ "goodkind.io/mwan/internal/ifmgr/modules/oobv4"
	_ "goodkind.io/mwan/internal/ifmgr/modules/oobv6"
	_ "goodkind.io/mwan/internal/ifmgr/modules/policyrules"
	_ "goodkind.io/mwan/internal/ifmgr/modules/ralost"
	_ "goodkind.io/mwan/internal/ifmgr/modules/slaachealth"
	_ "goodkind.io/mwan/internal/ifmgr/modules/wanroutes"
	_ "goodkind.io/mwan/internal/ifmgr/modules/wg"
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

	logger := buildIfMgrLogger(cfg, flags.debug)
	runID := tracing.NewID()
	logger = logger.With(
		slog.String(tracing.RunIDKey, runID),
		slog.String(tracing.ComponentKey, "ifmgr"),
	)
	logger.Info(
		"ifmgr: starting",
		"build", version.BuildVersionString(),
		"role", role,
		"dry_run", flags.dryRun,
		"known_roles", ifmgr.KnownRoles(),
	)

	dcfg, err := buildIfMgrDaemonConfig(cfg, role)
	if err != nil {
		logger.Warn("ifmgr: build daemon config failed", "err", err)
		return fmt.Errorf("build daemon config: %w", err)
	}

	dcfg.Notifier = notify.FromConfig(cfg, logger, "mwan-ifmgr")

	d, err := ifmgr.NewDaemon(logger, dcfg)
	if err != nil {
		logger.Warn("ifmgr: new daemon failed", "err", err)
		return fmt.Errorf("new daemon: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := d.Run(ctx); err != nil {
		logger.WarnContext(ctx, "ifmgr: daemon run failed", "err", err)
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

func buildIfMgrLogger(cfg *config.Config, debug bool) *slog.Logger {
	handlers := []slog.Handler{logging.StdoutJSON()}
	if p := cfg.IfMgr.LogFile; p != "" {
		handlers = append(handlers, logging.FileText(p, "[mwan-ifmgr]"))
	}
	if p := cfg.IfMgr.JSONLogFile; p != "" {
		handlers = append(handlers, logging.FileJSON(p))
	}
	// Email flows through notify.Manager, constructed in runIfMgr via
	// notify.FromConfig, with per-(kind, key) state-change semantics.
	logger, _ := logging.New(logging.Config{
		BuildVersion: version.BuildVersionString(),
		Handlers:     handlers,
	})
	if debug || cfg.IfMgr.Debug {
		logger.Info("ifmgr: debug logging enabled")
	}
	return logger
}

// logShutdownReason logs the signal that triggered ctx cancellation.
func logShutdownReason(ctx context.Context, log *slog.Logger) {
	if err := ctx.Err(); err != nil {
		log.InfoContext(ctx, "ifmgr: shutdown", "reason", err.Error())
	}
}

// buildIfMgrDaemonConfig translates cfg.IfMgr (the parsed TOML) into the
// shape ifmgr.Daemon expects. Module configs are adapted from the explicit
// TOML schema into typed runtime configs before daemon startup.
func buildIfMgrDaemonConfig(cfg *config.Config, role string) (ifmgr.DaemonConfig, error) {
	logger := slog.Default().With("component", "ifmgr")
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
				ifaceName, name,
			)
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
				logger.Warn("ifmgr: invalid dhcp_initial_backoff",
					"iface", name, "value", iface.DHCPInitialBackoff, "err", err)
				return ifmgr.DaemonConfig{}, fmt.Errorf("ifmgr.iface.%s.dhcp_initial_backoff: %w", name, err)
			}
			dhcpInit = d
		}
		if iface.DHCPMaxBackoff != "" {
			d, err := time.ParseDuration(iface.DHCPMaxBackoff)
			if err != nil {
				logger.Warn("ifmgr: invalid dhcp_max_backoff",
					"iface", name, "value", iface.DHCPMaxBackoff, "err", err)
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
			logger.Warn("ifmgr: invalid reconcile_interval",
				"value", cfg.IfMgr.ReconcileInterval, "err", err)
			return ifmgr.DaemonConfig{}, fmt.Errorf("ifmgr.reconcile_interval: %w", err)
		}
		rec = d
	}

	moduleConfigs, err := buildIfMgrModuleConfigs(cfg.IfMgr.Modules, cfg.IfMgr.WAN, role)
	if err != nil {
		logger.Warn("ifmgr: build module configs failed", "role", role, "err", err)
		return ifmgr.DaemonConfig{}, err
	}

	// The repeat cadence is consumed directly by notify.FromConfig from
	// cfg.IfMgr.Alerts or cfg.Notify.
	return ifmgr.DaemonConfig{
		Role:              role,
		Iface:             ifaceName,
		ReconcileInterval: rec,
		EnableDHCP:        enableDHCP,
		DHCPInitial:       dhcpInit,
		DHCPMax:           dhcpMax,
		EnableRA:          enableRA,
		Notifier:          nil,
		ModuleConfigs:     moduleConfigs,
	}, nil
}
