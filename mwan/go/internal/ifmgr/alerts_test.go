//go:build linux

package ifmgr

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// captureHandler records every slog.Record so adapter tests can assert
// what the wrapped notify.Manager emitted via the journald path. The
// notify package owns the broader state-change tests under
// internal/notify/manager_test.go; this file covers only the
// AlertManager → notify.Notifier adapter surface (variadic ...any tail
// translation, signature preservation for module callers).
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

// TestAlertManager_NotifyResolveAdapter checks that the adapter forwards
// a Notify and a Resolve through to the wrapped Notifier and that both
// the alert_kind and alert_key attributes survive the variadic-to-Attr
// conversion. The notify package separately tests the state machine, so
// this test only confirms the bridge.
func TestAlertManager_NotifyResolveAdapter(t *testing.T) {
	h := &captureHandler{}
	log := slog.New(h)
	a := NewAlertManager(log, AlertConfig{})
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	a.Notify(now, slog.LevelWarn, "kind", "key", "broken", "extra", "ctx")
	if !a.Active("kind", "key") {
		t.Fatal("expected Active=true after Notify")
	}
	a.Resolve(now.Add(time.Minute), "kind", "key", "fixed")
	if a.Active("kind", "key") {
		t.Fatal("expected Active=false after Resolve")
	}

	records := h.snapshot()
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if records[0].Level != slog.LevelWarn {
		t.Errorf("notify level=%v, want WARN", records[0].Level)
	}
	if records[1].Level != slog.LevelWarn {
		t.Errorf("resolve level=%v, want WARN (matches notify level)", records[1].Level)
	}
	if got := records[1].Message; got != "RECOVERED: fixed" {
		t.Errorf("resolve message=%q, want %q", got, "RECOVERED: fixed")
	}

	// Confirm the variadic ...any tail produced an extra=ctx attr on the
	// notify record. The adapter pairs alternating string/any entries.
	var sawExtra bool
	records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "extra" && a.Value.String() == "ctx" {
			sawExtra = true
		}
		return true
	})
	if !sawExtra {
		t.Error("expected variadic field extra=ctx to survive translation")
	}
}

// TestAlertManager_OddVariadicTailDropsDanglingKey confirms the adapter
// does not panic on an odd-length tail and silently drops the dangling
// key (mirrors slog's loose-pair tolerance).
func TestAlertManager_OddVariadicTailDropsDanglingKey(t *testing.T) {
	h := &captureHandler{}
	log := slog.New(h)
	a := NewAlertManager(log, AlertConfig{})
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	a.Notify(now, slog.LevelError, "kind", "key", "msg",
		"complete_pair_key", "complete_pair_val",
		"dangling_key",
	)

	records := h.snapshot()
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	var sawComplete, sawDangling bool
	records[0].Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "complete_pair_key":
			sawComplete = true
		case "dangling_key":
			sawDangling = true
		}
		return true
	})
	if !sawComplete {
		t.Error("expected complete pair to survive translation")
	}
	if sawDangling {
		t.Error("dangling key should be dropped, not emitted")
	}
}

// TestWrapNotifier_NilDegradesToNull confirms WrapNotifier(nil) returns
// an AlertManager that does not panic and reports Active=false.
func TestWrapNotifier_NilDegradesToNull(t *testing.T) {
	a := WrapNotifier(nil)
	a.Notify(time.Now(), slog.LevelWarn, "kind", "key", "msg")
	if a.Active("kind", "key") {
		t.Error("nil-wrapped AlertManager.Active should always return false")
	}
}
