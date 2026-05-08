package logging

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSends captures every Send call so tests can assert on cooldown
// behavior and body text. Concurrency-safe because Handle may be called
// from any goroutine.
type fakeSends struct {
	mu    sync.Mutex
	calls []sentMail
}

type sentMail struct {
	to      string
	subject string
	body    string
}

func (f *fakeSends) send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sentMail{to: to, subject: subject, body: body})
	return nil
}

func (f *fakeSends) snapshot() []sentMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sentMail, len(f.calls))
	copy(out, f.calls)
	return out
}

// fixedClock returns a stable Now() so cooldown tests stay deterministic
// without sleeping between Handle calls.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newFixedClock() *fixedClock {
	return &fixedClock{now: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)}
}

// TestEmailHandlerSendsTightBody wires the handler to a fakeSends and
// asserts that the body contains the new What/Where/Trace shape and the
// subject contains the level + message.
func TestEmailHandlerSendsTightBody(t *testing.T) {
	t.Parallel()
	fake := &fakeSends{}
	clock := newFixedClock()
	h := newEmailHandlerForTest(slog.LevelWarn, time.Hour, fake.send, "ops@example.com", "[mwan]", clock)

	r := recordFromAttrs(
		"wg_health: remote wg show failed",
		slog.String("iface", "mbrains"),
		slog.String("role", "vault-oob"),
		slog.String("trace", "abc12345"),
		slog.String("err", "ssh agoodkind@h: exit status 255 (stderr=\"Permission denied\")"),
	)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	calls := fake.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 send, got %d", len(calls))
	}
	got := calls[0]
	if got.to != "ops@example.com" {
		t.Errorf("to = %q, want ops@example.com", got.to)
	}
	if !strings.HasPrefix(got.subject, "[mwan] ERROR: wg_health") {
		t.Errorf("subject prefix wrong: %q", got.subject)
	}
	if !strings.Contains(got.body, "Where:   iface=mbrains, role=vault-oob") {
		t.Errorf("body missing Where line:\n%s", got.body)
	}
	if !strings.Contains(got.body, "stderr: Permission denied") {
		t.Errorf("body missing stderr line:\n%s", got.body)
	}
}

// TestEmailHandlerCooldownSuppresses confirms a duplicate message inside
// the cooldown window is dropped so we do not flood ops during a
// sustained outage.
func TestEmailHandlerCooldownSuppresses(t *testing.T) {
	t.Parallel()
	fake := &fakeSends{}
	clock := newFixedClock()
	h := newEmailHandlerForTest(slog.LevelWarn, time.Hour, fake.send, "ops@example.com", "", clock)

	r1 := recordFromAttrs("dup msg", slog.String("trace", "t1"))
	r2 := recordFromAttrs("dup msg", slog.String("trace", "t2"))
	if err := h.Handle(context.Background(), r1); err != nil {
		t.Fatalf("first send: %v", err)
	}
	clock.advance(5 * time.Minute) // still within the hour cooldown
	if err := h.Handle(context.Background(), r2); err != nil {
		t.Fatalf("second send: %v", err)
	}

	if got := len(fake.snapshot()); got != 1 {
		t.Fatalf("cooldown failed: want 1 send, got %d", got)
	}
}

// TestEmailHandlerCooldownExpires verifies that once the window passes,
// the same message is allowed through again.
func TestEmailHandlerCooldownExpires(t *testing.T) {
	t.Parallel()
	fake := &fakeSends{}
	clock := newFixedClock()
	h := newEmailHandlerForTest(slog.LevelWarn, time.Minute, fake.send, "ops@example.com", "", clock)

	r := recordFromAttrs("flaky", slog.String("trace", "t1"))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("first send: %v", err)
	}
	clock.advance(2 * time.Minute)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("second send: %v", err)
	}
	if got := len(fake.snapshot()); got != 2 {
		t.Fatalf("cooldown expiry: want 2 sends, got %d", got)
	}
}

// TestEmailHandlerBelowThreshold ensures records below threshold never
// dial the sender.
func TestEmailHandlerBelowThreshold(t *testing.T) {
	t.Parallel()
	fake := &fakeSends{}
	clock := newFixedClock()
	h := newEmailHandlerForTest(slog.LevelWarn, time.Hour, fake.send, "ops@example.com", "", clock)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "noisy", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(fake.snapshot()); got != 0 {
		t.Fatalf("info-level slipped through: got %d sends", got)
	}
}

// TestEmailHandlerWithAttrs verifies that attrs bound via WithAttrs land
// in the body via the same builder pipeline as record attrs.
func TestEmailHandlerWithAttrs(t *testing.T) {
	t.Parallel()
	fake := &fakeSends{}
	clock := newFixedClock()
	base := newEmailHandlerForTest(slog.LevelWarn, time.Hour, fake.send, "ops@example.com", "", clock)
	bound := base.WithAttrs([]slog.Attr{
		slog.String("commit", "deadbee"),
		slog.String("dirty", "clean"),
	})

	r := recordFromAttrs("alert", slog.String("trace", "t1"))
	if err := bound.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	calls := fake.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 send, got %d", len(calls))
	}
	if !strings.Contains(calls[0].body, "Build: deadbee (clean)") {
		t.Errorf("bound build attrs missing from body:\n%s", calls[0].body)
	}
}
