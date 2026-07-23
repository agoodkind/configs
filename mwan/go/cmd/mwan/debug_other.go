//go:build !linux

// Package main provides the mwan command-line entrypoint.
package main

import (
	"fmt"
	"os"

	"goodkind.io/mwan/internal/config"
)

// runDebug is Linux-only because its inspection views read Linux netlink state.
func runDebug(_ []string, _ *config.Config) int {
	fmt.Fprintln(os.Stderr, "mwan debug: Linux-only")
	return 1
}
