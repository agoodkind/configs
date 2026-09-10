//go:build !linux

package main

import (
	"fmt"
	"os"
)

func dispatchHealth() dispatchResult {
	fmt.Fprintln(os.Stderr, "mwan health: health checks require Linux")
	return dispatchResult{handled: true, code: 1}
}
