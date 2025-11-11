package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhaori96/crono/internal/buffer"
	"github.com/zhaori96/crono/internal/util"
	"github.com/zhaori96/crono/internal/wheel"
)

// RecreationPolicy defines when the scheduler should recreate expired or uninitialized resources.
type RecreationPolicy int

const (
	// RecreateNever means resources are never recreated after expiration.
	// If all resources expire, the scheduler becomes degraded.
	RecreateNever RecreationPolicy = iota

	// RecreateAlways means expired resources are always recreated on next acquisition.
	// Maintains pool functionality even if all resources expire.
	// May mask configuration issues (e.g., timeout too short).
	RecreateAlways
)

type schedulerState uint32

const (
	schedulerStateActive schedulerState = iota
	schedulerStateClosed
)

type Scheduler[T any] struct {
	_ util.NoCopy

	// Core
	buffer      buffer.FixedBuffer[*Lease[T]]
	timeWheel   *wheel.Wheel
	ownsWheel   bool
	idleTimeout time.Duration
	state       atomic.Value

	// Lazy Creation
	constructor      func() (T, error)
	recreationPolicy RecreationPolicy
	maxCapacity      int

	// Lifecycle
	destructor func(T)
	onExpired  func(T)

	// Error Handling
	constructorErrorHandler func(error)
	releaseErrorHandler     func(error)
	maxConstructorRetries   uint32

	// Resiliency
	circuitBreaker *circuitBreaker
	backoff        *backoff

	// Metrics
	acquiredCount atomic.Uint64
	releasedCount atomic.Uint64
	expiredCount  atomic.Uint64

	configure sync.Once
}

func NewScheduler[T any](
	constructor func() (T, error),
	capacity int,
	options ...Option,
) (*Scheduler[T], error) {
	config := parseConfiguration(options)
	return newScheduler(nil, constructor, capacity, config)
}

// NewSchedulerWith creates a scheduler with lazy resource creation using a Configuration struct.
// This variant accepts a declarative configuration instead of functional options,
// which can be useful for complex setups or configuration loaded from files.
func NewSchedulerWith[T any](
	constructor func() (T, error),
	capacity int,
	config Configuration,
) (*Scheduler[T], error) {
	return newScheduler(nil, constructor, capacity, config)
}

func NewPreloadedScheduler[T any](
	resources []T,
	options ...Option,
) (*Scheduler[T], error) {
	config := parseConfiguration(options)
	return newScheduler(resources, nil, 0, config)
}

// NewPreloadedSchedulerWith creates a scheduler with pre-created resources using a Configuration struct.
// This variant accepts a declarative configuration instead of functional options,
// which can be useful for complex setups or configuration loaded from files.
func NewPreloadedSchedulerWith[T any](
	resources []T,
	config Configuration,
) (*Scheduler[T], error) {
	return newScheduler(resources, nil, 0, config)
}

func newScheduler[T any](
	resources []T,
	constructor func() (T, error),
	capacity int,
	config Configuration,
) (*Scheduler[T], error) {
	totalResources := len(resources)
	if totalResources == 0 && constructor == nil {
		return nil, ErrNilConstructor
	}

	if totalResources == 0 && capacity == 0 {
		return nil, ErrZeroCapacity
	}

	if err := config.Resolve(); err != nil {
		return nil, err
	}

	scheduler := &Scheduler[T]{maxCapacity: capacity}
	scheduler.readConfig(config)

	leases := NewLeasesFrom(resources, capacity)
	buffer, err := buffer.NewFixedBuffer(0, buffer.AccessModeStrategic, leases...)
	if err != nil {
		if scheduler.ownsWheel {
			cleanupWheel(scheduler.timeWheel)
		}
		return nil, err
	}

	scheduler.buffer = buffer
	scheduler.backoff = newBackoff(config.Backoff)
	scheduler.circuitBreaker = newCircuitBreaker(config.CircuitBreaker)
	scheduler.state.Store(schedulerStateActive)

	return scheduler, nil
}

func (s *Scheduler[T]) Closed() bool {
	return s.state.Load() == schedulerStateClosed
}

func (s *Scheduler[T]) IdleTimeout() time.Duration {
	return s.idleTimeout
}

func (s *Scheduler[T]) Acquire() (T, Releaser, error) {
	return s.AcquireWithTimeout(s.idleTimeout)
}

func (s *Scheduler[T]) AcquireWithTimeout(
	idleTimeout time.Duration,
) (T, Releaser, error) {
	return s.acquireWithTimeout(idleTimeout)
}

func (s *Scheduler[T]) Close() {
	if !s.state.CompareAndSwap(schedulerStateActive, schedulerStateClosed) {
		return
	}

	if s.destructor != nil {
		for {
			lease, err := s.buffer.Get()
			if err != nil {
				break
			}

			if lease.isActive() {
				s.destructor(lease.value)
			}
		}
	}

	s.buffer.Close()

	// Stop wheel if we own it
	if s.ownsWheel {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.timeWheel.Stop(ctx); err != nil {
			// Silently ignore error (no logger in scheduler)
		}
	}
}

func (s *Scheduler[T]) readConfig(config Configuration) {
	s.configure.Do(func() {
		s.timeWheel = config.Wheel
		s.ownsWheel = config.ownsWheel
		s.idleTimeout = config.IdleTimeout
		s.recreationPolicy = config.RecreationPolicy
		s.destructor = extractDestructor[T](config.Destructor)
		s.onExpired = extractOnExpired[T](config.OnExpired)
		s.constructorErrorHandler = config.ConstructorErrorHandler
		s.releaseErrorHandler = config.ReleaseErrorHandler
		s.maxConstructorRetries = config.MaxConstructorRetries
	})
}

func (s *Scheduler[T]) acquireWithTimeout(
	idleTimeout time.Duration,
) (T, Releaser, error) {
	var zero T

	if s.Closed() {
		return zero, nil, ErrSchedulerClosed
	}

	if !s.timeWheel.Running() {
		return zero, nil, ErrWheelNotRunning
	}

	lease, err := s.buffer.Get()
	if err != nil {
		return zero, nil, err
	}

	if !lease.isInitialized() {
		return s.lazyAcquireWithTimeout(lease, idleTimeout)
	}

	if lease.Expired() {
		if err := s.cleanupExpiredLease(lease, false); err != nil {
			return zero, nil, err
		}
		return s.lazyAcquireWithTimeout(lease, idleTimeout)
	}

	s.prepareLease(lease, idleTimeout)
	s.acquiredCount.Add(1)
	return lease.value, lease, nil
}

func (s *Scheduler[T]) lazyAcquireWithTimeout(
	lease *Lease[T],
	idleTimeout time.Duration,
) (T, Releaser, error) {
	var zero T
	for {
		if lease == nil || !s.canRecreate(lease) {
			recoverable, err := s.getRecoverableLease()
			if err != nil {
				return zero, nil, err
			}
			lease = recoverable
		}

		if ok, err := s.tryInitializeLease(lease); err != nil {
			s.releaseToBuffer(lease)
			if !ok {
				return zero, nil, fmt.Errorf("%w: %w", ErrMaxRetriesExceeded, err)
			}
			lease = nil
			continue
		}

		s.prepareLease(lease, idleTimeout)
		s.acquiredCount.Add(1)
		return lease.value, lease, nil
	}
}

func (s *Scheduler[T]) prepareLease(
	lease *Lease[T],
	idleTimeout time.Duration,
) {
	if lease.handler != nil {
		lease.handler.Cancel()
		lease.handler = nil
	}

	lease.scheduler = s
	lease.idleTimeout = idleTimeout
	lease.setActive()
}

func (s *Scheduler[T]) getRecoverableLease() (*Lease[T], error) {
	availables := s.buffer.Metrics().Available()
	if availables == 0 {
		return nil, ErrNoAvailableResources
	}

	for range availables {

		lease, err := s.buffer.Get()
		if err != nil {
			return nil, err
		}

		if lease.Expired() {
			if err := s.cleanupExpiredLease(lease, false); err != nil {
				continue
			}
		}

		if s.canRecreate(lease) {
			return lease, nil
		}

		_ = s.releaseToBuffer(lease)
	}
	return nil, ErrNoAvailableResources
}

func (s *Scheduler[T]) canRecreate(lease *Lease[T]) bool {
	if !lease.isInitialized() {
		return true
	}

	if s.recreationPolicy != RecreateAlways {
		return false
	}

	return true
}

func (s *Scheduler[T]) tryInitializeLease(lease *Lease[T]) (bool, error) {
	if s.constructor == nil {
		return false, errors.New("lazy constructor not defined")
	}

	if s.circuitBreaker.IsOpen() {
		return false, ErrCircuitBreakerOpen
	}

	if s.backoff != nil && s.backoff.ShouldWait() {
		time.Sleep(s.backoff.NextDelay())
	}

	value, err := s.constructor()
	if err != nil {
		if s.constructorErrorHandler != nil {
			s.constructorErrorHandler(err)
		}

		s.circuitBreaker.RecordFailure()
		s.backoff.RecordFailure()
		return true, errors.New("constructor has been failed")
	}

	if s.circuitBreaker != nil {
		s.circuitBreaker.RecordSuccess()
	}

	if s.backoff != nil {
		s.backoff.RecordSuccess()
	}

	lease.value = value
	lease.setInitialized()
	return true, nil
}

func (s *Scheduler[T]) release(
	lease *Lease[T],
	idleTimeout time.Duration,
) error {
	if lease == nil {
		return ErrInvalidState
	}

	if !lease.setIdle() {
		return ErrInvalidState
	}

	handler, err := s.timeWheel.Schedule(lease, idleTimeout)
	if err != nil {
		lease.Expire()
		s.cleanupExpiredLease(lease, true)
		return err
	}

	lease.setHandler(handler)
	s.releasedCount.Add(1)

	err = s.buffer.Put(lease)
	if err != nil {
		lease.scheduler = nil
		lease.idleTimeout = 0
	}

	return err
}

func (s *Scheduler[T]) releaseToBuffer(lease *Lease[T]) error {
	if err := s.buffer.Put(lease); err != nil {
		if s.releaseErrorHandler != nil {
			s.releaseErrorHandler(err)
		}
		return err
	}
	return nil
}

func (s *Scheduler[T]) cleanupExpiredLease(
	lease *Lease[T],
	shouldReleaseToBuffer bool,
) error {
	if lease == nil {
		return nil
	}
	defer s.expiredCount.Add(1)

	if lease.handler != nil {
		lease.handler.Cancel()
		lease.handler = nil
	}

	defer lease.reset()
	if shouldReleaseToBuffer {
		if err := s.releaseToBuffer(lease); err != nil {
			return err
		}
	}

	if s.destructor != nil && lease.isActive() {
		s.destructor(lease.value)
	}

	return nil
}

// Helper functions

func parseConfiguration(options []Option) Configuration {
	configuration := defaultConfiguration()
	for _, option := range options {
		option(&configuration)
	}
	return configuration
}

func resolveWheel(configuration *Configuration) error {
	if configuration.Wheel != nil {
		return nil
	}

	tickInterval := configuration.IdleTimeout / 20
	if tickInterval < 10*time.Millisecond {
		tickInterval = 10 * time.Millisecond
	}

	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(tickInterval),
		wheel.WithSlotCount(64),
	)
	if err != nil {
		return err
	}

	if err := timeWheel.Start(); err != nil {
		return err
	}

	configuration.Wheel = timeWheel
	configuration.ownsWheel = true
	return nil
}

func cleanupWheel(timeWheel *wheel.Wheel) {
	if timeWheel != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = timeWheel.Stop(ctx)
	}
}

func extractDestructor[T any](generic func(any)) func(T) {
	if generic == nil {
		return nil
	}
	return func(value T) {
		generic(value)
	}
}

func extractOnExpired[T any](generic func(any)) func(T) {
	if generic == nil {
		return nil
	}
	return func(value T) {
		generic(value)
	}
}
