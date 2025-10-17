package buffer

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhaori96/crono/internal/util"
)

type strategicBuffer[T any] struct {
	_ util.NoCopy

	items     []T
	capacity  uint32
	allowLock bool

	head        atomic.Uint32
	tail        atomic.Uint32
	occupancy   atomic.Int32
	closed      atomic.Bool
	fallbackMux sync.Mutex
}

func newStrategicBuffer[T any](capacity int, allowLock bool) *strategicBuffer[T] {
	return &strategicBuffer[T]{
		items:     make([]T, capacity),
		capacity:  uint32(capacity),
		allowLock: allowLock,
	}
}

func (b *strategicBuffer[T]) Get() (T, error) {
	var zero T

	if b.closed.Load() && b.occupancy.Load() == 0 {
		return zero, ErrBufferClosed
	}

	if !b.acquireAvailable() {
		if b.closed.Load() {
			return zero, ErrBufferClosed
		}
		return zero, ErrBufferEmpty
	}

	index := b.advanceTail()
	item := b.items[index]
	b.items[index] = zero
	return item, nil
}

func (b *strategicBuffer[T]) Put(item T) error {
	return b.enqueue(item)
}

func (b *strategicBuffer[T]) Release(item T) error {
	err := b.enqueue(item)
	if errors.Is(err, ErrBufferFull) {
		return ErrReuseMismatch
	}
	return err
}

func (b *strategicBuffer[T]) Closed() bool {
	return b.closed.Load()
}

func (b *strategicBuffer[T]) Metrics() Metrics {
	occupancy := b.occupancy.Load()
	return Metrics{
		Capacity:  b.capacity,
		Occupancy: occupancy,
	}
}

func (b *strategicBuffer[T]) State() StateSnapshot {
	return StateSnapshot{
		Head: b.head.Load(),
		Tail: b.tail.Load(),
	}
}

func (b *strategicBuffer[T]) Close() {
	if !b.closed.CompareAndSwap(false, true) {
		return
	}

	const maxWaitAttempts = 64
	sleeper := util.NewExponentialSleeper(20*time.Microsecond, time.Millisecond)
	for attempt := 0; attempt < maxWaitAttempts; attempt++ {
		if b.occupancy.Load() == 0 {
			break
		}
		sleeper.Pause()
	}

	b.fallbackMux.Lock()
	defer b.fallbackMux.Unlock()

	b.occupancy.Store(0)

	var zero T
	for index := range b.items {
		b.items[index] = zero
	}
	b.items = slices.Clip(b.items)
	b.items = nil
}

func (b *strategicBuffer[T]) enqueue(item T) error {
	if b.closed.Load() {
		return ErrBufferClosed
	}

	if !b.reserveSlot() {
		if b.closed.Load() {
			return ErrBufferClosed
		}
		return ErrBufferFull
	}

	index, previous := b.advanceHead()
	if b.closed.Load() {
		b.rollbackReservation()
		b.restoreHead(previous, index)
		return ErrBufferClosed
	}

	b.items[index] = item
	return nil
}

func (b *strategicBuffer[T]) reserveSlot() bool {
	if !b.allowLock {
		for {
			current := b.occupancy.Load()
			if current == int32(b.capacity) {
				return false
			}
			if b.occupancy.CompareAndSwap(current, current+1) {
				return true
			}
		}
	}

	const maxAttempts = 10
	backoff := util.NewExponentialSleeper(10*time.Microsecond, 500*time.Microsecond)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		current := b.occupancy.Load()
		if current == int32(b.capacity) {
			return false
		}
		if b.occupancy.CompareAndSwap(current, current+1) {
			return true
		}
		if attempt+1 < maxAttempts {
			backoff.Pause()
		}
	}

	b.fallbackMux.Lock()
	defer b.fallbackMux.Unlock()

	current := b.occupancy.Load()
	if current == int32(b.capacity) {
		return false
	}
	b.occupancy.Add(1)
	return true
}

func (b *strategicBuffer[T]) acquireAvailable() bool {
	if !b.allowLock {
		for {
			current := b.occupancy.Load()
			if current == 0 {
				return false
			}
			if b.occupancy.CompareAndSwap(current, current-1) {
				return true
			}
		}
	}

	const maxAttempts = 10
	backoff := util.NewExponentialSleeper(10*time.Microsecond, 500*time.Microsecond)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		current := b.occupancy.Load()
		if current == 0 {
			return false
		}
		if b.occupancy.CompareAndSwap(current, current-1) {
			return true
		}
		if attempt+1 < maxAttempts {
			backoff.Pause()
		}
	}

	b.fallbackMux.Lock()
	defer b.fallbackMux.Unlock()

	current := b.occupancy.Load()
	if current == 0 {
		return false
	}
	b.occupancy.Add(-1)
	return true
}

func (b *strategicBuffer[T]) rollbackReservation() {
	b.occupancy.Add(-1)
}

func (b *strategicBuffer[T]) advanceHead() (uint32, uint32) {
	if !b.allowLock {
		for {
			current := b.head.Load()
			next := (current + 1) % b.capacity
			if b.head.CompareAndSwap(current, next) {
				return next, current
			}
		}
	}

	const maxAttempts = 10
	backoff := util.NewExponentialSleeper(10*time.Microsecond, 500*time.Microsecond)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		current := b.head.Load()
		next := (current + 1) % b.capacity
		if b.head.CompareAndSwap(current, next) {
			return next, current
		}
		if attempt+1 < maxAttempts {
			backoff.Pause()
		}
	}

	b.fallbackMux.Lock()
	defer b.fallbackMux.Unlock()

	current := b.head.Load()
	next := (current + 1) % b.capacity
	b.head.Store(next)
	return next, current
}

func (b *strategicBuffer[T]) advanceTail() uint32 {
	if !b.allowLock {
		for {
			current := b.tail.Load()
			next := (current + 1) % b.capacity
			if b.tail.CompareAndSwap(current, next) {
				return next
			}
		}
	}

	const maxAttempts = 10
	backoff := util.NewExponentialSleeper(10*time.Microsecond, 500*time.Microsecond)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		current := b.tail.Load()
		next := (current + 1) % b.capacity
		if b.tail.CompareAndSwap(current, next) {
			return next
		}
		if attempt+1 < maxAttempts {
			backoff.Pause()
		}
	}

	b.fallbackMux.Lock()
	defer b.fallbackMux.Unlock()

	current := b.tail.Load()
	next := (current + 1) % b.capacity
	b.tail.Store(next)
	return next
}

func (b *strategicBuffer[T]) restoreHead(previous uint32, current uint32) {
	if b.allowLock {
		b.fallbackMux.Lock()
		defer b.fallbackMux.Unlock()
		if b.head.Load() == current {
			b.head.Store(previous)
		}
		return
	}

	b.head.CompareAndSwap(current, previous)
}
