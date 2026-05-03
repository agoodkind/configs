package opnsenseclient

import "time"

// Clock supplies wall time for retry accounting.
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
