//go:build linux

package ifmgr

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

// AlertManager is a small transition + repeat tracker for module alerts.
// It does not own delivery; the actual email/journal handler lives in
// the slog logger chain (internal/logging routes WARN/ERROR through the
// email sender). AlertManager exists to suppress duplicate alerts per
// (kind, key) when modules call Notify repeatedly during the same
// degraded interval, and to re-emit on a configurable repeat cadence so
// long-lived problems do not silently drop off the operator's radar.
//
// Concurrency: safe for concurrent Notify from multiple modules.
type AlertManager struct {
	cfg AlertConfig
	log *slog.Logger

	mu    sync.Mutex
	state map[string]alertState
}

// AlertConfig controls the per-(kind,key) suppression and repeat cadence.
type AlertConfig struct {
	// RepeatEvery: if a previously-emitted alert is Notify()d again, the
	// manager re-emits at most once per RepeatEvery. Zero disables repeats
	// (alerts fire once per transition). Used as a fallback when
	// RepeatResolver is nil.
	RepeatEvery time.Duration

	// RepeatResolver, when non-nil, is consulted on every Notify to
	// determine the repeat interval for that (kind, key). This lets the
	// daemon plumb a per-alert-kind cadence from TOML without forcing
	// AlertManager to know about the config schema. When nil, RepeatEvery
	// is used for all (kind, key) pairs.
	RepeatResolver func(kind, key string) time.Duration
}

// alertState is the per-(kind,key) memory: was the alert active last
// time we saw it, when did we last emit, and at what level.
//
// lastLevel is recorded at Notify-emit time so Resolve can re-emit the
// recovery line at the same severity. Without this, Resolve would log
// at INFO and be filtered out by EmailConfig.MinLevel (default ERROR),
// leaving open alerts in the inbox with no visible close.
type alertState struct {
	active    bool
	lastEmit  time.Time
	lastLevel slog.Level
}

// NewAlertManager constructs an AlertManager with the given config.
// log must be non-nil; log.With("component","alerts") is applied here.
func NewAlertManager(log *slog.Logger, cfg AlertConfig) *AlertManager {
	if log == nil {
		panic("ifmgr.NewAlertManager: log is required")
	}
	return &AlertManager{
		cfg:   cfg,
		log:   log.With("component", "alerts"),
		state: map[string]alertState{},
	}
}

// Notify emits an alert at the given level if either:
//   - This (kind, key) was not active before (transition into bad state), OR
//   - It is still active AND RepeatEvery has elapsed since the last emit.
//
// After emit, the state is marked active. Call Resolve to clear.
//
// fields are key/value pairs appended to the log record for context.
func (a *AlertManager) Notify(
	now time.Time, level slog.Level, kind, key, msg string, fields ...any,
) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := kind + "|" + key
	st, exists := a.state[id]
	shouldEmit := !exists || !st.active
	repeatEvery := a.cfg.RepeatEvery
	if a.cfg.RepeatResolver != nil {
		repeatEvery = a.cfg.RepeatResolver(kind, key)
	}
	if !shouldEmit && repeatEvery > 0 && now.Sub(st.lastEmit) >= repeatEvery {
		shouldEmit = true
	}
	st.active = true
	if shouldEmit {
		st.lastEmit = now
		st.lastLevel = level
		baseFields := []any{
			"alert_kind", kind,
			"alert_key", key,
			"transition", !exists || !a.state[id].active,
		}
		a.log.Log(nil, level, msg, append(baseFields, fields...)...)
	}
	a.state[id] = st
}

// Resolve clears the (kind, key) so the next Notify will be treated as a
// fresh transition. Call when the underlying problem is fixed.
//
// The recovery line emits at the same severity the original Notify used,
// so it crosses the same MinLevel threshold the open alert did and the
// pairing actually shows up in the inbox. The msg is prefixed with
// "RECOVERED: " (unless the caller already supplied that prefix) so the
// gklog email subject, derived from the slog message, makes the open and
// close obvious at a glance.
func (a *AlertManager) Resolve(now time.Time, kind, key, msg string, fields ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := kind + "|" + key
	st, exists := a.state[id]
	if !exists || !st.active {
		return
	}
	st.active = false
	st.lastEmit = now
	a.state[id] = st
	level := st.lastLevel
	// Defensive: an alertState may exist with active=true but lastLevel
	// unset if a future code path ever marks active without going through
	// the emit branch. Fall back to WARN so the recovery still surfaces.
	if level == 0 {
		level = slog.LevelWarn
	}
	if !strings.HasPrefix(msg, "RECOVERED:") {
		msg = "RECOVERED: " + msg
	}
	baseFields := []any{
		"alert_kind", kind,
		"alert_key", key,
		"resolved", true,
	}
	a.log.Log(nil, level, msg, append(baseFields, fields...)...)
}

// Active reports whether the named alert is currently in the "fired but
// not resolved" state. Useful for modules that want to escalate (e.g.
// bridge_probe waits for slaac_health to be Active before suspecting
// the bridge).
func (a *AlertManager) Active(kind, key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.state[kind+"|"+key]
	return ok && st.active
}
