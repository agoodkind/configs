//go:build linux

package ifmgr

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// captureHandler is a minimal slog.Handler that records every Record it
// receives so tests can assert both the level/message of each emit (the
// gate that determines whether the email handler delivers a recovery
// line) and the total number of emits across a window (the gate that
// determines whether RepeatEvery / RepeatResolver suppress steady-state
// noise).
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *captureHandler) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

func (h *captureHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

func newAlertManagerForTest() (*AlertManager, *captureHandler) {
	h := &captureHandler{}
	log := slog.New(h)
	return NewAlertManager(log, AlertConfig{}), h
}

func TestResolveEmitsAtNotifyLevelWarn(t *testing.T) {
	a, h := newAlertManagerForTest()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	a.Notify(now, slog.LevelWarn, "wg_health", "peer-EYvlZyou", "stalled")
	a.Resolve(now.Add(time.Minute), "wg_health", "peer-EYvlZyou", "back to good")

	records := h.snapshot()
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	resolve := records[1]
	if resolve.Level != slog.LevelWarn {
		t.Errorf("resolve level=%v, want WARN", resolve.Level)
	}
	wantMsg := "RECOVERED: back to good"
	if resolve.Message != wantMsg {
		t.Errorf("resolve message=%q, want %q", resolve.Message, wantMsg)
	}
}

func TestResolveEmitsAtNotifyLevelError(t *testing.T) {
	a, h := newAlertManagerForTest()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	a.Notify(now, slog.LevelError, "wg_health", "peer-jz3eKGui", "remote wg show failed")
	a.Resolve(now.Add(time.Minute), "wg_health", "peer-jz3eKGui", "remote wg show ok")

	records := h.snapshot()
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	resolve := records[1]
	if resolve.Level != slog.LevelError {
		t.Errorf("resolve level=%v, want ERROR", resolve.Level)
	}
	wantMsg := "RECOVERED: remote wg show ok"
	if resolve.Message != wantMsg {
		t.Errorf("resolve message=%q, want %q", resolve.Message, wantMsg)
	}
}

func TestResolveOnUnknownKeyIsNoOp(t *testing.T) {
	a, h := newAlertManagerForTest()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	// Must not panic and must emit nothing: there is no active alert for
	// this (kind, key), so Resolve has no recovery to announce.
	a.Resolve(now, "wg_health", "never-fired", "spurious")

	if got := len(h.snapshot()); got != 0 {
		t.Errorf("emitted %d records on unknown-key Resolve, want 0", got)
	}
}

func TestResolveDoesNotDoublePrefixRecovered(t *testing.T) {
	a, h := newAlertManagerForTest()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	a.Notify(now, slog.LevelError, "kind", "key", "broken")
	// Caller has already added the prefix; AlertManager must not stack it.
	a.Resolve(now.Add(time.Minute), "kind", "key", "RECOVERED: fixed itself")

	records := h.snapshot()
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if got := records[1].Message; got != "RECOVERED: fixed itself" {
		t.Errorf("resolve message=%q, want %q", got, "RECOVERED: fixed itself")
	}
}

// TestAlertManager_StateChangeOnlyByDefault drives Notify many times across
// simulated time with RepeatEvery=0 and no per-module override. Only the
// initial transition should emit; subsequent Notify calls collapse into the
// already-active state.
func TestAlertManager_StateChangeOnlyByDefault(t *testing.T) {
	h := &captureHandler{}
	log := slog.New(h)
	am := NewAlertManager(log, AlertConfig{RepeatEvery: 0})

	start := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 240; i++ {
		now := start.Add(time.Duration(i) * time.Minute)
		am.Notify(now, slog.LevelWarn, "wg-peer-stalled", "peer1", "stalled")
	}

	if got := h.count(); got != 1 {
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
	h := &captureHandler{}
	log := slog.New(h)
	am := NewAlertManager(log, AlertConfig{RepeatEvery: 30 * time.Minute})

	start := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 120; i++ {
		now := start.Add(time.Duration(i) * time.Minute)
		am.Notify(now, slog.LevelWarn, "wg-peer-stalled", "peer1", "stalled")
	}

	if got := h.count(); got != 4 {
		t.Fatalf("expected 4 emits (transition + 3 repeats), got %d", got)
	}
}

// TestAlertManager_PerModuleOverride verifies that the RepeatResolver
// short-circuits the global RepeatEvery on a per-alert-kind basis.
// PerModule says wg-peer-stalled repeats every 24h, so during a 2h window
// only the transition emit fires. A different kind under the same window
// falls back to the 30m global cadence and emits 4 times.
func TestAlertManager_PerModuleOverride(t *testing.T) {
	h := &captureHandler{}
	log := slog.New(h)

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
	stalledCount := h.count()
	if stalledCount != 1 {
		t.Fatalf("wg-peer-stalled with 24h override: expected 1 emit, got %d", stalledCount)
	}

	for i := 0; i < 120; i++ {
		now := start.Add(time.Duration(i) * time.Minute)
		am.Notify(now, slog.LevelWarn, "wg-reconcile-failed", "peer1", "reconcile failed")
	}
	totalCount := h.count()
	reconcileCount := totalCount - stalledCount
	if reconcileCount != 4 {
		t.Fatalf("wg-reconcile-failed using global 30m: expected 4 emits, got %d", reconcileCount)
	}
}
