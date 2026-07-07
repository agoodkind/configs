//go:build !linux

package main

import (
	"fmt"
	"os"
)

// runPDProbe is Linux-only. The pd package reads systemd-networkd over
// D-Bus and uses netlink and journald, none of which exist on the
// FreeBSD (mwan-opnsense) build.
func runPDProbe(_ []string) int {
	fmt.Fprintln(os.Stderr, "mwan pd: Linux-only")
	return 1
}
