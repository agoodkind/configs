//go:build linux

package oob

import (
	"log/slog"
	"sync"
	"time"
)

// AlertManager evaluates daemon state and emits alerts via the configured
// slog.Logger. The email handler in pkg/emaillog picks up WARN/ERROR
// records and dispatches mail with its own per-process cooldown. We layer
// our own per-alert-kind transition detection on top so we only fire when
// state actually changes (and re-fire periodically while a problem
// persists, so the email handler eventually re-sends after its cooldown).
type AlertManager struct {
	cfg AlertConfig
	log *slog.Logger

	mu         sync.Mutex
	raLostAt   time.Time // zero when RA is healthy
	raLostFlag bool      // true when last evaluation said "lost"
	v4LostAt   time.Time
	v4LostFlag bool

	// repeat-emit timestamps so we re-emit periodically
	raLostNextEmit time.Time
	v4LostNextEmit time.Time
}

// AlertConfig defines thresholds for alert transitions.
type AlertConfig struct {
	RALostAfter      time.Duration // RA-learned default missing this long -> WARN
	V4LeaseLostAfter time.Duration // No BOUND lease this long -> ERROR
	RepeatEvery      time.Duration // While a problem persists, re-emit at this cadence
}

// NewAlertManager constructs an AlertManager with the given config.
func NewAlertManager(log *slog.Logger, cfg AlertConfig) *AlertManager {
	if cfg.RepeatEvery == 0 {
		cfg.RepeatEvery = 30 * time.Minute
	}
	return &AlertManager{
		cfg: cfg,
		log: log.With("component", "alerts"),
	}
}

// EvaluateRA inspects the V6Manager.LastRASeen() and emits alerts when the
// RA-learned default has been missing too long. Call from the daemon's
// periodic reconcile tick.
func (a *AlertManager) EvaluateRA(lastSeen time.Time, now time.Time) {
	if lastSeen.IsZero() {
		// No baseline yet; ignore until the first observation.
		a.log.Debug("alerts: RA never observed yet; skipping evaluation")
		return
	}
	age := now.Sub(lastSeen)
	a.mu.Lock()
	defer a.mu.Unlock()

	if age >= a.cfg.RALostAfter {
		// In a "lost" state. Emit on transition; then re-emit every RepeatEvery.
		if !a.raLostFlag {
			a.raLostFlag = true
			a.raLostAt = now
			a.log.Warn("oob: RA-learned default missing",
				"alert", "ra-lost",
				"age", age.String(),
				"last_seen", lastSeen.UTC().Format(time.RFC3339),
				"threshold", a.cfg.RALostAfter.String(),
			)
			a.raLostNextEmit = now.Add(a.cfg.RepeatEvery)
			return
		}
		if now.After(a.raLostNextEmit) {
			a.log.Warn("oob: RA-learned default still missing",
				"alert", "ra-lost-repeat",
				"age", age.String(),
				"missing_for", now.Sub(a.raLostAt).String(),
			)
			a.raLostNextEmit = now.Add(a.cfg.RepeatEvery)
		} else {
			a.log.Debug("alerts: RA still lost, before next-emit",
				"age", age.String(),
				"next_emit_in", time.Until(a.raLostNextEmit).String())
		}
		return
	}

	// Healthy. Emit restoration on the transition out of "lost".
	if a.raLostFlag {
		downFor := now.Sub(a.raLostAt)
		a.log.Info("oob: RA-learned default restored",
			"alert", "ra-restored",
			"down_for", downFor.String(),
		)
		a.raLostFlag = false
		a.raLostAt = time.Time{}
	} else {
		a.log.Debug("alerts: RA healthy", "age", age.String())
	}
}

// EvaluateV4 inspects V4Manager.LastBound() and emits alerts when no lease
// has been BOUND in too long.
func (a *AlertManager) EvaluateV4(lastBound time.Time, now time.Time) {
	if lastBound.IsZero() {
		a.log.Debug("alerts: V4 never bound yet; skipping evaluation")
		return
	}
	age := now.Sub(lastBound)
	a.mu.Lock()
	defer a.mu.Unlock()

	if age >= a.cfg.V4LeaseLostAfter {
		if !a.v4LostFlag {
			a.v4LostFlag = true
			a.v4LostAt = now
			a.log.Error("oob: DHCPv4 lease not renewed",
				"alert", "v4-lost",
				"age", age.String(),
				"last_bound", lastBound.UTC().Format(time.RFC3339),
				"threshold", a.cfg.V4LeaseLostAfter.String(),
			)
			a.v4LostNextEmit = now.Add(a.cfg.RepeatEvery)
			return
		}
		if now.After(a.v4LostNextEmit) {
			a.log.Error("oob: DHCPv4 lease still not renewed",
				"alert", "v4-lost-repeat",
				"age", age.String(),
				"missing_for", now.Sub(a.v4LostAt).String(),
			)
			a.v4LostNextEmit = now.Add(a.cfg.RepeatEvery)
		}
		return
	}

	if a.v4LostFlag {
		downFor := now.Sub(a.v4LostAt)
		a.log.Info("oob: DHCPv4 lease restored",
			"alert", "v4-restored",
			"down_for", downFor.String(),
		)
		a.v4LostFlag = false
		a.v4LostAt = time.Time{}
	}
}

// NotifyRenumber emits a one-shot WARN when the daemon detects a SLAAC prefix
// change. This is informational; the renumber itself is handled by the kernel
// and the OOB table sync is automatic via the route monitor.
func (a *AlertManager) NotifyRenumber(oldPrefix, newPrefix string) {
	a.log.Warn("oob: MB renumbered our SLAAC prefix",
		"alert", "renumber",
		"old", oldPrefix,
		"new", newPrefix,
	)
}

// NotifyDHCPLeaseChange emits an INFO record when the lease IP differs from
// the last observed IP. Useful for tracking how often MB rotates us.
func (a *AlertManager) NotifyDHCPLeaseChange(oldCIDR, newCIDR string) {
	a.log.Info("oob: DHCPv4 lease IP changed",
		"alert", "dhcp-lease-change",
		"old", oldCIDR,
		"new", newCIDR,
	)
}
