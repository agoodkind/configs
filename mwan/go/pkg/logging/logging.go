package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// TeeHandler fans out a slog.Record to multiple child handlers.
type TeeHandler struct {
	children []slog.Handler
}

func NewTeeHandler(children ...slog.Handler) *TeeHandler {
	return &TeeHandler{children: children}
}

func (t *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.children {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (t *TeeHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range t.children {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	children := make([]slog.Handler, len(t.children))
	for i, h := range t.children {
		children[i] = h.WithAttrs(attrs)
	}
	return &TeeHandler{children: children}
}

func (t *TeeHandler) WithGroup(name string) slog.Handler {
	children := make([]slog.Handler, len(t.children))
	for i, h := range t.children {
		children[i] = h.WithGroup(name)
	}
	return &TeeHandler{children: children}
}

// TextHandler writes human-readable lines to a writer.
// Format: 2006-01-02 15:04:05 [<label>] LEVEL msg key=val key=val
type TextHandler struct {
	mu    sync.Mutex
	w     io.Writer
	label string
}

func NewTextHandler(w io.Writer, label string) *TextHandler {
	return &TextHandler{w: w, label: label}
}

func (h *TextHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *TextHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Time.Format("2006-01-02 15:04:05"))
	b.WriteByte(' ')
	b.WriteString(h.label)
	b.WriteByte(' ')
	b.WriteString(r.Level.String())
	b.WriteByte(' ')
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		fmt.Fprintf(&b, "%v", a.Value.Any())
		return true
	})
	b.WriteByte('\n')
	line := b.String()

	h.mu.Lock()
	defer h.mu.Unlock()
	_, _ = h.w.Write([]byte(line))
	return nil
}

func (h *TextHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *TextHandler) WithGroup(string) slog.Handler      { return h }

// NewLumberjackWriter returns a rotating log writer for the given path.
func NewLumberjackWriter(path string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   path,
		MaxSize:    100,
		MaxBackups: 0,
		MaxAge:     0,
		Compress:   true,
		LocalTime:  true,
	}
}
