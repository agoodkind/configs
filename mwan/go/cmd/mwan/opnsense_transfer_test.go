package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTransferWatchdogFiresOnStall proves the watchdog cancels the context when
// no progress is reported for the stall window.
func TestTransferWatchdogFiresOnStall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wd := startTransferWatchdog(cancel, 100*time.Millisecond)
	defer wd.stop()

	select {
	case <-ctx.Done():
		if !wd.fired() {
			t.Fatal("context canceled but watchdog did not record a stall")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not fire within 2s for a 100ms stall")
	}
}

// TestTransferWatchdogProgressPreventsFiring proves that steady progress keeps
// the watchdog from firing.
func TestTransferWatchdogProgressPreventsFiring(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wd := startTransferWatchdog(cancel, 200*time.Millisecond)
	defer wd.stop()

	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		wd.markProgress()
		time.Sleep(20 * time.Millisecond)
	}
	if wd.fired() {
		t.Fatal("watchdog fired despite steady progress")
	}
	if ctx.Err() != nil {
		t.Fatalf("context canceled despite steady progress: %v", ctx.Err())
	}
}

// TestTransferWatchdogStopHalts proves stop() ends the watchdog so it never
// fires afterward, and that a stopped watchdog reports no stall.
func TestTransferWatchdogStopHalts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wd := startTransferWatchdog(cancel, 100*time.Millisecond)
	wd.stop()

	// Well past the stall window: a stopped watchdog must not fire.
	time.Sleep(300 * time.Millisecond)
	if wd.fired() {
		t.Fatal("watchdog fired after stop()")
	}
	if ctx.Err() != nil {
		t.Fatalf("context canceled after stop(): %v", ctx.Err())
	}
}

// TestTransferWatchdogFailureMessages proves failure() returns a clear stall
// error once the watchdog tripped, and otherwise wraps the real error.
func TestTransferWatchdogFailureMessages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wd := startTransferWatchdog(cancel, 100*time.Millisecond)
	defer wd.stop()

	real := errors.New("boom")
	if got := wd.failure(ctx, "upload: send data", real); !errors.Is(got, real) {
		t.Fatalf("before stall, failure should wrap the real error, got %v", got)
	}

	<-ctx.Done()
	if !wd.fired() {
		t.Fatal("watchdog did not record a stall after firing")
	}
	got := wd.failure(ctx, "upload: recv data ack", real)
	if errors.Is(got, real) {
		t.Fatalf("after stall, failure must not wrap the real error, got %v", got)
	}
	if !strings.Contains(got.Error(), "transfer stalled") {
		t.Fatalf("after stall, failure should mention a stall, got %v", got)
	}
}

// TestRequireProbeTransferStall covers the fallback-on-empty behavior plus the
// parse and positivity checks.
func TestRequireProbeTransferStall(t *testing.T) {
	t.Run("empty falls back to default", func(t *testing.T) {
		writeTempTOML(t, `
hostname = "stall-test"

[opnsense.probe]
target = "unix:///tmp/x.sock"
timeout = "10s"
upload_chunk_bytes = 16384
`)
		cfg, err := loadOpnsenseConfig()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		got, err := requireProbeTransferStall(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != probeTransferStallDefault {
			t.Fatalf("got %s, want default %s", got, probeTransferStallDefault)
		}
	})

	t.Run("explicit value is parsed", func(t *testing.T) {
		writeTempTOML(t, `
hostname = "stall-test"

[opnsense.probe]
target = "unix:///tmp/x.sock"
transfer_stall_timeout = "45s"
`)
		cfg, err := loadOpnsenseConfig()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		got, err := requireProbeTransferStall(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 45*time.Second {
			t.Fatalf("got %s, want 45s", got)
		}
	})

	t.Run("malformed value errors", func(t *testing.T) {
		writeTempTOML(t, `
hostname = "stall-test"

[opnsense.probe]
target = "unix:///tmp/x.sock"
transfer_stall_timeout = "not-a-duration"
`)
		cfg, err := loadOpnsenseConfig()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if _, err := requireProbeTransferStall(cfg); err == nil {
			t.Fatal("expected an error for a malformed duration")
		}
	})
}
