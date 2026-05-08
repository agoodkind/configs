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
// receives. Used here to assert the level and message that AlertManager
// emits on Notify and Resolve, since those are the gates that determine
// whether the email handler actually delivers a recovery line.
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
