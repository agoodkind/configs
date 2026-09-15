package main

import (
	"fmt"
	"os"

	"goodkind.io/mwan/internal/version"
)

// sysrepoVersionUnavailable is the libsysrepo field of a binary that does not
// link the sysrepo binding.
const sysrepoVersionUnavailable = "unavailable"

// runVersion implements `mwan version`. The deploy runs it on every binary it
// installs, so it reads only what is compiled into the binary and the binary
// file itself: no config, no socket, and no privilege.
func runVersion(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "usage: mwan version")
		return 2
	}
	fmt.Fprintf(os.Stdout, "version=%s %s libsysrepo=%s\n",
		version.BuildVersion(), version.BuildVersionString(), linkedSysrepoVersion())
	return 0
}
