package buffer

import (
	"slices"
	"sync"

	"github.com/zhaori96/crono/internal/util"
)

type synchronousBuffer[T any] struct {
	_ util.NoCopy

	items    []T
	capacity int

	head  int
	tail  int
	count int

	closed bool

	mutex    sync.Mutex
	notEmpty *sync.Cond
	notFull  *sync.Cond
}

func newSynchronousBuffer[T any](
	capacity int,
	startItems ...T,
) *synchronousBuffer[T] {
	switch {
	case capacity <= 0 && len(startItems) == 0:
		panic("capacity or len of startItems must be greather than zero")
	case capacity > 0 && capacity < len(startItems):
		panic("when capacity is greather than zero len of startItems must be less or equal to capacity ")
	}

	buffer := &synchronousBuffer[T]{
		items:    make([]T, capacity),
		capacity: capacity,
	}
	buffer.notEmpty = sync.NewCond(&buffer.mutex)
	buffer.notFull = sync.NewCond(&buffer.mutex)

	if capacity <= 0 {
		buffer.items = startItems
		return buffer
	}

	buffer.items = make([]T, capacity)
	copy(buffer.items, startItems)

	return buffer
}

func (b *synchronousBuffer[T]) Get() (T, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	var zero T
	for b.count == 0 {
		if b.closed {
			return zero, ErrBufferClosed
		}
		b.notEmpty.Wait()
	}

	b.tail = (b.tail + 1) % b.capacity
	item := b.items[b.tail]
	b.items[b.tail] = zero
	b.count--
	b.notFull.Signal()
	return item, nil
}

func (b *synchronousBuffer[T]) Put(item T) error {
	return b.enqueue(item)
}

func (b *synchronousBuffer[T]) Closed() bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.closed
}

func (b *synchronousBuffer[T]) Metrics() Metrics {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return Metrics{
		Capacity:  b.capacity,
		Occupancy: b.count,
	}
}

func (b *synchronousBuffer[T]) State() StateSnapshot {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return StateSnapshot{
		Head: uint32(b.head),
		Tail: uint32(b.tail),
	}
}

func (b *synchronousBuffer[T]) Close() {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if b.closed {
		return
	}

	b.closed = true

	var zero T
	for index := range b.items {
		b.items[index] = zero
	}

	b.items = slices.Clip(b.items)
	b.items = nil

	b.notEmpty.Broadcast()
	b.notFull.Broadcast()
}

func (b *synchronousBuffer[T]) enqueue(item T) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	for b.count == b.capacity {
		if b.closed {
			return ErrBufferClosed
		}
		b.notFull.Wait()
	}

	if b.closed {
		return ErrBufferClosed
	}

	b.head = (b.head + 1) % b.capacity
	b.items[b.head] = item
	b.count++
	b.notEmpty.Signal()
	return nil
}
