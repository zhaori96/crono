package util

import (
	"time"
)

type ExponentialSleeper struct {
	current time.Duration
	maximum time.Duration
}

func NewExponentialSleeper(startDuration, maximumDuration time.Duration) ExponentialSleeper {
	if startDuration <= 0 {
		startDuration = time.Microsecond
	}
	if maximumDuration < startDuration {
		maximumDuration = startDuration
	}
	return ExponentialSleeper{
		current: startDuration,
		maximum: maximumDuration,
	}
}

func (s *ExponentialSleeper) Pause() {
	time.Sleep(s.current)
	nextDuration := s.current * 2
	if nextDuration > s.maximum {
		nextDuration = s.maximum
	}
	s.current = nextDuration
}
