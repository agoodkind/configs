package opnsensesvc

import internalclock "goodkind.io/mwan/internal/clock"

// Clock supplies wall time for operations that need testable timestamps.
type Clock = internalclock.Clock

type realClock = internalclock.Real

func clockOrReal(candidate Clock) Clock {
	if candidate != nil {
		return candidate
	}
	return realClock{}
}
