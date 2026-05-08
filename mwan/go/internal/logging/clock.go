package logging

import "time"

// emailClock supplies wall time for the email-handler cooldown limiter
// so tests can swap in a fake clock without spinning up the real
// smtp2go path or relying on [time.Sleep].
type emailClock interface {
	Now() time.Time
}

type realEmailClock struct{}

// Now satisfies emailClock for production callers.
func (realEmailClock) Now() time.Time {
	return time.Now()
}
