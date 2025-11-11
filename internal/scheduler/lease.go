package scheduler

import (
	"sync/atomic"
	"time"

	"github.com/zhaori96/crono/internal/wheel"
)

type Releaser interface {
	Release() error
}

type leaseState uint32

const (
	leaseStateIdle leaseState = iota
	leaseStateActive
	leaseStateExpired
)

var leaseSequence = NewSequence(0)

type Lease[T any] struct {
	id          int
	value       T
	handler     wheel.Handler
	state       atomic.Value
	scheduler   *Scheduler[T]
	idleTimeout time.Duration
	initialized atomic.Bool
}

func NewLease[T any](value T, initialized bool) *Lease[T] {
	lease := &Lease[T]{id: leaseSequence.Next(), value: value}
	lease.state.Store(leaseStateIdle)
	lease.initialized.Store(initialized)
	return lease
}

func NewLeasesFrom[T any](values []T, capacity int) []*Lease[T] {
	capacity = max(len(values), capacity)
	leases := make([]*Lease[T], capacity)
	totalValues := len(values)
	var zero T
	for index := range capacity {
		if index < totalValues {
			leases[index] = NewLease(values[index], true)
		} else {
			leases[index] = NewLease(zero, false)
		}
	}
	return leases
}

func (l *Lease[T]) Expire() bool {
	if !l.state.CompareAndSwap(leaseStateIdle, leaseStateExpired) {
		return false
	}
	if l.scheduler != nil && l.scheduler.onExpired != nil {
		l.scheduler.onExpired(l.value)
	}
	return true
}

func (l *Lease[T]) Expired() bool {
	return l.state.Load() == leaseStateExpired
}

func (l *Lease[T]) Release() error {
	if l.scheduler == nil {
		return ErrInvalidState
	}
	return l.scheduler.release(l, l.idleTimeout)
}

func (l *Lease[T]) setInitialized() {
	l.initialized.Store(true)
}

func (l *Lease[T]) setIdle() bool {
	return l.state.CompareAndSwap(leaseStateActive, leaseStateIdle)
}

func (l *Lease[T]) setActive() bool {
	return l.state.CompareAndSwap(leaseStateIdle, leaseStateActive)
}

func (l *Lease[T]) isActive() bool {
	return l.state.Load() == leaseStateActive
}

func (l *Lease[T]) isInitialized() bool {
	return l.initialized.Load()
}

func (l *Lease[T]) setHandler(handler wheel.Handler) {
	l.handler = handler
}

func (l *Lease[T]) reset() {
	l.handler = nil
	l.scheduler = nil
	l.idleTimeout = 0
	l.initialized.Store(false)
	l.state.Store(leaseStateIdle)
}
