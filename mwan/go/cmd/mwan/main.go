package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"goodkind.io/mwan/internal/agent"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/healthcheck"
	"goodkind.io/mwan/internal/version"
	"goodkind.io/mwan/internal/watchdog"
)

// subcommand is the typed enum of mwan subcommands that take a config.
// "health-check", "opnsense", "opnsense-probe", and "opnsense-host" are
// config-less and dispatched before this switch.
type subcommand string

const (
	subcmdAgent                subcommand = "agent"
	subcmdWatchdog             subcommand = "watchdog"
	subcmdIfmgr                subcommand = "ifmgr"
	subcmdHealthCheck          subcommand = "health-check"
	subcmdOPNsense             subcommand = "opnsense"
	subcmdOPNsenseHost         subcommand = "opnsense-host"
	subcmdOPNsenseProbe        subcommand = "opnsense-probe"
	subcmdOPNsenseImportConfig subcommand = "opnsense-import-config"
	subcmdOPNsenseUpgrade      subcommand = "opnsense-upgrade"
	subcmdOPNsenseValidate     subcommand = "opnsense-validate"
)

// dispatchResult describes how dispatchConfigLess handled a subcommand.
// handled=true means the subcommand ran (caller exits with code).
// handled=false means the caller should fall through to the config-loading path.
type dispatchResult struct {
	handled bool
	code    int
}

func main() {
	if invokedAsOPNsenseDaemon(os.Args[0]) {
		os.Exit(runOPNsenseDaemon(os.Args[1:]))
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mwan <agent|watchdog|health-check|ifmgr|opnsense|opnsense-probe|opnsense-host|opnsense-import-config|opnsense-upgrade|opnsense-validate> [flags]")
		os.Exit(1)
	}
	sub := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)

	// Boundary log: every mwan invocation is recorded with build identity
	// and the chosen subcommand, before subcommand-specific logger setup.
	// This is the single grep-anchor that links a binary on disk to the
	// session it executed in.
	slog.Info("mwan boundary",
		"build", version.BuildVersionString(),
		"subcommand", sub)

	if res := dispatchConfigLess(subcommand(sub)); res.handled {
		os.Exit(res.code)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan: %v\n", err)
		os.Exit(1)
	}

	if code := dispatchWithConfig(sub, subcommand(sub), cfg); code != 0 {
		os.Exit(code)
	}
}

// dispatchConfigLess handles subcommands that do not load mwan config.
func dispatchConfigLess(sub subcommand) dispatchResult {
	switch sub {
	case subcmdHealthCheck:
		if err := healthcheck.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "mwan health-check: %v\n", err)
			return dispatchResult{handled: true, code: 1}
		}
		return dispatchResult{handled: true, code: 0}
	case subcmdOPNsenseProbe:
		if err := runOPNsenseProbe(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "mwan opnsense-probe: %v\n", err)
			return dispatchResult{handled: true, code: 1}
		}
		return dispatchResult{handled: true, code: 0}
	case subcmdOPNsense:
		return dispatchResult{handled: true, code: runOPNsenseDaemon(os.Args[1:])}
	case subcmdOPNsenseHost:
		return dispatchResult{handled: true, code: runOPNsenseHost(os.Args[1:])}
	case subcmdOPNsenseImportConfig:
		if err := runOPNsenseImportConfig(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "mwan opnsense-import-config: %v\n", err)
			return dispatchResult{handled: true, code: 1}
		}
		return dispatchResult{handled: true, code: 0}
	case subcmdOPNsenseUpgrade:
		if err := runOPNsenseUpgrade(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "mwan opnsense-upgrade: %v\n", err)
			return dispatchResult{handled: true, code: 1}
		}
		return dispatchResult{handled: true, code: 0}
	case subcmdOPNsenseValidate:
		if err := runOPNsenseValidate(os.Args[1:]); err != nil {
			if !strings.Contains(err.Error(), "help requested") {
				fmt.Fprintf(os.Stderr, "mwan opnsense-validate: %v\n", err)
			}
			return dispatchResult{handled: true, code: 1}
		}
		return dispatchResult{handled: true, code: 0}
	case subcmdAgent, subcmdWatchdog, subcmdIfmgr:
		return dispatchResult{handled: false}
	}
	return dispatchResult{handled: false}
}

// dispatchWithConfig handles subcommands that need a loaded mwan config.
// Returns the exit code; 0 means success. The helper prints the error
// itself so wrapcheck doesn't fire on the cross-package error returns.
func dispatchWithConfig(rawSub string, sub subcommand, cfg *config.Config) int {
	var runErr error
	switch sub {
	case subcmdAgent:
		runErr = agent.Run(cfg)
	case subcmdWatchdog:
		if len(os.Args) > 1 && os.Args[1] == "failover" {
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			runErr = watchdog.FailoverRun(cfg)
		} else {
			runErr = watchdog.Run(cfg)
		}
	case subcmdIfmgr:
		runErr = runIfMgr(cfg)
	case subcmdHealthCheck, subcmdOPNsense, subcmdOPNsenseProbe, subcmdOPNsenseHost, subcmdOPNsenseImportConfig, subcmdOPNsenseUpgrade, subcmdOPNsenseValidate:
		fmt.Fprintf(os.Stderr, "internal dispatch error for subcommand %q\n", rawSub)
		return 1
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", rawSub)
		return 1
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "mwan %s: %v\n", rawSub, runErr)
		return 1
	}
	return 0
}

func invokedAsOPNsenseDaemon(argv0 string) bool {
	binaryName := filepath.Base(argv0)
	return binaryName == "mwan-opnsense" || strings.HasPrefix(binaryName, "mwan-opnsense.")
}
