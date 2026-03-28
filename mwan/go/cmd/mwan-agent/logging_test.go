package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewAgentLogger(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "agent.log")
	log, err := newAgentLogger(p)
	if err != nil {
		t.Fatal(err)
	}
	if log == nil {
		t.Fatal("nil logger")
	}
	log.Info("probe", "k", "v")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("log file: %v", err)
	}
}

type errHandler struct{}

func (errHandler) Enabled(context.Context, slog.Level) bool { return true }

func (errHandler) Handle(context.Context, slog.Record) error {
	return errors.New("handler error")
}

func (errHandler) WithAttrs([]slog.Attr) slog.Handler { return errHandler{} }

func (errHandler) WithGroup(string) slog.Handler { return errHandler{} }

func TestTeeHandler_HandleErrorPropagates(t *testing.T) {
	t.Parallel()
	th := newTeeHandler(errHandler{}, slog.NewJSONHandler(io.Discard, nil))
	err := th.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "m", 0))
	if err == nil || !strings.Contains(err.Error(), "handler error") {
		t.Fatalf("want handler error, got %v", err)
	}
}

func TestTeeHandler_Enabled(t *testing.T) {
	t.Parallel()
	disabled := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})
	th := newTeeHandler(disabled, slog.NewJSONHandler(io.Discard, nil))
	if th.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("expected disabled at debug when both handlers reject")
	}
	th2 := newTeeHandler(slog.NewJSONHandler(io.Discard, nil), disabled)
	if !th2.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected enabled when one child accepts")
	}
}

func TestTeeHandler_WithAttrs(t *testing.T) {
	t.Parallel()
	base := slog.NewJSONHandler(io.Discard, nil)
	th := newTeeHandler(base).WithAttrs([]slog.Attr{slog.String("a", "b")})
	if th == nil {
		t.Fatal("nil handler")
	}
}

func TestTeeHandler_WithGroup(t *testing.T) {
	t.Parallel()
	base := slog.NewJSONHandler(io.Discard, nil)
	th := newTeeHandler(base).WithGroup("g")
	if th == nil {
		t.Fatal("nil handler")
	}
}

func TestTextHandler_WithGroup(t *testing.T) {
	t.Parallel()
	th := (&textHandler{w: io.Discard}).WithGroup("g")
	if th == nil {
		t.Fatal("nil handler")
	}
}

func TestTextHandler_Handle(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	th := &textHandler{w: &b}
	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "hello", 0)
	rec.Add("x", 1)
	if err := th.Handle(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "hello") || !strings.Contains(b.String(), "x=1") {
		t.Fatalf("output=%q", b.String())
	}
}
