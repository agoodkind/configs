package main

import (
	"fmt"
	"log/slog"
	"os"

	"goodkind.io/mwan/internal/version"
)

func opnsenseDaemonUsage() int {
	fmt.Fprintln(os.Stderr, "usage: mwan opnsense {serve|version|status|is-enabled} [flags]")
	return 2
}

type opnsenseDaemonSubcommand string

const (
	opnsenseSubcmdVersion   opnsenseDaemonSubcommand = "version"
	opnsenseSubcmdServe     opnsenseDaemonSubcommand = "serve"
	opnsenseSubcmdStatus    opnsenseDaemonSubcommand = "status"
	opnsenseSubcmdIsEnabled opnsenseDaemonSubcommand = "is-enabled"
	opnsenseSubcmdHelpH     opnsenseDaemonSubcommand = "-h"
	opnsenseSubcmdHelpL     opnsenseDaemonSubcommand = "--help"
	opnsenseSubcmdHelp      opnsenseDaemonSubcommand = "help"
)

func runOPNsenseDaemon(args []string) int {
	if os.Getenv("MWAN_OPNSENSE_DAEMON_CHILD") == "1" {
		_ = os.Unsetenv("MWAN_OPNSENSE_DAEMON_CHILD")
	}
	if len(args) < 1 {
		return opnsenseDaemonUsage()
	}
	sub := opnsenseDaemonSubcommand(args[0])
	subArgs := args[1:]
	switch sub {
	case opnsenseSubcmdVersion:
		logOPNsenseDaemonBoundary("version")
		fmt.Fprintln(os.Stdout, version.BuildVersionString())
		return 0
	case opnsenseSubcmdServe:
		logOPNsenseDaemonBoundary("serve")
		return runOPNsenseDaemonServe(subArgs)
	case opnsenseSubcmdStatus:
		logOPNsenseDaemonBoundary("status")
		return runOPNsenseDaemonStatus(subArgs)
	case opnsenseSubcmdIsEnabled:
		logOPNsenseDaemonBoundary("is-enabled")
		return runOPNsenseDaemonIsEnabled(subArgs)
	case opnsenseSubcmdHelpH, opnsenseSubcmdHelpL, opnsenseSubcmdHelp:
		return opnsenseDaemonUsage()
	default:
		fmt.Fprintf(os.Stderr, "mwan opnsense: unknown subcommand %q\n", string(sub))
		return opnsenseDaemonUsage()
	}
}

func logOPNsenseDaemonBoundary(subcommandName string) {
	slog.Info("mwan opnsense boundary",
		"build", version.BuildVersionString(),
		"subcommand", subcommandName)
}
