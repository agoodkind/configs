//go:build linux

package bgp

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"goodkind.io/mwan/internal/netif"

	"github.com/osrg/gobgp/v4/pkg/apiutil"
	bgppkt "github.com/osrg/gobgp/v4/pkg/packet/bgp"
)

func TestBestPathWatchInstallsRemotePathAndSkipsLocalPath(t *testing.T) {
	t.Parallel()

	fake := newFakeBGPServer()
	cfg := baseGRConfig(true)
	writer := &recordedRouteWriter{routes: make(map[int][]netif.CurrentRoute)}
	fib := newFIB(FIBConfig{Tables: []int{100}, InternalIface: "vmbr250", Shadow: false}, testFIBLogger(), writer)
	speaker := newSpeakerWithFake(cfg, fake)
	speaker.SetFIB(fib)
	if err := speaker.Start(context.Background()); err != nil {
		t.Fatalf("start speaker: %v", err)
	}

	peer := "3d06:bad:b01:fe::5"
	remote := netip.MustParsePrefix("3d06:bad:b01:4::/64")
	fake.emitBestPaths([]*apiutil.Path{watchPath(t, peer, remote, false)})
	if got, want := len(writer.installs), 1; got != want {
		t.Fatalf("remote path install count = %d, want %d", got, want)
	}
	if got, want := writer.installs[0].Dest, remote.String(); got != want {
		t.Fatalf("remote path destination = %s, want %s", got, want)
	}

	local := watchPath(t, peer, netip.MustParsePrefix("3d06:bad:b01:8::/64"), false)
	local.PeerAddress = netip.Addr{}
	fake.emitBestPaths([]*apiutil.Path{local})
	if got, want := len(writer.installs), 1; got != want {
		t.Fatalf("local path install count = %d, want %d", got, want)
	}
}

func TestPeerDownWithdrawsTrackedRoutes(t *testing.T) {
	t.Parallel()

	fake := newFakeBGPServer()
	cfg := baseGRConfig(true)
	writer := &recordedRouteWriter{routes: make(map[int][]netif.CurrentRoute)}
	fib := newFIB(FIBConfig{Tables: []int{100}, InternalIface: "vmbr250", Shadow: false}, testFIBLogger(), writer)
	speaker := newSpeakerWithFake(cfg, fake)
	speaker.SetFIB(fib)
	if err := speaker.Start(context.Background()); err != nil {
		t.Fatalf("start speaker: %v", err)
	}

	prefix := netip.MustParsePrefix("3d06:bad:b01:4::/64")
	peer := "3d06:bad:b01:fe::5"
	fake.emitBestPaths([]*apiutil.Path{watchPath(t, peer, prefix, false)})
	fake.emitPeerUpdate(peerStateEvent(peer, bgppkt.BGP_FSM_IDLE))

	if got, want := len(writer.deletes), 1; got != want {
		t.Fatalf("peer-down delete count = %d, want %d", got, want)
	}
	if got, want := writer.deletes[0].Dest, prefix.String(); got != want {
		t.Fatalf("peer-down deleted destination = %s, want %s", got, want)
	}
}

type capturedSweepTimer struct {
	stopped bool
}

func (t *capturedSweepTimer) Stop() bool {
	t.stopped = true
	return true
}

type scheduledSweepArm struct {
	delay    time.Duration
	callback func()
	timer    *capturedSweepTimer
}

func captureSweepArms(speaker *Speaker) *[]scheduledSweepArm {
	scheduled := make([]scheduledSweepArm, 0, 2)
	speaker.afterFunc = func(delay time.Duration, callback func()) sweepTimer {
		timer := &capturedSweepTimer{}
		scheduled = append(scheduled, scheduledSweepArm{
			delay:    delay,
			callback: callback,
			timer:    timer,
		})
		return timer
	}
	return &scheduled
}

func TestStaleSweepDoesNotArmBeforePeerEstablishes(t *testing.T) {
	fake := newFakeBGPServer()
	cfg := baseGRConfig(true)
	stale := netif.CurrentRoute{Dest: "3d06:bad:b01:8::/64", Via: "3d06:bad:b01:fe::5", Dev: "vmbr250", Metric: 0}
	writer := &recordedRouteWriter{routes: map[int][]netif.CurrentRoute{100: {stale}}}
	fib := newFIB(FIBConfig{Tables: []int{100}, InternalIface: "vmbr250", Shadow: false}, testFIBLogger(), writer)
	speaker := newSpeakerWithFake(cfg, fake)
	speaker.SetFIB(fib)
	scheduled := captureSweepArms(speaker)

	if err := speaker.Start(context.Background()); err != nil {
		t.Fatalf("start speaker: %v", err)
	}
	if got, want := len(*scheduled), 1; got != want {
		t.Fatalf("startup sweep timer count = %d, want %d", got, want)
	}
	if got, want := (*scheduled)[0].delay, staleSweepStartupGraceDelay; got != want {
		t.Fatalf("startup grace sweep delay = %s, want %s", got, want)
	}
	if err := fib.SweepStale(context.Background()); err != nil {
		t.Fatalf("sweep before peer establishment: %v", err)
	}
	if got := len(writer.deletes); got != 0 {
		t.Fatalf("pre-establishment stale delete count = %d, want 0", got)
	}
	if got, want := len(*scheduled), 1; got != want {
		t.Fatalf("pre-establishment sweep timer count = %d, want %d", got, want)
	}
}

func TestFirstEstablishedPeerDoesNotArmStaleSweepEarly(t *testing.T) {
	fake := newFakeBGPServer()
	cfg := baseGRConfig(true)
	stale := netif.CurrentRoute{Dest: "3d06:bad:b01:8::/64", Via: "3d06:bad:b01:fe::5", Dev: "vmbr250", Metric: 0}
	writer := &recordedRouteWriter{routes: map[int][]netif.CurrentRoute{100: {stale}}}
	fib := newFIB(FIBConfig{Tables: []int{100}, InternalIface: "vmbr250", Shadow: false}, testFIBLogger(), writer)
	speaker := newSpeakerWithFake(cfg, fake)
	speaker.SetFIB(fib)
	scheduled := captureSweepArms(speaker)

	if err := speaker.Start(context.Background()); err != nil {
		t.Fatalf("start speaker: %v", err)
	}
	peer := "3d06:bad:b01:fe::5"
	fake.emitPeerUpdate(peerStateEvent(peer, bgppkt.BGP_FSM_ESTABLISHED))
	if got, want := len(*scheduled), 1; got != want {
		t.Fatalf("peer-established sweep timer count = %d, want %d", got, want)
	}
	if (*scheduled)[0].timer.stopped {
		t.Fatal("first peer establishment stopped the startup grace timer")
	}
	if err := fib.SweepStale(context.Background()); err != nil {
		t.Fatalf("sweep before startup grace: %v", err)
	}
	if got, want := len(writer.deletes), 0; got != want {
		t.Fatalf("pre-grace stale delete count = %d, want %d", got, want)
	}

	(*scheduled)[0].callback()
	if err := fib.SweepStale(context.Background()); err != nil {
		t.Fatalf("sweep after startup grace: %v", err)
	}
	if got, want := len(writer.deletes), 1; got != want {
		t.Fatalf("post-grace stale delete count = %d, want %d", got, want)
	}
}

func TestStaleSweepArmsAfterStartupGraceWithoutPeerEstablishment(t *testing.T) {
	fake := newFakeBGPServer()
	cfg := baseGRConfig(true)
	stale := netif.CurrentRoute{Dest: "3d06:bad:b01:8::/64", Via: "3d06:bad:b01:fe::5", Dev: "vmbr250", Metric: 0}
	writer := &recordedRouteWriter{routes: map[int][]netif.CurrentRoute{100: {stale}}}
	fib := newFIB(FIBConfig{Tables: []int{100}, InternalIface: "vmbr250", Shadow: false}, testFIBLogger(), writer)
	speaker := newSpeakerWithFake(cfg, fake)
	speaker.SetFIB(fib)
	scheduled := captureSweepArms(speaker)

	if err := speaker.Start(context.Background()); err != nil {
		t.Fatalf("start speaker: %v", err)
	}
	if got, want := len(*scheduled), 1; got != want {
		t.Fatalf("startup grace sweep timer count = %d, want %d", got, want)
	}
	(*scheduled)[0].callback()
	if err := fib.SweepStale(context.Background()); err != nil {
		t.Fatalf("sweep after startup grace: %v", err)
	}
	if got, want := len(writer.deletes), 1; got != want {
		t.Fatalf("post-grace stale delete count = %d, want %d", got, want)
	}
}
