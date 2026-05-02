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

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mwan <agent|watchdog|health-check|ifmgr> [flags]")
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
	if sub == "health-check" {
		if err := healthcheck.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "mwan health-check: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan: %v\n", err)
		os.Exit(1)
	}

	var runErr error
	switch sub {
	case "agent":
		agent.Run(cfg)
	case "watchdog":
		if len(os.Args) > 1 && os.Args[1] == "failover" {
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			runErr = watchdog.FailoverRun(cfg)
		} else {
			runErr = watchdog.Run(cfg)
		}
	case "ifmgr":
		runErr = runIfMgr(cfg)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", sub)
		os.Exit(1)
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "mwan %s: %v\n", sub, runErr)
		os.Exit(1)
	}
}
