//go:build !linux

package main

import (
	"fmt"
	"os"
)

// runTraceBoot only has a Linux implementation; the boot trace id belongs to
// the MWAN VM.
func runTraceBoot() int {
	fmt.Fprintln(os.Stderr, "mwan trace-boot: requires Linux")
	return 1
}
