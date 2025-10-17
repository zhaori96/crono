package scheduler

import (
	"sync/atomic"
	"time"

	"github.com/zhaori96/crono/internal/wheel"
)

type Lease[T any] struct {
	binding leaseBinding[T]
	handle  wheel.Handle
}

type leaseState uint32

const (
	leaseStatePending leaseState = iota
	leaseStateActive
	leaseStateReleased
	leaseStateExpired
)

type leaseBinding[T any] struct {
	scheduler    *Scheduler[T]
	value        T
	state        atomic.Uint32
	timeoutNanos atomic.Int64
}

func (b *leaseBinding[T]) initialize(
	scheduler *Scheduler[T],
	value T,
	timeout time.Duration,
) {
	b.scheduler = scheduler
	b.value = value
	b.state.Store(uint32(leaseStatePending))
	b.timeoutNanos.Store(timeout.Nanoseconds())
}

func (b *leaseBinding[T]) activate() {
	b.state.Store(uint32(leaseStateActive))
	if b.scheduler != nil {
		b.scheduler.onLeaseActivated()
	}
}

func (b *leaseBinding[T]) currentTimeout() time.Duration {
	return time.Duration(b.timeoutNanos.Load())
}

func (b *leaseBinding[T]) updateTimeout(timeout time.Duration) {
	b.timeoutNanos.Store(timeout.Nanoseconds())
}

func (b *leaseBinding[T]) tryRelease() bool {
	return b.state.CompareAndSwap(
		uint32(leaseStateActive),
		uint32(leaseStateReleased),
	)
}

func (b *leaseBinding[T]) tryExpire() bool {
	return b.state.CompareAndSwap(
		uint32(leaseStateActive),
		uint32(leaseStateExpired),
	)
}

func (b *leaseBinding[T]) stateValue() leaseState {
	return leaseState(b.state.Load())
}

func (b *leaseBinding[T]) consumeValue() T {
	value := b.value
	var zeroValue T
	b.value = zeroValue
	return value
}

func (b *leaseBinding[T]) finalizeRelease(manual bool) error {
	if b.scheduler == nil {
		return nil
	}

	value := b.consumeValue()
	releaseError := b.scheduler.releaseValue(value)
	if manual {
		b.scheduler.onLeaseReleased()
	} else {
		b.scheduler.onLeaseExpired()
	}
	b.scheduler = nil
	return releaseError
}

func (b *leaseBinding[T]) Expire() {
	if b == nil {
		return
	}
	if !b.tryExpire() {
		return
	}
	_ = b.finalizeRelease(false)
}

func (b *leaseBinding[T]) Expired() bool {
	if b == nil {
		return true
	}
	return b.stateValue() == leaseStateExpired
}

func (l *Lease[T]) Value() (T, error) {
	if l == nil {
		var zeroValue T
		return zeroValue, ErrLeaseInactive
	}
	if l.binding.stateValue() != leaseStateActive {
		var zeroValue T
		return zeroValue, ErrLeaseInactive
	}
	return l.binding.value, nil
}

func (l *Lease[T]) Release() error {
	if l == nil {
		return ErrLeaseInactive
	}
	binding := &l.binding
	if binding.scheduler == nil {
		return ErrLeaseInactive
	}

	if !binding.tryRelease() {
		if binding.stateValue() == leaseStateExpired {
			return ErrLeaseExpired
		}
		return ErrLeaseInactive
	}

	if l.handle != nil {
		l.handle.Cancel()
		l.handle = nil
	}

	return binding.finalizeRelease(true)
}

func (l *Lease[T]) ResetTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return ErrNonPositiveTimeout
	}
	if l == nil {
		return ErrLeaseInactive
	}
	binding := &l.binding
	if binding.stateValue() != leaseStateActive {
		return ErrLeaseInactive
	}
	if l.handle == nil {
		return ErrHandleUnavailable
	}

	if resetError := l.handle.Reset(timeout); resetError != nil {
		return resetError
	}
	binding.updateTimeout(timeout)
	if binding.scheduler != nil {
		binding.scheduler.onLeaseTimeoutReset()
	}
	return nil
}

func (l *Lease[T]) KeepAlive() error {
	if l == nil {
		return ErrLeaseInactive
	}
	binding := &l.binding
	if binding.stateValue() != leaseStateActive {
		return ErrLeaseInactive
	}
	if l.handle == nil {
		return ErrHandleUnavailable
	}
	if keepAliveError := l.handle.KeepAlive(); keepAliveError != nil {
		return keepAliveError
	}
	if binding.scheduler != nil {
		binding.scheduler.onLeaseKeepAlive()
	}
	return nil
}

func (l *Lease[T]) Timeout() (time.Duration, error) {
	if l == nil {
		return 0, ErrLeaseInactive
	}
	if l.binding.stateValue() != leaseStateActive {
		return 0, ErrLeaseInactive
	}
	return l.binding.currentTimeout(), nil
}
