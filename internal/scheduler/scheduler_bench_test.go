package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/zhaori96/crono/internal/wheel"
)

func BenchmarkSchedulerAcquireRelease(b *testing.B) {
	const (
		capacity       = 512
		idleTimeout    = 200 * time.Millisecond
		wheelTick      = 10 * time.Millisecond
		wheelSlotCount = 128
	)

	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(wheelTick),
		wheel.WithSlotCount(wheelSlotCount),
	)
	if err != nil {
		b.Fatalf("failed to create wheel: %v", err)
	}
	if startErr := timeWheel.Start(); startErr != nil {
		b.Fatalf("failed to start wheel: %v", startErr)
	}

	resources := make([]int, capacity)
	for i := 0; i < capacity; i++ {
		resources[i] = i
	}

	resourceScheduler, err := NewPreloadedScheduler[int](
		resources,
		WithWheel(timeWheel),
		WithIdleTimeout(idleTimeout),
	)
	if err != nil {
		b.Fatalf("failed to create scheduler: %v", err)
	}

	b.Cleanup(func() {
		resourceScheduler.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if stopErr := timeWheel.Stop(ctx); stopErr != nil {
			b.Fatalf("failed to stop wheel: %v", stopErr)
		}
	})

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, releaser, acquireErr := resourceScheduler.Acquire()
		if acquireErr != nil {
			b.Fatalf("acquire failed: %v", acquireErr)
		}
		if releaseErr := releaser.Release(); releaseErr != nil {
			b.Fatalf("release failed: %v", releaseErr)
		}
	}
}
