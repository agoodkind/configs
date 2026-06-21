//go:build !linux

// Package main provides the mwan CLI entrypoint.
package main

import (
	"errors"

	"goodkind.io/mwan/internal/config"
)

// runIfMgr returns an error on non-Linux platforms. The ifmgr daemon
// uses Linux-only netlink, ICMPv6, and /proc/sys facilities.
func runIfMgr(_ *config.Config) error {
	return errors.New("ifmgr subcommand is Linux-only")
}
