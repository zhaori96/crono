package wheel

import (
	"math"
	"sync/atomic"
)

type Sequence struct {
	start uint64
	value atomic.Uint64
}

func NewSequence(start int) *Sequence {
	sequence := &Sequence{start: uint64(start)}
	sequence.value.Store(sequence.start)
	return sequence
}

func (s *Sequence) Next() uint64 {
	if s.value.CompareAndSwap(math.MaxUint64, s.start) {
		return s.start
	}

	value := s.value.Add(1)
	return value
}

func (s *Sequence) Current() uint64 {
	return s.value.Load()
}

func (s *Sequence) Reset() uint64 {
	return s.value.Swap(s.start)
}
