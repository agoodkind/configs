//go:build linux

package bgp

import (
	"context"
	"net/netip"
	"testing"

	"goodkind.io/mwan/internal/netif"

	"github.com/osrg/gobgp/v4/pkg/apiutil"
	bgppkt "github.com/osrg/gobgp/v4/pkg/packet/bgp"
)

func TestBestPathWatchInstallsAllowedPathAndRejectsUndeclaredPath(t *testing.T) {
	t.Parallel()

	fake := newFakeBGPServer()
	router := Router{
		Name:          "router-2",
		AddressV4:     "10.250.250.5",
		AddressV6:     "3d06:bad:b01:fe::5",
		AllocationsV6: []netip.Prefix{netip.MustParsePrefix("3d06:bad:b01:4::/62")},
	}
	cfg := baseGRConfig(true)
	cfg.Neighbors = nil
	cfg.NeighborsV6 = nil
	cfg.Routers = []Router{router}
	writer := &recordedRouteWriter{routes: make(map[int][]netif.CurrentRoute)}
	fib := newFIB(FIBConfig{Tables: []int{100}, InternalIface: "vmbr250", Shadow: false}, testFIBLogger(), writer)
	speaker := newSpeakerWithFake(cfg, fake)
	speaker.SetFIB(fib)
	if err := speaker.Start(context.Background()); err != nil {
		t.Fatalf("start speaker: %v", err)
	}

	allowed := netip.MustParsePrefix("3d06:bad:b01:4::/64")
	fake.emitBestPaths([]*apiutil.Path{watchPath(t, router.AddressV6, allowed, false)})
	if got, want := len(writer.installs), 1; got != want {
		t.Fatalf("allowed path install count = %d, want %d", got, want)
	}
	if got, want := writer.installs[0].Dest, allowed.String(); got != want {
		t.Fatalf("allowed path destination = %s, want %s", got, want)
	}

	rejected := netip.MustParsePrefix("3d06:bad:b01::/64")
	fake.emitBestPaths([]*apiutil.Path{watchPath(t, router.AddressV6, rejected, false)})
	if got, want := len(writer.installs), 1; got != want {
		t.Fatalf("rejected path install count = %d, want %d", got, want)
	}
}

func TestPeerDownWithdrawsOnlyThatRouterAllocations(t *testing.T) {
	t.Parallel()

	fake := newFakeBGPServer()
	router := Router{
		Name:          "router-2",
		AddressV4:     "10.250.250.5",
		AddressV6:     "3d06:bad:b01:fe::5",
		AllocationsV6: []netip.Prefix{netip.MustParsePrefix("3d06:bad:b01:4::/62")},
	}
	cfg := baseGRConfig(true)
	cfg.Neighbors = nil
	cfg.NeighborsV6 = nil
	cfg.Routers = []Router{router}
	writer := &recordedRouteWriter{routes: make(map[int][]netif.CurrentRoute)}
	fib := newFIB(FIBConfig{Tables: []int{100}, InternalIface: "vmbr250", Shadow: false}, testFIBLogger(), writer)
	speaker := newSpeakerWithFake(cfg, fake)
	speaker.SetFIB(fib)
	if err := speaker.Start(context.Background()); err != nil {
		t.Fatalf("start speaker: %v", err)
	}

	prefix := netip.MustParsePrefix("3d06:bad:b01:4::/64")
	fake.emitBestPaths([]*apiutil.Path{watchPath(t, router.AddressV6, prefix, false)})
	fake.emitPeerUpdate(peerStateEvent(router.AddressV6, bgppkt.BGP_FSM_ESTABLISHED))
	fake.emitPeerUpdate(peerStateEvent(router.AddressV6, bgppkt.BGP_FSM_IDLE))

	if got, want := len(writer.deletes), 1; got != want {
		t.Fatalf("peer-down delete count = %d, want %d", got, want)
	}
	if got, want := writer.deletes[0].Dest, prefix.String(); got != want {
		t.Fatalf("peer-down deleted destination = %s, want %s", got, want)
	}
}
