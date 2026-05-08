package upgrade

import "time"

// realClock is the wall-clock implementation of Clock. Tests inject a
// fixed-time fake; production code uses this. The package keeps the
// helper in its own file so the linter recognizes it as a canonical
// clock helper, matching the convention used by internal/notify and
// internal/agent.
type realClock struct{}

// Now returns the current wall-clock time.
func (realClock) Now() time.Time {
	return time.Now()
}
