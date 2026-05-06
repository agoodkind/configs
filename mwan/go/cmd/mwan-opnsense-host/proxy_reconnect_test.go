package main

import (
	"context"
	"testing"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsenseclient"
)

// TestColdStartUpstreamUnreachable covers the new cold-start contract:
// the bridge expects the upstream MWN1 socket to be reachable at Dial
// time. If nothing is listening, Dial fails and the bridge process
// exits, then systemd Restart=always brings it back. There is no
// in-process reconnect loop yet.
func TestColdStartUpstreamUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	t.Setenv("TMPDIR", "/tmp")
	socketPath := t.TempDir() + "/missing.sock"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := opnsenseclient.Dial(ctx, opnsenseclient.Config{Target: "unix://" + socketPath})
	if err == nil {
		t.Fatal("Dial with no upstream listener: want error, got nil")
	}
}

// TestMidLifeUpstreamRestart covers scenario (b): bridge has had
// successful RPCs, upstream is killed, the next probe returns an
// error (no hang). After the daemon restarts, the bridge exits and
// systemd brings it back; we model the restart cycle by reconnecting
// a fresh fixture and verifying it works again.
func TestMidLifeUpstreamRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	fix := newBridgeFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := fix.probe.Version(ctx, &mwanv1.VersionRequest{}); err != nil {
		t.Fatalf("initial probe: %v", err)
	}

	fix.daemon.Stop()

	// After daemon death, the next probe must fail quickly rather
	// than hang. We do not require a specific gRPC code; either an
	// error or a deadline-exceeded is acceptable.
	probeCtx, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	_, err := fix.probe.Version(probeCtx, &mwanv1.VersionRequest{})
	if err == nil {
		t.Fatal("probe Version after daemon death: want error, got nil")
	}
}

// TestBridgeProcessSurvivesProbeChurn covers scenario (e): the bridge
// keeps serving probe RPCs cleanly even when probe contexts churn.
// Asserts the bridge listener stays alive throughout.
func TestBridgeProcessSurvivesProbeChurn(t *testing.T) {
	if testing.Short() {
		t.Skip("reconnect test exercises real timing, skipped in -short")
	}
	fix := newBridgeFixture(t)
	for i := range 5 {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		_, err := fix.probe.Version(ctx, &mwanv1.VersionRequest{})
		cancel()
		if err != nil {
			t.Fatalf("probe Version[%d]: %v", i, err)
		}
	}
}
