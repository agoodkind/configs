package opnsensesvc

import (
	"context"
	"errors"
	"testing"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
)

// TestWatchdog_NoopWhenHealthy proves the watchdog leaves wedgeCount
// alone when the dispatcher is idle. The dispatcher receives no
// traffic so frameCh is empty on every tick, which keeps detectWedge
// in the no-op branch. The test runs for several intervals to give
// the watchdog plenty of opportunities to fire incorrectly before
// ctx cancel.
func TestWatchdog_NoopWhenHealthy(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, d, stop := startDispatcherWithWorkersAndRef(t, srv, 2)
	defer stop()

	// Resolve the dispatcher behind the client by sending one quick
	// RPC so the worker pool has popped at least one job (which
	// stamps lastFramePoppedNS) before the watchdog starts.
	resp := client.call(t, mwn1.MethodVersion, 800, &mwanv1.VersionRequest{})
	assertNoErrorFrame(t, resp)

	cfg := WatchdogConfig{
		Interval:       10 * time.Millisecond,
		IdleBudget:     50 * time.Millisecond,
		EscalateAfter:  3,
		EscalateWindow: time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := d.RunWatchdog(ctx, cfg); err != nil {
		t.Fatalf("watchdog should not escalate on healthy dispatcher: %v", err)
	}
	if got := d.wedgeCount.Load(); got != 0 {
		t.Fatalf("expected wedgeCount=0, got %d", got)
	}
}

// TestWatchdog_DetectsAndResetsWedge submits one slow Exec to occupy
// the single worker, queues a follow-up Exec that the slow handler
// holds in frameCh, and confirms the watchdog detects the stall and
// fires a reset. The dispatcher's wedgeCount is the externally
// observable signal that the reset actually happened.
func TestWatchdog_DetectsAndResetsWedge(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, d, stop := startDispatcherWithWorkersAndRef(t, srv, 1)
	defer stop()

	// Pop a fast job first so lastFramePoppedNS is initialized.
	resp := client.call(t, mwn1.MethodVersion, 810, &mwanv1.VersionRequest{})
	assertNoErrorFrame(t, resp)

	// Submit two long Exec jobs: the first occupies the worker, the
	// second sits in frameCh long enough for the watchdog to react.
	holdA := client.registerResponse(811)
	holdB := client.registerResponse(812)
	defer client.unregisterResponse(811)
	defer client.unregisterResponse(812)
	sendExecSleep(t, client, 811)
	sendExecSleep(t, client, 812)

	cfg := WatchdogConfig{
		Interval:       20 * time.Millisecond,
		IdleBudget:     80 * time.Millisecond,
		EscalateAfter:  0, // disable escalation for this test
		EscalateWindow: time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := d.RunWatchdog(ctx, cfg)
	if err != nil {
		t.Fatalf("watchdog should not escalate with EscalateAfter=0: %v", err)
	}
	if got := d.wedgeCount.Load(); got < 1 {
		t.Fatalf("expected wedgeCount>=1 after wedge, got %d", got)
	}

	// The in-flight Exec gets cancelled by the reset; drain its
	// response so the test client is clean for the next case.
	drainExecResponse(t, holdA)
	// holdB was drained out of frameCh without execution; it produces
	// no response.
	select {
	case unexpected := <-holdB:
		t.Fatalf("expected drained queue entry to produce no response, got %+v", unexpected)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestWatchdog_EscalatesAfterRepeatedResets keeps re-wedging the
// dispatcher across multiple watchdog cycles to confirm the
// escalation counter trips and the watchdog returns
// ErrWatchdogEscalated. The test runs an outer goroutine that
// resubmits Exec(sleep) jobs every time the watchdog clears the
// queue so the wedge condition keeps recurring.
func TestWatchdog_EscalatesAfterRepeatedResets(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, d, stop := startDispatcherWithWorkersAndRef(t, srv, 1)
	defer stop()

	resp := client.call(t, mwn1.MethodVersion, 820, &mwanv1.VersionRequest{})
	assertNoErrorFrame(t, resp)

	// Background goroutine keeps frameCh primed with slow Exec jobs
	// so the watchdog observes a wedge on every tick. The feeder
	// stops on stopFeed close and tolerates a closed connection
	// during teardown without failing the test.
	stopFeed := make(chan struct{})
	feederDone := make(chan struct{})
	go func() {
		defer close(feederDone)
		seq := uint64(900)
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopFeed:
				return
			case <-ticker.C:
			}
			ch := client.registerResponse(seq)
			payload, _, err := mwn1.MarshalRequest(client.reg, mwn1.MethodExec, &mwanv1.ExecRequest{
				Command:        "/bin/sleep",
				Args:           []string{"10"},
				TimeoutSeconds: 30,
			})
			if err != nil {
				return
			}
			if err := client.conn.SendMessage(mwn1.MethodExec, seq, mwn1.FlagRequest, payload); err != nil {
				return
			}
			go drainAfter(t, ch, 2*time.Second)
			seq++
		}
	}()
	defer func() {
		close(stopFeed)
		<-feederDone
	}()

	cfg := WatchdogConfig{
		Interval:       20 * time.Millisecond,
		IdleBudget:     60 * time.Millisecond,
		EscalateAfter:  2,
		EscalateWindow: 5 * time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := d.RunWatchdog(ctx, cfg)
	if !errors.Is(err, ErrWatchdogEscalated) {
		t.Fatalf("expected ErrWatchdogEscalated, got %v (wedgeCount=%d)", err, d.wedgeCount.Load())
	}
}

// drainAfter waits up to budget for a response and discards it.
// Used by the escalation test to keep response channels from
// blocking the test client when reset cancels in-flight Execs.
func drainAfter(_ *testing.T, ch chan rpcResponse, budget time.Duration) {
	select {
	case <-ch:
	case <-time.After(budget):
	}
}
