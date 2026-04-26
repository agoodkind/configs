//go:build !linux

package main

import (
	"errors"

	"goodkind.io/mwan/internal/config"
)

// runOOB returns a clear error on non-Linux. The oob daemon uses kernel
// netlink, the `ip` command, raw sockets for DHCPv4, and rdisc6, all of
// which are Linux-specific.
func runOOB(_ *config.Config) error {
	return errors.New("oob subcommand is Linux-only (uses ip/netlink/rdisc6/raw sockets)")
}
