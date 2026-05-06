package main

import (
	"fmt"
	"log/slog"
	"os"

	"goodkind.io/mwan/internal/agent"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/healthcheck"
	"goodkind.io/mwan/internal/version"
	"goodkind.io/mwan/internal/watchdog"
)

// subcommand is the typed enum of mwan subcommands that take a config.
// "health-check", "opnsense-probe", and "opnsense-host" are
// config-less and dispatched before this switch.
type subcommand string

const (
	subcmdAgent         subcommand = "agent"
	subcmdWatchdog      subcommand = "watchdog"
	subcmdIfmgr         subcommand = "ifmgr"
	subcmdHealthCheck   subcommand = "health-check"
	subcmdOPNsenseHost  subcommand = "opnsense-host"
	subcmdOPNsenseProbe subcommand = "opnsense-probe"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mwan <agent|watchdog|health-check|ifmgr|opnsense-probe|opnsense-host> [flags]")
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

	// Subcommands that don't need config
	switch subcommand(sub) {
	case subcmdHealthCheck:
		if err := healthcheck.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "mwan health-check: %v\n", err)
			os.Exit(1)
		}
		return
	case subcmdOPNsenseProbe:
		if err := runOPNsenseProbe(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "mwan opnsense-probe: %v\n", err)
			os.Exit(1)
		}
		return
	case subcmdOPNsenseHost:
		os.Exit(runOPNsenseHost(os.Args[1:]))
	case subcmdAgent, subcmdWatchdog, subcmdIfmgr:
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan: %v\n", err)
		os.Exit(1)
	}

	var runErr error
	switch subcommand(sub) {
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
	case subcmdHealthCheck, subcmdOPNsenseProbe, subcmdOPNsenseHost:
		fmt.Fprintf(os.Stderr, "internal dispatch error for subcommand %q\n", sub)
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", sub)
		os.Exit(1)
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "mwan %s: %v\n", sub, runErr)
		os.Exit(1)
	}
}
