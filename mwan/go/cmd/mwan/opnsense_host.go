package main

import (
	"fmt"
	"os"

	"goodkind.io/mwan/internal/version"
)

// opnsenseHostUsage prints the host bridge subcommand usage.
func opnsenseHostUsage() int {
	fmt.Fprintln(os.Stderr, "usage: mwan opnsense-host {serve|version} [flags]")
	return 2
}

type opnsenseHostSubcommand string

const (
	opnsenseHostSubcmdVersion opnsenseHostSubcommand = "version"
	opnsenseHostSubcmdServe   opnsenseHostSubcommand = "serve"
	opnsenseHostSubcmdHelpH   opnsenseHostSubcommand = "-h"
	opnsenseHostSubcmdHelpL   opnsenseHostSubcommand = "--help"
	opnsenseHostSubcmdHelp    opnsenseHostSubcommand = "help"
)

func runOPNsenseHost(args []string) int {
	if len(args) < 1 {
		return opnsenseHostUsage()
	}
	sub := opnsenseHostSubcommand(args[0])
	subArgs := args[1:]
	switch sub {
	case opnsenseHostSubcmdVersion:
		fmt.Fprintln(os.Stdout, version.BuildVersionString())
		return 0
	case opnsenseHostSubcmdServe:
		return runOPNsenseHostServe(subArgs)
	case opnsenseHostSubcmdHelpH, opnsenseHostSubcmdHelpL, opnsenseHostSubcmdHelp:
		return opnsenseHostUsage()
	default:
		fmt.Fprintln(os.Stderr, "mwan opnsense-host: unknown subcommand")
		return opnsenseHostUsage()
	}
}
