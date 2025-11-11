package scheduler

import "sync/atomic"

type Sequence struct {
	sequence atomic.Int64
}

func NewSequence(start int) *Sequence {
	sequence := &Sequence{}
	sequence.sequence.Store(int64(start))
	return sequence
}

func (s *Sequence) Next() int {
	value := s.sequence.Add(1)
	return int(value)
}

func (s *Sequence) Current() int {
	return int(s.sequence.Load())
}

func (s *Sequence) Reset() int {
	return int(s.sequence.Swap(0))
}
