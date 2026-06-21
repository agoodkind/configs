// Package ops wraps external host, guest, and transport operations for mwan.
package ops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ChannelName identifies one guest-management transport channel.
type ChannelName string

const (
	// ChanVsock is the primary gRPC-over-vsock channel to the guest agent.
	ChanVsock ChannelName = "vsock"
	// ChanTCP is the fallback TCP management channel to the guest agent.
	ChanTCP ChannelName = "tcp_mgmt"
	// ChanPVE is the Proxmox REST guest-exec fallback channel.
	ChanPVE ChannelName = "pve_rest"
)

type channelHealth struct {
	lastSuccess      time.Time
	lastFailure      time.Time
	lastError        string
	consecutiveFails int
	healthy          bool
}

// ChannelTracker records success and failure state for each transport channel.
type ChannelTracker struct {
	mu       sync.Mutex
	channels map[ChannelName]*channelHealth
	now      func() time.Time
}

// NewChannelTracker returns a tracker backed by the real wall clock.
func NewChannelTracker() *ChannelTracker {
	return NewChannelTrackerWithClock(time.Now)
}

// NewChannelTrackerWithClock returns a tracker that uses now for timestamps.
func NewChannelTrackerWithClock(now func() time.Time) *ChannelTracker {
	if now == nil {
		now = time.Now
	}
	return &ChannelTracker{
		mu: sync.Mutex{},
		channels: map[ChannelName]*channelHealth{
			ChanVsock: {},
			ChanTCP:   {},
			ChanPVE:   {},
		},
		now: now,
	}
}

func (t *ChannelTracker) recordSuccess(ch ChannelName) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.channels[ch]
	h.lastSuccess = t.now()
	h.consecutiveFails = 0
	h.lastError = ""
	h.healthy = true
}

func (t *ChannelTracker) recordFailure(ch ChannelName, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.channels[ch]
	h.lastFailure = t.now()
	h.consecutiveFails++
	if err != nil {
		h.lastError = err.Error()
	}
	h.healthy = false
}

// Summary returns a multi-line human-readable status of all three channels
// for inclusion in alert emails.
func (t *ChannelTracker) Summary() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var sb strings.Builder
	for _, name := range []ChannelName{ChanVsock, ChanTCP, ChanPVE} {
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
		fmt.Fprintf(&sb, "  %-10s %s\n", name, status)
	}
	return sb.String()
}

// LogAll emits one slog line per channel at DEBUG level.
func (t *ChannelTracker) LogAll(ctx context.Context, log *slog.Logger) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, name := range []ChannelName{ChanVsock, ChanTCP, ChanPVE} {
		h := t.channels[name]
		log.DebugContext(ctx, "channel health",
			"channel", name,
			"healthy", h.healthy,
			"consecutive_fails", h.consecutiveFails,
			"last_error", h.lastError,
			"last_success", h.lastSuccess,
			"last_failure", h.lastFailure,
		)
	}
}
