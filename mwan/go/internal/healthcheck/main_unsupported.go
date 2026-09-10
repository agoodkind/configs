//go:build !linux

// Package healthcheck provides continuous connectivity testing with structured logging.
package healthcheck

import "fmt"

// Run reports that interface-bound health checks require Linux.
func Run() error {
	return fmt.Errorf("health checks require Linux")
}
