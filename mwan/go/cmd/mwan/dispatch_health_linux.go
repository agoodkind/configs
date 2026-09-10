//go:build linux

package main

import (
	"fmt"
	"os"

	"goodkind.io/mwan/internal/healthcheck"
)

func dispatchHealth() dispatchResult {
	if err := healthcheck.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mwan health: %v\n", err)
		return dispatchResult{handled: true, code: 1}
	}
	return dispatchResult{handled: true, code: 0}
}
