package cutover2

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultAutoRollbackThreshold is how long connectivity can be down
	// before auto-rollback triggers. Set conservatively to avoid firing
	// during the expected FRR stop+start window (~18s on testbed, longer
	// on production).
	defaultAutoRollbackThreshold = 45 * time.Second

	// healthProbeInterval is how often the auto-rollback monitor checks connectivity.
	healthProbeInterval = 2 * time.Second
)

// healthMonitor runs continuous connectivity checks in a goroutine.
// If connectivity drops for longer than the threshold, it calls the
// rollback function and cancels the context.
//
// The monitor can be paused during expected downtime windows (e.g. FRR
// stop+start) to avoid false triggers.
type healthMonitor struct {
	log          *slog.Logger
	threshold    time.Duration
	interval     time.Duration
	onRollback   func()
	cancel       context.CancelFunc
	failingSince atomic.Value // stores time.Time
	triggered    atomic.Bool

	mu     sync.Mutex
	paused bool
}

// startHealthMonitor begins monitoring and returns the monitor.
// Call monitor.Stop() to stop, monitor.Pause()/Resume() during expected downtime.
func startHealthMonitor(ctx context.Context, log *slog.Logger, rollbackFn func(), cancel context.CancelFunc) *healthMonitor {
	m := &healthMonitor{
		log:        log,
		threshold:  defaultAutoRollbackThreshold,
		interval:   healthProbeInterval,
		onRollback: rollbackFn,
		cancel:     cancel,
	}

	go m.run(ctx)

	return m
}

// Pause suspends health checking. The failure timer is reset.
// Use during expected downtime (FRR stop+start).
func (m *healthMonitor) Pause() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused = true
	m.failingSince.Store(time.Time{})
	m.log.Info("auto-rollback monitor paused")
}

// Resume resumes health checking after a pause.
func (m *healthMonitor) Resume() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused = false
	m.failingSince.Store(time.Time{})
	m.log.Info("auto-rollback monitor resumed")
}

// Stop terminates the monitor goroutine.
func (m *healthMonitor) Stop() {
	m.cancel()
}

func (m *healthMonitor) isPaused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused
}

func (m *healthMonitor) run(ctx context.Context) {
	m.log.Info("auto-rollback monitor started", "threshold", m.threshold.String(), "interval", m.interval.String())

	for {
		select {
		case <-ctx.Done():
			m.log.Info("auto-rollback monitor stopped")
			return
		case <-time.After(m.interval):
		}

		if m.isPaused() {
			continue
		}

		ok := probe(ctx)
		if ok {
			m.failingSince.Store(time.Time{})
			continue
		}

		// Connectivity down
		now := time.Now()
		stored, _ := m.failingSince.Load().(time.Time)
		if stored.IsZero() {
			m.failingSince.Store(now)
			m.log.Warn("auto-rollback: connectivity lost, starting threshold timer")
			continue
		}

		downFor := now.Sub(stored)
		if downFor >= m.threshold && !m.triggered.Load() {
			m.triggered.Store(true)
			m.log.Error("auto-rollback: TRIGGERED",
				"down_for", downFor.String(),
				"threshold", m.threshold.String(),
				"err", "connectivity loss exceeded auto-rollback threshold")
			m.onRollback()
			m.cancel()
			return
		}

		m.log.Warn("auto-rollback: connectivity still down",
			"down_for", downFor.Round(time.Second).String(),
			"remaining", (m.threshold - downFor).Round(time.Second).String())
	}
}

// probe tests connectivity with a quick ping. Returns true if reachable.
func probe(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if exec.CommandContext(ctx, "ping", "-c", "1", "-W", "2", "1.1.1.1").Run() == nil {
		return true
	}
	return exec.CommandContext(ctx, "ping6", "-c", "1", "-W", "2", "2606:4700:4700::1111").Run() == nil
}
