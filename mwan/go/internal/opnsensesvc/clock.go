package opnsensesvc

import "time"

// Clock supplies wall time for operations that need testable timestamps.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

func clockOrReal(candidate Clock) Clock {
	if candidate != nil {
		return candidate
	}
	return realClock{}
}
