package main

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type channelName string

const (
	chanVsock channelName = "vsock"
	chanTCP   channelName = "tcp_mgmt"
	chanPVE   channelName = "pve_rest"
)

type channelHealth struct {
	lastSuccess      time.Time
	lastFailure      time.Time
	lastError        string
	consecutiveFails int
	healthy          bool
}

type channelTracker struct {
	mu       sync.Mutex
	channels map[channelName]*channelHealth
}

func newChannelTracker() *channelTracker {
	return &channelTracker{
		channels: map[channelName]*channelHealth{
			chanVsock: {},
			chanTCP:   {},
			chanPVE:   {},
		},
	}
}

func (t *channelTracker) recordSuccess(ch channelName) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.channels[ch]
	h.lastSuccess = time.Now()
	h.consecutiveFails = 0
	h.lastError = ""
	h.healthy = true
}

func (t *channelTracker) recordFailure(ch channelName, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.channels[ch]
	h.lastFailure = time.Now()
	h.consecutiveFails++
	if err != nil {
		h.lastError = err.Error()
	}
	h.healthy = false
}

// summary returns a multi-line human-readable status of all three channels
// for inclusion in alert emails.
func (t *channelTracker) summary() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var sb strings.Builder
	for _, name := range []channelName{chanVsock, chanTCP, chanPVE} {
		h := t.channels[name]
		status := "NEVER_USED"
		if !h.lastSuccess.IsZero() || !h.lastFailure.IsZero() {
			if h.healthy {
				status = fmt.Sprintf("OK  (last_success=%s)", h.lastSuccess.Format(time.RFC3339))
			} else {
				status = fmt.Sprintf("FAIL(consecutive=%d last_err=%q last_failure=%s)",
					h.consecutiveFails, h.lastError, h.lastFailure.Format(time.RFC3339))
			}
		}
		sb.WriteString(fmt.Sprintf("  %-10s %s\n", name, status))
	}
	return sb.String()
}

// logAll emits one slog line per channel at INFO level.
func (t *channelTracker) logAll(log *slog.Logger) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, name := range []channelName{chanVsock, chanTCP, chanPVE} {
		h := t.channels[name]
		log.Info("channel health",
			"channel", name,
			"healthy", h.healthy,
			"consecutive_fails", h.consecutiveFails,
			"last_error", h.lastError,
			"last_success", h.lastSuccess,
			"last_failure", h.lastFailure,
		)
	}
}
