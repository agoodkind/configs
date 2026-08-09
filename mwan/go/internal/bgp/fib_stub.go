//go:build !linux

package bgp

import (
	"context"
	"net/netip"
)

// PathEvent is the accepted best-path change received from a BGP peer.
type PathEvent struct {
	Peer      string
	Prefix    netip.Prefix
	NextHop   netip.Addr
	Withdrawn bool
}

// FIB is a no-op outside Linux because the netif backend is Linux-only.
type FIB struct{}

// ArmSweep is a no-op outside Linux.
func (*FIB) ArmSweep() {}

// Apply accepts path changes without mutating unsupported platforms.
func (*FIB) Apply(_ context.Context, _ PathEvent) error {
	return nil
}

// SweepStale leaves unsupported platforms unchanged.
func (*FIB) SweepStale(_ context.Context) error {
	return nil
}
