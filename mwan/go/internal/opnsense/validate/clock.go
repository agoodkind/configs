package validate

import "time"

// clock is the canonical time seam for the validate package. The
// real implementation calls [time.Now]; tests inject a fake to pin
// timestamps deterministically.
type clock interface {
	Now() time.Time
}

// realClock returns the real wall-clock time. The [time.Now] call
// lives here because this is the canonical clock helper for the
// validate package.
type realClock struct{}

// Now returns the current wall-clock time.
func (realClock) Now() time.Time {
	return time.Now()
}
