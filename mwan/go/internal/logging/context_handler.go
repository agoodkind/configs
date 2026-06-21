package logging

import (
	"context"
	"fmt"
	"log/slog"

	"goodkind.io/mwan/internal/tracing"
)

// ContextHandler copies tracing attributes from context onto each slog record.
type ContextHandler struct {
	next slog.Handler
}

// NewContextHandler wraps next so traced contexts enrich emitted records.
func NewContextHandler(next slog.Handler) *ContextHandler {
	return &ContextHandler{next: next}
}

// Enabled reports whether next handles the supplied level for ctx.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds tracing attrs from ctx before delegating to next.
func (h *ContextHandler) Handle(ctx context.Context, record slog.Record) error {
	if attrs := tracing.AttrsFromContext(ctx); len(attrs) > 0 {
		cloned := record.Clone()
		cloned.AddAttrs(attrs...)
		record = cloned
	}
	if err := h.next.Handle(ctx, record); err != nil {
		slog.ErrorContext(ctx, "context handler failed", "err", err)
		return fmt.Errorf("context handler: %w", err)
	}
	return nil
}

// WithAttrs returns a handler that adds attrs before delegating to next.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a handler that nests subsequent attrs under name.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{next: h.next.WithGroup(name)}
}
