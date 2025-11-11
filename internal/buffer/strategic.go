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

	head      atomic.Uint32
	tail      atomic.Uint32
	occupancy atomic.Int32
	closed    atomic.Bool
	mutex     sync.Mutex
	working   sync.WaitGroup
}

func newStrategicBuffer[T any](
	capacity int,
	allowLock bool,
	startItems ...T,
) *strategicBuffer[T] {
	switch {
	case capacity <= 0 && len(startItems) == 0:
		panic("capacity or len of startItems must be greather than zero")
	case capacity > 0 && capacity < len(startItems):
		panic("when capacity is greather than zero len of startItems must be less or equal to capacity ")
	}

	buffer := &strategicBuffer[T]{
		capacity:  uint32(capacity),
		allowLock: allowLock,
	}

	if capacity <= 0 {
		buffer.items = startItems
		buffer.capacity = uint32(len(startItems))
		buffer.occupancy.Store(int32(len(buffer.items)))
		return buffer
	}

	buffer.items = make([]T, capacity)
	copy(buffer.items, startItems)
	buffer.occupancy.Store(int32(len(buffer.items)))

	return buffer
}

func (b *strategicBuffer[T]) Get() (T, error) {
	var zero T
	if b.closed.Load() && b.occupancy.Load() == 0 {
		return zero, ErrBufferClosed
	}

	b.working.Add(1)
	defer b.working.Done()

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
	err := b.enqueue(item)
	if errors.Is(err, ErrBufferFull) {
		return ErrReuseMismatch
	}
	return err
}

func (b *strategicBuffer[T]) Closed() bool {
	return b.closed.Load()
}

func (b *strategicBuffer[T]) Occupancy() int {
	return int(b.occupancy.Load())
}

func (b *strategicBuffer[T]) Capacity() int {
	return int(b.capacity)
}

func (b *strategicBuffer[T]) Metrics() Metrics {
	return Metrics{
		Capacity:  int(b.capacity),
		Occupancy: int(b.occupancy.Load()),
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

	b.working.Wait()
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

	b.working.Add(1)
	defer b.working.Done()

	if !b.reserveSlot() {
		if b.closed.Load() {
			return ErrBufferClosed
		}
		return ErrBufferFull
	}

	index, previous := b.advanceHead()
	if b.closed.Load() {
		b.rollbackReservation()
		b.head.CompareAndSwap(previous, index)
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

	for attempt := range maxAttempts {
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

	b.mutex.Lock()
	defer b.mutex.Unlock()

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

	for attempt := range maxAttempts {
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

	b.mutex.Lock()
	defer b.mutex.Unlock()

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

	for attempt := range maxAttempts {
		current := b.head.Load()
		next := (current + 1) % b.capacity
		if b.head.CompareAndSwap(current, next) {
			return next, current
		}
		if attempt+1 < maxAttempts {
			backoff.Pause()
		}
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()

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

	for attempt := range maxAttempts {
		current := b.tail.Load()
		next := (current + 1) % b.capacity
		if b.tail.CompareAndSwap(current, next) {
			return next
		}
		if attempt+1 < maxAttempts {
			backoff.Pause()
		}
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()

	current := b.tail.Load()
	next := (current + 1) % b.capacity
	b.tail.Store(next)
	return next
}
