//go:build !linux

package main

import (
	"fmt"
	"os"
)

// runWanconfigSelftest is Linux-only. The yangpub binding links libyang and
// libsysrepo, which only the linux gateway carries; the FreeBSD
// (mwan-opnsense) build has no management surface to publish into, so the
// package is left out of that binary entirely rather than stubbed.
func runWanconfigSelftest(_ []string) int {
	fmt.Fprintln(os.Stderr, "mwan wanconfig-selftest: Linux-only")
	return 1
}
