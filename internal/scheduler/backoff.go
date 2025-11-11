package scheduler

import (
	"sync/atomic"
	"time"
)

// Backoff configuration for exponential backoff between constructor retries.
type Backoff struct {
	// Enabled determines if backoff is active.
	Enabled bool

	// InitialDelay is the delay after the first failure.
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration

	// Multiplier is applied to the delay after each failure.
	// Example: 2.0 means delay doubles after each failure.
	Multiplier float64
}

func NewDefaultBackoff() *Backoff {
	return &Backoff{
		Enabled:      true,
		InitialDelay: time.Second,
		MaxDelay:     5 * time.Second,
		Multiplier:   1.2,
	}
}

type backoff struct {
	configuration    *Backoff
	currentDelay     atomic.Int64
	lastAttemptTime  atomic.Int64
	consecutiveFails atomic.Uint32
}

func newBackoff(configuration *Backoff) *backoff {
	if configuration == nil {
		return nil
	}

	implementation := &backoff{
		configuration: configuration,
	}

	implementation.currentDelay.Store(int64(configuration.InitialDelay))

	return implementation
}

func (b *backoff) ShouldWait() bool {
	if b == nil || !b.configuration.Enabled {
		return false
	}

	lastAttempt := time.Unix(0, b.lastAttemptTime.Load())
	if lastAttempt.IsZero() {
		return false
	}

	currentDelay := time.Duration(b.currentDelay.Load())
	return time.Since(lastAttempt) < currentDelay
}

func (b *backoff) NextDelay() time.Duration {
	if b == nil || !b.configuration.Enabled {
		return 0
	}

	b.lastAttemptTime.Store(time.Now().UnixNano())
	return time.Duration(b.currentDelay.Load())
}

func (b *backoff) RecordFailure() {
	if b == nil || !b.configuration.Enabled {
		return
	}

	fails := b.consecutiveFails.Add(1)

	// Calculate new delay with exponential backoff
	currentDelay := time.Duration(b.currentDelay.Load())
	newDelay := time.Duration(float64(currentDelay) * b.configuration.Multiplier)

	// Cap at max delay
	if newDelay > b.configuration.MaxDelay {
		newDelay = b.configuration.MaxDelay
	}

	// Ensure we don't overflow
	if newDelay < currentDelay {
		newDelay = b.configuration.MaxDelay
	}

	b.currentDelay.Store(int64(newDelay))
	b.lastAttemptTime.Store(time.Now().UnixNano())

	_ = fails // Used for potential future metrics
}

func (b *backoff) RecordSuccess() {
	if b == nil || !b.configuration.Enabled {
		return
	}

	b.consecutiveFails.Store(0)
	b.currentDelay.Store(int64(b.configuration.InitialDelay))
}
