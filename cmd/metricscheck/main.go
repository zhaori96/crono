package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/zhaori96/crono/internal/scheduler"
	"github.com/zhaori96/crono/internal/wheel"
)

func main() {
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

	resources := []string{"alpha", "bravo", "charlie", "delta"}
	resourceScheduler, err := scheduler.NewPreloadedScheduler(
		resources,
		scheduler.WithWheel(timeWheel),
		scheduler.WithIdleTimeout(120*time.Millisecond),
	)
	if err != nil {
		log.Fatalf("failed to create scheduler: %v", err)
	}
	defer resourceScheduler.Close()

	valueA, releaserA, err := resourceScheduler.Acquire()
	if err != nil {
		log.Fatalf("failed to acquire first lease: %v", err)
	}
	fmt.Printf("Acquired value A: %q\n", valueA)

	valueB, releaserB, err := resourceScheduler.Acquire()
	if err != nil {
		log.Fatalf("failed to acquire second lease: %v", err)
	}
	fmt.Printf("Acquired value B: %q\n", valueB)

	if err := releaserA.Release(); err != nil {
		log.Fatalf("failed to release A: %v", err)
	}
	fmt.Println("Released value A")

	time.Sleep(250 * time.Millisecond)

	schedulerMetrics := resourceScheduler.Metrics()
	printMetrics(schedulerMetrics)

	if err := releaserB.Release(); err != nil {
		fmt.Printf("Release B after delay returned: %v\n", err)
	} else {
		fmt.Println("Released value B")
	}
}

func printMetrics(metrics scheduler.Metrics) {
	fmt.Println("\nScheduler metrics snapshot:")
	fmt.Printf("  Idle timeout: %s\n", metrics.IdleTimeout)
	fmt.Printf("  Acquired count: %d\n", metrics.AcquiredCount)
	fmt.Printf("  Released count: %d\n", metrics.ReleasedCount)
	fmt.Printf("  Expired count: %d\n", metrics.ExpiredCount)
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
