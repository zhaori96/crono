package util

import (
	"sync/atomic"
)

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
	~float32 | ~float64
}

type Atomic[T Number] struct {
	_     NoCopy
	value atomic.Value
}

func (v *Atomic[T]) CompareAndSwap(old T, new T) bool {
	
	return v.value.CompareAndSwap(old, new)
}
func (v *Atomic[T]) Load() T {
	loaded := v.value.Load()
	if loaded == nil {
		var zero T
		return zero
	}
	return loaded.(T)
}

func (v *Atomic[T]) Store(value T) {
	v.value.Store(value)
}

func (v *Atomic[T]) Swap(new T) T {
	swapped := v.value.Swap(new)
	if swapped == nil {
		var zero T
		return zero
	}
	return swapped.(T)
}
