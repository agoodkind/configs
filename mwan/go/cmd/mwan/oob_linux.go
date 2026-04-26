//go:build linux

package main

import (
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/oob"
)

// runOOB dispatches to the oob daemon on Linux. Defined per-OS so the
// mwan binary still cross-compiles to darwin (the oob package itself uses
// Linux-only syscalls and raw sockets).
func runOOB(cfg *config.Config) error {
	return oob.Run(cfg)
}
