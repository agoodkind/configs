package opnsensesvc

import (
	"context"
	"errors"
	"time"
)

// WatchdogConfig controls the dispatcher wedge watchdog. The
// watchdog periodically checks whether the worker pool is making
// progress on queued jobs and, on detected wedge, invokes a Reset on
// the dispatcher. After EscalateAfter resets within EscalateWindow
// the watchdog returns ErrWatchdogEscalated so the supervisor
// (Proxmox rc.d) can restart the daemon. Visibility into every
// wedge and every reset lands on the logger as structured slog
// events.
type WatchdogConfig struct {
	// Interval is the period between wedge checks. A zero or negative
	// value disables the watchdog entirely.
	Interval time.Duration
	// IdleBudget is the maximum time the worker pool may go without
	// popping a queued job before the watchdog treats it as wedged.
	// Must be strictly larger than Interval to allow at least one
	// tick of headroom.
	IdleBudget time.Duration
	// EscalateAfter is the number of resets within EscalateWindow
	// that triggers escalation. Zero or negative disables escalation
	// so the watchdog never exits on its own.
	EscalateAfter int
	// EscalateWindow is the rolling window for counting resets.
	EscalateWindow time.Duration
}

// DefaultWatchdogConfig returns sensible production defaults: check
// every 5 s, treat 30 s of no popped jobs as a wedge, escalate after
// three resets within five minutes. Tests typically override every
// field to keep run time short.
func DefaultWatchdogConfig() WatchdogConfig {
	return WatchdogConfig{
		Interval:       5 * time.Second,
		IdleBudget:     30 * time.Second,
		EscalateAfter:  3,
		EscalateWindow: 5 * time.Minute,
	}
}

// ErrWatchdogEscalated is the sentinel returned by RunWatchdog when
// the dispatcher has been reset more times than the configured
// threshold within the rolling window. Callers convert this to a
// non-zero exit so the supervisor restarts the daemon.
var ErrWatchdogEscalated = errors.New("watchdog: escalated after repeated resets")

// RunWatchdog runs the watchdog loop until ctx is cancelled or the
// escalate threshold trips. It returns nil on clean ctx cancel and
// ErrWatchdogEscalated on escalation. A Watchdog with cfg.Interval
// <= 0 returns immediately, which lets callers disable the watchdog
// by zeroing the field. Safe to call concurrently with Serve.
func (d *Dispatcher) RunWatchdog(ctx context.Context, cfg WatchdogConfig) error {
	if cfg.Interval <= 0 {
		return nil
	}
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	var resetTimes []time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if !d.detectWedge(now, cfg.IdleBudget) {
				continue
			}
			d.recordWedge(now)
			_ = d.runReset()
			resetTimes = pruneResetTimes(append(resetTimes, now), now, cfg.EscalateWindow)
			if cfg.EscalateAfter > 0 && len(resetTimes) >= cfg.EscalateAfter {
				d.log.ErrorContext(ctx, "dispatcher: watchdog escalating",
					"err", ErrWatchdogEscalated,
					"resets_in_window", len(resetTimes),
					"escalate_after", cfg.EscalateAfter,
					"window_seconds", cfg.EscalateWindow.Seconds())
				return ErrWatchdogEscalated
			}
		}
	}
}

// detectWedge returns true when frameCh has queued work and the
// worker pool has not popped anything for longer than budget. The
// check is read-only and cheap so the watchdog can fire frequently
// without affecting hot paths.
func (d *Dispatcher) detectWedge(now time.Time, budget time.Duration) bool {
	if len(d.frameCh) == 0 {
		return false
	}
	last := d.lastFramePoppedNS.Load()
	if last == 0 {
		return false
	}
	elapsed := now.UnixNano() - last
	return elapsed > budget.Nanoseconds()
}

// recordWedge emits the structured detection log line just before
// the reset fires. Splitting detection logging from the reset call
// keeps the two signals separately observable in the log.
func (d *Dispatcher) recordWedge(now time.Time) {
	last := d.lastFramePoppedNS.Load()
	idleMs := int64(0)
	if last > 0 {
		idleMs = (now.UnixNano() - last) / int64(time.Millisecond)
	}
	d.log.Warn("dispatcher: watchdog detected wedge",
		"idle_ms", idleMs,
		"queued_jobs", len(d.frameCh))
}

// pruneResetTimes drops entries older than the rolling window so the
// escalation counter reflects only recent resets. The returned slice
// shares storage with the input; callers should treat the input
// slice as invalid after the call.
func pruneResetTimes(times []time.Time, now time.Time, window time.Duration) []time.Time {
	if window <= 0 {
		return times
	}
	cutoff := now.Add(-window)
	for len(times) > 0 && times[0].Before(cutoff) {
		times = times[1:]
	}
	return times
}
