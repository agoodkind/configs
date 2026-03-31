// Package emaillog provides a slog.Handler that sends email for every log
// record at or above a configured threshold level. A per-message-string
// cooldown prevents floods during sustained outages.
package emaillog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Sender delivers a single email notification.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// sharedState holds the cooldown map behind a pointer so Handler clones
// produced by WithAttrs / WithGroup share the same limiter without copying
// the mutex (which Go forbids after first use).
type sharedState struct {
	mu       sync.Mutex
	lastSent map[string]time.Time
}

// Handler is a slog.Handler that emails records at or above threshold.
type Handler struct {
	threshold slog.Level
	cooldown  time.Duration
	sender    Sender
	to        string
	attrs     []slog.Attr
	group     string
	shared    *sharedState
}

// New returns a Handler ready to emit emails.
func New(threshold slog.Level, cooldown time.Duration, sender Sender, to string) *Handler {
	return &Handler{
		threshold: threshold,
		cooldown:  cooldown,
		sender:    sender,
		to:        to,
		shared:    &sharedState{lastSent: make(map[string]time.Time)},
	}
}

// Enabled reports whether the record level meets the threshold.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.threshold
}

// Handle emails the record if it is not suppressed by cooldown.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.threshold {
		return nil
	}

	h.shared.mu.Lock()
	last, ok := h.shared.lastSent[r.Message]
	if ok && time.Since(last) < h.cooldown {
		h.shared.mu.Unlock()
		return nil
	}
	h.shared.lastSent[r.Message] = time.Now()
	h.shared.mu.Unlock()

	levelStr := strings.ToUpper(r.Level.String())
	subject := fmt.Sprintf("[mwan] %s: %s", levelStr, r.Message)

	var body strings.Builder
	body.WriteString(r.Message)
	body.WriteByte('\n')

	writeAttr := func(key, value string) {
		fmt.Fprintf(&body, "  %-12s  %s\n", key, value)
	}
	for _, a := range h.attrs {
		writeAttr(a.Key, a.Value.String())
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(a.Key, a.Value.String())
		return true
	})
	writeAttr("time", r.Time.Format(time.RFC3339))
	writeAttr("level", levelStr)

	return h.sender.Send(ctx, h.to, subject, body.String())
}

// WithAttrs returns a clone with additional attributes prepended to every email body.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := *h
	out.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &out
}

// WithGroup returns a clone with the group name stored (used for completeness;
// group prefix is not currently applied to body attribute keys).
func (h *Handler) WithGroup(name string) slog.Handler {
	out := *h
	out.attrs = append([]slog.Attr(nil), h.attrs...)
	if name == "" {
		return &out
	}
	if h.group == "" {
		out.group = name
	} else {
		out.group = h.group + "." + name
	}
	return &out
}
