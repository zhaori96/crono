package scheduler

import (
	"time"

	"github.com/zhaori96/crono/internal/buffer"
)

type Metrics struct {
	IdleTimeout       time.Duration
	ActiveLeases      uint64
	AcquiredCount     uint64
	ReleasedCount     uint64
	ExpiredCount      uint64
	TimeoutResetCount uint64
	KeepAliveCount    uint64
	BufferMetrics     buffer.Metrics
	WheelMetrics      WheelMetrics
}

type WheelMetrics struct {
	ScheduledCount   uint64
	ExpiredCount     uint64
	CancelledCount   uint64
	RescheduledCount uint64
}

func (s *Scheduler[T]) Metrics() Metrics {
	bufferMetrics := s.resourceBuffer.Metrics()
	wheelMetrics := WheelMetrics{
		ScheduledCount:   s.timeWheel.ScheduledCount(),
		ExpiredCount:     s.timeWheel.ExpiredCount(),
		CancelledCount:   s.timeWheel.CancelledCount(),
		RescheduledCount: s.timeWheel.RescheduledCount(),
	}

	return Metrics{
		IdleTimeout:       s.idleTimeout,
		ActiveLeases:      s.activeLeases.Load(),
		AcquiredCount:     s.acquiredCount.Load(),
		ReleasedCount:     s.releasedCount.Load(),
		ExpiredCount:      s.expiredCount.Load(),
		TimeoutResetCount: s.resetCount.Load(),
		KeepAliveCount:    s.keepAliveCount.Load(),
		BufferMetrics:     bufferMetrics,
		WheelMetrics:      wheelMetrics,
	}
}
