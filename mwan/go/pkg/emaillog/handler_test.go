package emaillog

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockSender struct {
	mu    sync.Mutex
	calls []struct{ to, subject, body string }
}

func (m *mockSender) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, struct{ to, subject, body string }{to, subject, body})
	return nil
}

func (m *mockSender) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func TestHandleSuppressesBelowThreshold(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	h := New(slog.LevelError, time.Minute, s, "ops@example.com")
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "below threshold", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if s.count() != 0 {
		t.Fatalf("expected 0 emails, got %d", s.count())
	}
}

func TestHandleSendsAtThreshold(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	h := New(slog.LevelWarn, time.Minute, s, "ops@example.com")
	r := slog.NewRecord(time.Now(), slog.LevelWarn, "at threshold", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if s.count() != 1 {
		t.Fatalf("expected 1 email, got %d", s.count())
	}
	s.mu.Lock()
	sub := s.calls[0].subject
	s.mu.Unlock()
	if !strings.Contains(sub, "WARN") || !strings.Contains(sub, "at threshold") {
		t.Fatalf("unexpected subject: %q", sub)
	}
}

func TestHandleSuppressesDuplicateWithinCooldown(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	h := New(slog.LevelInfo, time.Minute, s, "ops@example.com")
	send := func() {
		r := slog.NewRecord(time.Now(), slog.LevelError, "same message", 0)
		if err := h.Handle(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	send()
	send()
	if s.count() != 1 {
		t.Fatalf("expected 1 email, got %d", s.count())
	}
}

func TestHandleAllowsAfterCooldownExpires(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	cd := 20 * time.Millisecond
	h := New(slog.LevelInfo, cd, s, "ops@example.com")
	send := func() {
		r := slog.NewRecord(time.Now(), slog.LevelError, "retry msg", 0)
		if err := h.Handle(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	send()
	time.Sleep(cd + 20*time.Millisecond)
	send()
	if s.count() != 2 {
		t.Fatalf("expected 2 emails, got %d", s.count())
	}
}
