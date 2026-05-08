package logging

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"goodkind.io/mwan/internal/email"
)

// sendFn is the function shape the handler calls to deliver an email.
// Using a function value (instead of an interface from another package)
// keeps wrapcheck quiet about the cross-package boundary while still
// letting tests inject a fake.
type sendFn func(ctx context.Context, to, subject, body string) error

// emailCooldownState holds the per-message-string cooldown map behind a
// pointer so handler clones produced by WithAttrs and WithGroup share
// the same limiter without copying the mutex (which Go forbids after
// first use).
type emailCooldownState struct {
	mu       sync.Mutex
	lastSent map[string]time.Time
}

// emailHandler is a [slog.Handler] that emails records at or above
// threshold. It produces a tight body via [BuildEmailBody] so the alert
// reads cleanly above the host-snapshot footer that send-email appends
// downstream.
type emailHandler struct {
	threshold     slog.Level
	cooldown      time.Duration
	send          sendFn
	to            string
	subjectPrefix string
	attrs         []slog.Attr
	cool          *emailCooldownState
	clock         emailClock
}

// newEmailHandler wires the handler against a concrete [email.Sender].
// Tests use newEmailHandlerForTest with a fake send function and clock.
func newEmailHandler(threshold slog.Level, cooldown time.Duration, sender *email.Sender, to, subjectPrefix string) *emailHandler {
	return newEmailHandlerForTest(threshold, cooldown, sender.Send, to, subjectPrefix, realEmailClock{})
}

// newEmailHandlerForTest is the seam used by unit tests so they can pass
// a fake send function and clock without wiring up a real smtp2go sender.
func newEmailHandlerForTest(threshold slog.Level, cooldown time.Duration, send sendFn, to, subjectPrefix string, clock emailClock) *emailHandler {
	return &emailHandler{
		threshold:     threshold,
		cooldown:      cooldown,
		send:          send,
		to:            to,
		subjectPrefix: subjectPrefix,
		attrs:         nil,
		cool: &emailCooldownState{
			mu:       sync.Mutex{},
			lastSent: make(map[string]time.Time),
		},
		clock: clock,
	}
}

// Enabled reports whether the record level meets the threshold.
func (h *emailHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.threshold
}

// Handle emits an email if the record is at or above threshold and not
// suppressed by cooldown.
func (h *emailHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.threshold {
		return nil
	}

	now := h.clock.Now()
	h.cool.mu.Lock()
	last, ok := h.cool.lastSent[r.Message]
	if ok && now.Sub(last) < h.cooldown {
		h.cool.mu.Unlock()
		return nil
	}
	h.cool.lastSent[r.Message] = now
	h.cool.mu.Unlock()

	subject := buildSubject(h.subjectPrefix, r)
	body := BuildEmailBody(r, h.attrs)
	// Pass through the sender's error unwrapped; this is the standard
	// slog.Handler contract and matches ContextHandler's pattern.
	// Wrapping here would also trigger the staticcheck-extra rule
	// against returning wrapped errors from Handle without an
	// accompanying slog log call (which would recurse).
	return h.send(ctx, h.to, subject, body)
}

// buildSubject composes the email subject line as
// "[prefix] LEVEL: message" or "LEVEL: message" if prefix is empty.
func buildSubject(prefix string, r slog.Record) string {
	level := strings.ToUpper(r.Level.String())
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return level + ": " + r.Message
	}
	return prefix + " " + level + ": " + r.Message
}

// WithAttrs returns a clone with attrs merged onto the bound set so they
// flow into every body without affecting the cooldown map (shared via
// the cool pointer).
func (h *emailHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := *h
	out.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &out
}

// WithGroup is a no-op for the body builder because we render attrs
// flat; the bound attrs slice is preserved so the chain still composes.
func (h *emailHandler) WithGroup(_ string) slog.Handler {
	out := *h
	out.attrs = append([]slog.Attr(nil), h.attrs...)
	return &out
}
