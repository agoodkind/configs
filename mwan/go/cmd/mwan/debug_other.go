//go:build !linux

// Package main provides the mwan command-line entrypoint.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"goodkind.io/mwan/internal/config"
)

type nonLinuxDebugView string

const (
	nonLinuxDebugViewConnectivity nonLinuxDebugView = "connectivity"
	nonLinuxDebugViewCurl4        nonLinuxDebugView = "curl4"
	nonLinuxDebugViewCurl6        nonLinuxDebugView = "curl6"
	nonLinuxDebugViewLB4          nonLinuxDebugView = "lb4"
	nonLinuxDebugViewLB4Ifaces    nonLinuxDebugView = "lb4-ifaces"
	nonLinuxDebugViewLB6          nonLinuxDebugView = "lb6"
	nonLinuxDebugViewLB6Ifaces    nonLinuxDebugView = "lb6-ifaces"
	nonLinuxDebugViewPing4        nonLinuxDebugView = "ping4"
	nonLinuxDebugViewPing6        nonLinuxDebugView = "ping6"
)

// runDebug is Linux-only because its inspection views read Linux netlink state.
func runDebug(args []string, cfg *config.Config) int {
	if len(args) > 0 && isNonLinuxActiveDebugProbe(nonLinuxDebugView(args[0])) {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))
		err := runDebugProbeView(
			context.Background(),
			os.Stdout,
			logger,
			cfg,
			args[0],
			args[1:],
		)
		fmt.Fprintf(os.Stderr, "mwan debug %s: %v\n", args[0], err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "mwan debug: Linux-only")
	return 1
}

func isNonLinuxActiveDebugProbe(view nonLinuxDebugView) bool {
	switch view {
	case nonLinuxDebugViewConnectivity,
		nonLinuxDebugViewPing4,
		nonLinuxDebugViewPing6,
		nonLinuxDebugViewCurl4,
		nonLinuxDebugViewCurl6,
		nonLinuxDebugViewLB4,
		nonLinuxDebugViewLB6,
		nonLinuxDebugViewLB4Ifaces,
		nonLinuxDebugViewLB6Ifaces:
		return true
	default:
		return false
	}
}
