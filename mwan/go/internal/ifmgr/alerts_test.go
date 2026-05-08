//go:build linux

package ifmgr

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// captureHandler is a minimal slog.Handler that records every emit so the
// tests can count how many alert lines AlertManager produced. It is the
// stand-in for the real email/journal handler chain that production wires
// up via internal/logging.
type captureHandler struct {
	mu     sync.Mutex
	emits  []slog.Record
	leveld slog.Level
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{leveld: slog.LevelDebug}
}

func (h *captureHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.leveld
}

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.emits = append(h.emits, r)
	return nil
}

func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *captureHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.emits)
}

// TestAlertManager_StateChangeOnlyByDefault drives Notify many times across
// simulated time with RepeatEvery=0 and no per-module override. Only the
// initial transition should emit; subsequent Notify calls collapse into the
// already-active state.
func TestAlertManager_StateChangeOnlyByDefault(t *testing.T) {
	cap := newCaptureHandler()
	log := slog.New(cap)
	am := NewAlertManager(log, AlertConfig{RepeatEvery: 0})

	start := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 240; i++ {
		now := start.Add(time.Duration(i) * time.Minute)
		am.Notify(now, slog.LevelWarn, "wg-peer-stalled", "peer1", "stalled")
	}

	if got := cap.count(); got != 1 {
		t.Fatalf("expected exactly 1 emit (transition only), got %d", got)
	}
}

// TestAlertManager_RepeatEveryGlobal drives Notify every minute for two
// hours with RepeatEvery=30m. Expect 4 emits: the transition at t=0, then
// repeats at t=30m, t=60m, t=90m. The Notify at t=120m is exactly at the
// next boundary; with our minute-stepped clock the boundary at t=120m is
// also a repeat tick, so we drive Notify for i in [0, 120) to land on
// 4 emits.
func TestAlertManager_RepeatEveryGlobal(t *testing.T) {
	cap := newCaptureHandler()
	log := slog.New(cap)
	am := NewAlertManager(log, AlertConfig{RepeatEvery: 30 * time.Minute})

	start := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 120; i++ {
		now := start.Add(time.Duration(i) * time.Minute)
		am.Notify(now, slog.LevelWarn, "wg-peer-stalled", "peer1", "stalled")
	}

	if got := cap.count(); got != 4 {
		t.Fatalf("expected 4 emits (transition + 3 repeats), got %d", got)
	}
}

// TestAlertManager_PerModuleOverride verifies that the RepeatResolver
// short-circuits the global RepeatEvery on a per-alert-kind basis.
// PerModule says wg-peer-stalled repeats every 24h, so during a 2h window
// only the transition emit fires. A different kind under the same window
// falls back to the 30m global cadence and emits 4 times.
func TestAlertManager_PerModuleOverride(t *testing.T) {
	cap := newCaptureHandler()
	log := slog.New(cap)

	perKind := map[string]time.Duration{
		"wg-peer-stalled": 24 * time.Hour,
	}
	resolver := func(kind, _ string) time.Duration {
		if d, ok := perKind[kind]; ok {
			return d
		}
		return 30 * time.Minute
	}
	am := NewAlertManager(log, AlertConfig{
		RepeatEvery:    30 * time.Minute,
		RepeatResolver: resolver,
	})

	start := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 120; i++ {
		now := start.Add(time.Duration(i) * time.Minute)
		am.Notify(now, slog.LevelWarn, "wg-peer-stalled", "peer1", "stalled")
	}
	stalledCount := cap.count()
	if stalledCount != 1 {
		t.Fatalf("wg-peer-stalled with 24h override: expected 1 emit, got %d", stalledCount)
	}

	for i := 0; i < 120; i++ {
		now := start.Add(time.Duration(i) * time.Minute)
		am.Notify(now, slog.LevelWarn, "wg-reconcile-failed", "peer1", "reconcile failed")
	}
	totalCount := cap.count()
	reconcileCount := totalCount - stalledCount
	if reconcileCount != 4 {
		t.Fatalf("wg-reconcile-failed using global 30m: expected 4 emits, got %d", reconcileCount)
	}
}
