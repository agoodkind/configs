//go:build linux

package cloudflaredtap

import (
	"log/slog"
	"regexp"
	"testing"
)

func TestNew_DefaultsApplied(t *testing.T) {
	m, err := New(map[string]any{"unit": "cloudflared-oob"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mod, ok := m.(*Module)
	if !ok {
		t.Fatalf("New returned wrong type")
	}
	if mod.cfg.Unit != "cloudflared-oob" {
		t.Errorf("unit=%q want cloudflared-oob", mod.cfg.Unit)
	}
	if mod.cfg.JournalctlPath != "" {
		t.Errorf("JournalctlPath should be empty until Init applies the default; got %q",
			mod.cfg.JournalctlPath)
	}
	if len(mod.cfg.DowngradePatterns) != 0 {
		t.Errorf("DowngradePatterns should be empty; got %d", len(mod.cfg.DowngradePatterns))
	}
}

func TestNew_DowngradePatternsParsedFromAny(t *testing.T) {
	m, _ := New(map[string]any{
		"unit":               "x",
		"downgrade_patterns": []any{"foo", "bar.*"},
	})
	mod := m.(*Module)
	if len(mod.cfg.DowngradePatterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(mod.cfg.DowngradePatterns))
	}
	if mod.cfg.DowngradePatterns[0] != "foo" || mod.cfg.DowngradePatterns[1] != "bar.*" {
		t.Errorf("patterns: %v", mod.cfg.DowngradePatterns)
	}
}

func TestNew_DowngradePatternsParsedFromStringSlice(t *testing.T) {
	m, _ := New(map[string]any{
		"unit":               "x",
		"downgrade_patterns": []string{"alpha", "beta"},
	})
	mod := m.(*Module)
	if len(mod.cfg.DowngradePatterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(mod.cfg.DowngradePatterns))
	}
}

func TestNew_JournalctlPathOverride(t *testing.T) {
	m, _ := New(map[string]any{"unit": "x", "journalctl_path": "/usr/bin/journalctl-test"})
	if m.(*Module).cfg.JournalctlPath != "/usr/bin/journalctl-test" {
		t.Errorf("journalctl_path not parsed")
	}
}

func TestMapPriority(t *testing.T) {
	cases := []struct {
		p    int
		want slog.Level
	}{
		{0, slog.LevelError}, // emerg
		{1, slog.LevelError}, // alert
		{2, slog.LevelError}, // crit
		{3, slog.LevelError}, // err
		{4, slog.LevelWarn},  // warning
		{5, slog.LevelInfo},  // notice
		{6, slog.LevelInfo},  // info
		{7, slog.LevelDebug}, // debug
		{99, slog.LevelInfo}, // out of range fallback
	}
	for _, c := range cases {
		got := mapPriority(c.p)
		if got != c.want {
			t.Errorf("mapPriority(%d) = %v want %v", c.p, got, c.want)
		}
	}
}

func TestMatchAny(t *testing.T) {
	pats := []*regexp.Regexp{
		regexp.MustCompile(`failed to dial to edge with quic: timeout`),
		regexp.MustCompile(`datagram manager encountered a failure while serving`),
	}
	if !matchAny(pats, "WRN failed to dial to edge with quic: timeout connIndex=2") {
		t.Errorf("expected match for noisy edge-rotation line")
	}
	if !matchAny(pats, "datagram manager encountered a failure while serving connIndex=0") {
		t.Errorf("expected match for datagram-manager line")
	}
	if matchAny(pats, "Registered tunnel connection connIndex=0 location=sjc07") {
		t.Errorf("unexpected match for benign Registered line")
	}
	if matchAny(nil, "anything") {
		t.Errorf("nil patterns should never match")
	}
}

func TestStrField(t *testing.T) {
	e := map[string]any{
		"MESSAGE":      "hello",
		"_PID":         "1234",
		"_NUMBER_LIKE": float64(7),
	}
	if got := strField(e, "MESSAGE"); got != "hello" {
		t.Errorf("MESSAGE = %q", got)
	}
	if got := strField(e, "_PID"); got != "1234" {
		t.Errorf("_PID = %q", got)
	}
	if got := strField(e, "_NUMBER_LIKE"); got != "" {
		t.Errorf("non-string field should return empty; got %q", got)
	}
	if got := strField(e, "MISSING"); got != "" {
		t.Errorf("missing field should return empty; got %q", got)
	}
}

func TestIntField(t *testing.T) {
	e := map[string]any{
		"PRIORITY": "6",
		"BAD":      "not-a-number",
	}
	if got := intField(e, "PRIORITY"); got != 6 {
		t.Errorf("PRIORITY = %d", got)
	}
	if got := intField(e, "BAD"); got != 0 {
		t.Errorf("non-numeric should return 0; got %d", got)
	}
	if got := intField(e, "MISSING"); got != 0 {
		t.Errorf("missing should return 0; got %d", got)
	}
}
