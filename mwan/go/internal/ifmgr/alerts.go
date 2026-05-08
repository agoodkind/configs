//go:build linux

package ifmgr

import (
	"context"
	"log/slog"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/notify"
)

// AlertManager is the ifmgr-facing adapter over the notify package.
// The state machine (per-(kind, key) active/lastEmit/lastLevel plus
// repeat cadence) lives in notify.Manager. This wrapper keeps the
// existing module call surface, which uses time.Time and a variadic
// fields ...any tail, so module code under internal/ifmgr/modules/ does
// not need to change in slice B.
//
// Concurrency: forwards to notify.Notifier, which is safe for concurrent
// Notify and Resolve calls.
type AlertManager struct {
	n notify.Notifier
}

// AlertConfig is retained as the test/legacy entry point. RepeatEvery
// maps to notify.Config.RepeatEvery. The old RepeatResolver field has
// been removed since per-kind cadence now flows through cfg.Notify in
// the production path; tests that need per-kind overrides should
// construct a notify.Manager directly.
type AlertConfig struct {
	// RepeatEvery applied to every (kind, key). Zero disables repeats so
	// alerts fire once per transition.
	RepeatEvery time.Duration
}

// NewAlertManager builds an AlertManager wired to a notify.Manager. log
// must be non-nil; the wrapped Manager journals through it. The Manager
// has a nil email Sink, which is the right default for tests and for
// daemon paths that have not been wired through notify.FromConfig yet.
//
// Implementation note: this constructor builds a synthetic *config.Config
// so notify.FromConfig can be used without exposing an additional
// constructor on the notify package. cfg.Notify.RepeatEvery feeds the
// global cadence; PerKind is left empty here because the test surface
// does not exercise it.
func NewAlertManager(log *slog.Logger, cfg AlertConfig) *AlertManager {
	if log == nil {
		panic("ifmgr.NewAlertManager: log is required")
	}
	syntheticCfg := &config.Config{}
	if cfg.RepeatEvery > 0 {
		syntheticCfg.Notify.RepeatEvery = cfg.RepeatEvery.String()
	}
	n := notify.FromConfig(syntheticCfg, log, "mwan-ifmgr")
	return &AlertManager{n: n}
}

// WrapNotifier adapts an existing notify.Notifier to the AlertManager
// surface. The daemon path uses this so cmd/mwan can build the
// underlying Manager once via notify.FromConfig (with the email sink
// wired) and pass it through DaemonConfig.
func WrapNotifier(n notify.Notifier) *AlertManager {
	if n == nil {
		n = notify.NullNotifier{}
	}
	return &AlertManager{n: n}
}

// Notify emits an alert at the given level via the wrapped Notifier.
// The fields tail accepts the slog ...any pattern existing modules use
// (alternating key/value pairs) and is normalised into []slog.Attr
// before crossing the boundary.
func (a *AlertManager) Notify(
	now time.Time, level slog.Level, kind, key, msg string, fields ...any,
) {
	a.n.Notify(context.Background(), notify.Event{
		Now:     now,
		Level:   level,
		Kind:    kind,
		Key:     key,
		Message: msg,
		Fields:  fieldsToAttrs(fields),
	})
}

// Resolve clears the (kind, key) so the next Notify is treated as a
// fresh transition. The wrapped Notifier emits the recovery line at
// the level the original Notify used; see notify.Manager for the
// state-change semantics.
func (a *AlertManager) Resolve(now time.Time, kind, key, msg string, fields ...any) {
	// notify.Notifier.Resolve does not take a now; the wrapped Manager
	// reads its clock internally. The now argument is preserved on this
	// adapter only so module call sites stay unchanged.
	_ = now
	a.n.Resolve(context.Background(), kind, key, msg, fieldsToAttrs(fields)...)
}

// Active reports whether the named alert is currently in the "fired
// but not resolved" state. Forwards to the wrapped Notifier.
func (a *AlertManager) Active(kind, key string) bool {
	return a.n.Active(kind, key)
}

// fieldsToAttrs converts the variadic ...any tail used by ifmgr modules
// into []slog.Attr that notify.Event accepts. Pairs are alternating
// key (string) and value (any). Odd-length tails are tolerated by
// dropping the dangling key, mirroring slog's loose-pair behaviour.
func fieldsToAttrs(fields []any) []slog.Attr {
	if len(fields) == 0 {
		return nil
	}
	attrs := make([]slog.Attr, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		attrs = append(attrs, slog.Any(key, fields[i+1]))
	}
	return attrs
}
