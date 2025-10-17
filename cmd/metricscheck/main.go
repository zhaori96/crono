package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/zhaori96/crono/internal/buffer"
	"github.com/zhaori96/crono/internal/scheduler"
	"github.com/zhaori96/crono/internal/wheel"
)

func main() {
	strategicBuffer, err := buffer.NewFixedBuffer[string](4, buffer.AccessModeStrategic)
	if err != nil {
		log.Fatalf("failed to create buffer: %v", err)
	}

	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(50*time.Millisecond),
		wheel.WithSlotCount(64),
	)
	if err != nil {
		log.Fatalf("failed to create wheel: %v", err)
	}

	if err := timeWheel.Start(); err != nil {
		log.Fatalf("failed to start wheel: %v", err)
	}
	defer stopWheel(timeWheel)

	resourceScheduler, err := scheduler.NewScheduler[string](
		strategicBuffer,
		timeWheel,
		scheduler.WithIdleTimeout(120*time.Millisecond),
	)
	if err != nil {
		log.Fatalf("failed to create scheduler: %v", err)
	}
	defer resourceScheduler.Close()

	preloadValues(resourceScheduler, []string{"alpha", "bravo", "charlie"})

	leaseA, err := resourceScheduler.Acquire()
	if err != nil {
		log.Fatalf("failed to acquire first lease: %v", err)
	}

	leaseB, err := resourceScheduler.Acquire()
	if err != nil {
		log.Fatalf("failed to acquire second lease: %v", err)
	}

	valueA, err := leaseA.Value()
	if err != nil {
		log.Fatalf("failed to read value from first lease: %v", err)
	}
	fmt.Printf("Lease A received value: %q\n", valueA)

	_, err = leaseB.Value()
	if err != nil {
		log.Fatalf("failed to read value from second lease: %v", err)
	}

	if err := leaseA.KeepAlive(); err != nil {
		log.Fatalf("failed to keep lease A alive: %v", err)
	}

	if err := leaseB.ResetTimeout(200 * time.Millisecond); err != nil {
		log.Fatalf("failed to reset lease B timeout: %v", err)
	}

	if err := leaseA.Release(); err != nil {
		log.Fatalf("failed to release lease A: %v", err)
	}

	time.Sleep(250 * time.Millisecond)

	schedulerMetrics := resourceScheduler.Metrics()
	printMetrics(schedulerMetrics)

	if err := leaseB.Release(); err != nil {
		fmt.Printf("Lease B release after expiration returned: %v\n", err)
	}
}

func preloadValues(resourceScheduler *scheduler.Scheduler[string], values []string) {
	for _, value := range values {
		if err := resourceScheduler.Put(value); err != nil {
			log.Fatalf("failed to preload value %q: %v", value, err)
		}
	}
}

func printMetrics(metrics scheduler.Metrics) {
	fmt.Println("Scheduler metrics snapshot:")
	fmt.Printf("  Idle timeout: %s\n", metrics.IdleTimeout)
	fmt.Printf("  Active leases: %d\n", metrics.ActiveLeases)
	fmt.Printf("  Acquired count: %d\n", metrics.AcquiredCount)
	fmt.Printf("  Released count: %d\n", metrics.ReleasedCount)
	fmt.Printf("  Expired count: %d\n", metrics.ExpiredCount)
	fmt.Printf("  Timeout resets: %d\n", metrics.TimeoutResetCount)
	fmt.Printf("  Keep alives: %d\n", metrics.KeepAliveCount)
	fmt.Println("  Buffer metrics:")
	fmt.Printf("    Capacity: %d\n", metrics.BufferMetrics.Capacity)
	fmt.Printf("    Occupancy: %d\n", metrics.BufferMetrics.Occupancy)
	fmt.Println("  Wheel metrics:")
	fmt.Printf("    Scheduled: %d\n", metrics.WheelMetrics.ScheduledCount)
	fmt.Printf("    Expired: %d\n", metrics.WheelMetrics.ExpiredCount)
	fmt.Printf("    Cancelled: %d\n", metrics.WheelMetrics.CancelledCount)
	fmt.Printf("    Rescheduled: %d\n", metrics.WheelMetrics.RescheduledCount)
}

func stopWheel(timeWheel *wheel.Wheel) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := timeWheel.Stop(ctx); err != nil {
		log.Printf("wheel stop returned error: %v", err)
	}
}
