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
	mu     sync.Mutex
	w      io.Writer
	label  string
	attrs  []slog.Attr
	groups []string
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
	writeAttrs(&b, "", h.attrs)
	recordAttrs := make([]slog.Attr, 0)
	r.Attrs(func(a slog.Attr) bool {
		recordAttrs = append(recordAttrs, a)
		return true
	})
	writeAttrs(&b, groupPrefix(h.groups), recordAttrs)
	b.WriteByte('\n')
	line := b.String()

	h.mu.Lock()
	defer h.mu.Unlock()
	_, _ = h.w.Write([]byte(line))
	return nil
}

func (h *TextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TextHandler{
		w:     h.w,
		label: h.label,
		attrs: append(append([]slog.Attr(nil), h.attrs...),
			wrapGroups(h.groups, attrs)...),
		groups: append([]string(nil), h.groups...),
	}
}

func (h *TextHandler) WithGroup(name string) slog.Handler {
	out := &TextHandler{
		w:      h.w,
		label:  h.label,
		attrs:  append([]slog.Attr(nil), h.attrs...),
		groups: append([]string(nil), h.groups...),
	}
	if name != "" {
		out.groups = append(out.groups, name)
	}
	return out
}

func writeAttrs(builder *strings.Builder, prefix string, attrs []slog.Attr) {
	for _, attr := range attrs {
		writeAttr(builder, prefix, attr)
	}
}

func writeAttr(builder *strings.Builder, prefix string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		nextPrefix := attr.Key
		if prefix != "" {
			nextPrefix = prefix + "." + attr.Key
		}
		writeAttrs(builder, nextPrefix, attr.Value.Group())
		return
	}

	key := attr.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	builder.WriteByte(' ')
	builder.WriteString(key)
	builder.WriteByte('=')
	fmt.Fprintf(builder, "%v", attr.Value.Any())
}

func wrapGroups(groups []string, attrs []slog.Attr) []slog.Attr {
	if len(groups) == 0 {
		return attrs
	}

	wrapped := append([]slog.Attr(nil), attrs...)
	for i := len(groups) - 1; i >= 0; i-- {
		wrapped = []slog.Attr{slog.Group(groups[i], attrsToAny(wrapped)...)}
	}
	return wrapped
}

func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, attr)
	}
	return out
}

func groupPrefix(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	return strings.Join(groups, ".")
}

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
