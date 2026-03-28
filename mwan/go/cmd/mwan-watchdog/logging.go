package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// teeHandler fans out a slog.Record to multiple child handlers.
type teeHandler struct {
	children []slog.Handler
}

func newTeeHandler(children ...slog.Handler) *teeHandler {
	return &teeHandler{children: children}
}

func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.children {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range t.children {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	children := make([]slog.Handler, len(t.children))
	for i, h := range t.children {
		children[i] = h.WithAttrs(attrs)
	}
	return &teeHandler{children: children}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	children := make([]slog.Handler, len(t.children))
	for i, h := range t.children {
		children[i] = h.WithGroup(name)
	}
	return &teeHandler{children: children}
}

// textHandler writes human-readable lines to a writer.
// Format: 2006-01-02 15:04:05 [mwan-watchdog] LEVEL msg key=val key=val
type textHandler struct {
	mu sync.Mutex
	w  io.Writer
}

func (h *textHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Time.Format("2006-01-02 15:04:05"))
	b.WriteString(" [mwan-watchdog] ")
	b.WriteString(r.Level.String())
	b.WriteByte(' ')
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(fmt.Sprintf("%v", a.Value.Any()))
		return true
	})
	b.WriteByte('\n')
	line := b.String()

	h.mu.Lock()
	defer h.mu.Unlock()
	_, _ = h.w.Write([]byte(line))
	return nil
}

func (h *textHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *textHandler) WithGroup(string) slog.Handler      { return h }

func newLumberjackWriter(path string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   path,
		MaxSize:    100,
		MaxBackups: 0,
		MaxAge:     0,
		Compress:   true,
		LocalTime:  true,
	}
}

// newWatchdogLogger tees to a text file, a JSON lines file, and JSON on stdout
// (journald). Paths come from cfg.LogFile and cfg.JSONLogFile.
func newWatchdogLogger(cfg config) (*slog.Logger, error) {
	textLJ := newLumberjackWriter(cfg.LogFile)
	jsonLJ := newLumberjackWriter(cfg.JSONLogFile)

	txtH := &textHandler{w: textLJ}
	jsonOpts := &slog.HandlerOptions{Level: slog.LevelDebug}
	jsonH := slog.NewJSONHandler(jsonLJ, jsonOpts)
	stdoutH := slog.NewJSONHandler(os.Stdout, jsonOpts)

	logger := slog.New(newTeeHandler(txtH, jsonH, stdoutH))
	return logger, nil
}
