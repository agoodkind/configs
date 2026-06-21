// Package alert coordinates alert delivery during rollback and outages.
package alert

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Coord: coordinates signal delivery during an in-progress rollback
// ---------------------------------------------------------------------------

// Coord tracks whether rollback is active and whether shutdown should follow it.
type Coord struct {
	mu                    sync.Mutex
	rollingBack           bool
	shutdownAfterRollback bool
}

// SetRollingBack records whether a rollback is in progress.
func (c *Coord) SetRollingBack(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollingBack = v
}

// IsRollingBack reports whether rollback is currently in progress.
func (c *Coord) IsRollingBack() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rollingBack
}

// OnSignalDuringRollback records that shutdown should happen after rollback.
func (c *Coord) OnSignalDuringRollback() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdownAfterRollback = true
}

// TakeShutdownAfterRollback reports and clears the deferred shutdown flag.
func (c *Coord) TakeShutdownAfterRollback() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.shutdownAfterRollback
	c.shutdownAfterRollback = false
	return v
}

// ---------------------------------------------------------------------------
// Limiter: prevents alert floods during sustained outages
// ---------------------------------------------------------------------------

// Limiter rate-limits partial and total outage alerts with one cooldown window.
type Limiter struct {
	mu                sync.Mutex
	nextPartialSendAt time.Time
	nextTotalSendAt   time.Time
	cooldown          time.Duration
}

// NewLimiter returns a limiter whose cooldown lasts cooldownSec seconds.
func NewLimiter(cooldownSec int) *Limiter {
	return &Limiter{
		mu:                sync.Mutex{},
		nextPartialSendAt: time.Time{},
		nextTotalSendAt:   time.Time{},
		cooldown:          time.Duration(cooldownSec) * time.Second,
	}
}

// TrySendPartial reports whether a partial outage alert may be sent now.
func (a *Limiter) TrySendPartial(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.nextPartialSendAt) {
		return false
	}
	a.nextPartialSendAt = now.Add(a.cooldown)
	return true
}

// TrySendTotal reports whether a total outage alert may be sent now.
func (a *Limiter) TrySendTotal(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.nextTotalSendAt) {
		return false
	}
	a.nextTotalSendAt = now.Add(a.cooldown)
	return true
}

// ResetCooldowns clears both alert cooldown windows.
func (a *Limiter) ResetCooldowns() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextPartialSendAt = time.Time{}
	a.nextTotalSendAt = time.Time{}
}

// PartialCooldownRemaining returns the remaining partial alert cooldown.
func (a *Limiter) PartialCooldownRemaining(now time.Time) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.nextPartialSendAt) {
		return a.nextPartialSendAt.Sub(now)
	}
	return 0
}

// TotalCooldownRemaining returns the remaining total alert cooldown.
func (a *Limiter) TotalCooldownRemaining(now time.Time) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.nextTotalSendAt) {
		return a.nextTotalSendAt.Sub(now)
	}
	return 0
}
