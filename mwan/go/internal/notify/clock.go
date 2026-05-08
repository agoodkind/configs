package notify

import "time"

// clock is the test seam for [time.Now]. Production code uses
// realClock; the state-change tests inject a fake so they can advance
// time deterministically without driving wall-clock waits.
type clock interface {
	Now() time.Time
}

// realClock returns the real wall-clock time. The [time.Now] call
// lives here because this is the canonical clock helper for the
// notify package.
type realClock struct{}

// Now returns the current wall-clock time.
func (realClock) Now() time.Time {
	return time.Now()
}
