package main

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// watchdogCoord: coordinates signal delivery during an in-progress rollback
// ---------------------------------------------------------------------------

type watchdogCoord struct {
	mu                    sync.Mutex
	rollingBack           bool
	shutdownAfterRollback bool
}

func (c *watchdogCoord) setRollingBack(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollingBack = v
}

func (c *watchdogCoord) isRollingBack() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rollingBack
}

func (c *watchdogCoord) onSignalDuringRollback() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdownAfterRollback = true
}

func (c *watchdogCoord) takeShutdownAfterRollback() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.shutdownAfterRollback
	c.shutdownAfterRollback = false
	return v
}

// ---------------------------------------------------------------------------
// alertLimiter: prevents alert floods during sustained outages
// ---------------------------------------------------------------------------

type alertLimiter struct {
	mu                sync.Mutex
	nextPartialSendAt time.Time
	nextTotalSendAt   time.Time
	cooldown          time.Duration
}

func newAlertLimiter(cooldownSec int) *alertLimiter {
	return &alertLimiter{cooldown: time.Duration(cooldownSec) * time.Second}
}

func (a *alertLimiter) trySendPartial(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.nextPartialSendAt) {
		return false
	}
	a.nextPartialSendAt = now.Add(a.cooldown)
	return true
}

func (a *alertLimiter) trySendTotal(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.nextTotalSendAt) {
		return false
	}
	a.nextTotalSendAt = now.Add(a.cooldown)
	return true
}

func (a *alertLimiter) resetCooldowns() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextPartialSendAt = time.Time{}
	a.nextTotalSendAt = time.Time{}
}

func (a *alertLimiter) partialCooldownRemaining(now time.Time) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.nextPartialSendAt) {
		return a.nextPartialSendAt.Sub(now)
	}
	return 0
}

func (a *alertLimiter) totalCooldownRemaining(now time.Time) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.nextTotalSendAt) {
		return a.nextTotalSendAt.Sub(now)
	}
	return 0
}
