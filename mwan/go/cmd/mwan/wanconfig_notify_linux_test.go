//go:build linux

package main

import (
	"log/slog"
	"testing"

	"goodkind.io/mwan/internal/wanstate"
)

// quietLogger keeps the notifier's drop warnings out of test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// TestSurfaceNotifier_RendersTheNotifications pins what a subscriber
// receives for the two transitions: the member is named by its interface
// as everywhere else in the tree, and both directions of each change are
// carried.
func TestSurfaceNotifier_RendersTheNotifications(t *testing.T) {
	t.Parallel()
	notifier := newSurfaceNotifier(quietLogger(), liveTestGateway())

	notifier.HealthTransition("att", wanstate.HealthHealthy, wanstate.HealthUnhealthy)
	notifier.TierChange(0, 1)

	health := <-notifier.queue
	if health.path != "/goodkind-mwan-steering:health-transition" {
		t.Fatalf("health path = %s", health.path)
	}
	wantHealth := map[string]string{
		"interface": "enatt0", "health": "unhealthy", "previous-health": "healthy",
	}
	assertItems(t, health, wantHealth)

	tier := <-notifier.queue
	if tier.path != "/goodkind-mwan-steering:tier-change" {
		t.Fatalf("tier path = %s", tier.path)
	}
	assertItems(t, tier, map[string]string{"active-tier": "1", "previous-tier": "0"})
}

func assertItems(t *testing.T, event surfaceEvent, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	for _, item := range event.items {
		got[item.Path] = item.Value
	}
	if len(got) != len(want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	for leaf, value := range want {
		if got[leaf] != value {
			t.Fatalf("leaf %s = %q, want %q", leaf, got[leaf], value)
		}
	}
}

// TestSurfaceNotifier_UnknownMemberAndFullQueue pins the two guard
// behaviors: a member outside the published gateway produces nothing, and
// a full queue drops the event instead of blocking the writer.
func TestSurfaceNotifier_UnknownMemberAndFullQueue(t *testing.T) {
	t.Parallel()
	notifier := newSurfaceNotifier(quietLogger(), liveTestGateway())

	notifier.HealthTransition("stranger", wanstate.HealthHealthy, wanstate.HealthUnhealthy)
	if len(notifier.queue) != 0 {
		t.Fatalf("unknown member queued %d events", len(notifier.queue))
	}

	for range notifyQueueDepth + 3 {
		notifier.TierChange(0, 1)
	}
	if len(notifier.queue) != notifyQueueDepth {
		t.Fatalf("queue holds %d events, want the cap %d", len(notifier.queue), notifyQueueDepth)
	}
}
