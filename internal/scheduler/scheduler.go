package scheduler

import (
	"sync/atomic"
	"time"

	"github.com/zhaori96/crono/internal/buffer"
	"github.com/zhaori96/crono/internal/util"
	"github.com/zhaori96/crono/internal/wheel"
)

type Scheduler[T any] struct {
	_ util.NoCopy

	resourceBuffer buffer.FixedBuffer[T]
	timeWheel      *wheel.Wheel

	idleTimeout time.Duration

	releaseErrorHandler func(error)

	activeLeases   atomic.Uint64
	acquiredCount  atomic.Uint64
	releasedCount  atomic.Uint64
	expiredCount   atomic.Uint64
	resetCount     atomic.Uint64
	keepAliveCount atomic.Uint64

	state atomic.Uint32
}

type schedulerState uint32

const (
	schedulerStateActive schedulerState = iota
	schedulerStateClosed
)

func NewScheduler[T any](
	resourceBuffer buffer.FixedBuffer[T],
	timeWheel *wheel.Wheel,
	options ...Option,
) (*Scheduler[T], error) {
	if resourceBuffer == nil {
		return nil, ErrNilBuffer
	}
	if timeWheel == nil {
		return nil, ErrNilWheel
	}

	configurationData := defaultConfiguration()
	for _, option := range options {
		option(&configurationData)
	}

	if configurationData.idleTimeout <= 0 {
		return nil, ErrInvalidIdleTimeout
	}

	scheduler := &Scheduler[T]{
		resourceBuffer:      resourceBuffer,
		timeWheel:           timeWheel,
		idleTimeout:         configurationData.idleTimeout,
		releaseErrorHandler: configurationData.releaseErrorHandler,
	}

	return scheduler, nil
}

func (s *Scheduler[T]) Closed() bool {
	return s.state.Load() == uint32(schedulerStateClosed)
}

func (s *Scheduler[T]) Close() {
	if s.state.CompareAndSwap(
		uint32(schedulerStateActive),
		uint32(schedulerStateClosed),
	) {
		s.resourceBuffer.Close()
	}
}

func (s *Scheduler[T]) IdleTimeout() time.Duration {
	return s.idleTimeout
}

func (s *Scheduler[T]) Put(value T) error {
	if s.Closed() {
		return ErrSchedulerClosed
	}
	return s.resourceBuffer.Put(value)
}

func (s *Scheduler[T]) Acquire() (*Lease[T], error) {
	return s.acquireWithTimeout(s.idleTimeout)
}

func (s *Scheduler[T]) AcquireWithTimeout(
	idleTimeout time.Duration,
) (*Lease[T], error) {
	if idleTimeout <= 0 {
		return nil, ErrNonPositiveTimeout
	}
	return s.acquireWithTimeout(idleTimeout)
}

func (s *Scheduler[T]) acquireWithTimeout(
	idleTimeout time.Duration,
) (*Lease[T], error) {
	if s.Closed() {
		return nil, ErrSchedulerClosed
	}
	if !s.timeWheel.Running() {
		return nil, ErrWheelNotRunning
	}

	value, acquireError := s.resourceBuffer.Get()
	if acquireError != nil {
		return nil, acquireError
	}

	lease := &Lease[T]{}
	lease.binding.initialize(s, value, idleTimeout)

	handle, scheduleError := s.timeWheel.Schedule(&lease.binding, idleTimeout)
	if scheduleError != nil {
		s.releaseValue(value)
		return nil, scheduleError
	}
	lease.handle = handle
	lease.binding.activate()

	return lease, nil
}

func (s *Scheduler[T]) releaseValue(value T) error {
	releaseError := s.resourceBuffer.Release(value)
	if releaseError != nil && s.releaseErrorHandler != nil {
		s.releaseErrorHandler(releaseError)
	}
	return releaseError
}

func (s *Scheduler[T]) onLeaseActivated() {
	s.acquiredCount.Add(1)
	s.activeLeases.Add(1)
}

func (s *Scheduler[T]) onLeaseReleased() {
	s.releasedCount.Add(1)
	s.decrementActiveLeases()
}

func (s *Scheduler[T]) onLeaseExpired() {
	s.expiredCount.Add(1)
	s.decrementActiveLeases()
}

func (s *Scheduler[T]) onLeaseTimeoutReset() {
	s.resetCount.Add(1)
}

func (s *Scheduler[T]) onLeaseKeepAlive() {
	s.keepAliveCount.Add(1)
}

func (s *Scheduler[T]) decrementActiveLeases() {
	for {
		current := s.activeLeases.Load()
		if current == 0 {
			return
		}
		if s.activeLeases.CompareAndSwap(current, current-1) {
			return
		}
	}
}
