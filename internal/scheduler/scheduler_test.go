package scheduler

import (
	"testing"
	"time"

	"github.com/zhaori96/crono/internal/wheel"
)

func TestSchedulerBasicAcquireRelease(t *testing.T) {
	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(10*time.Millisecond),
		wheel.WithSlotCount(64),
	)
	if err != nil {
		t.Fatalf("failed to create wheel: %v", err)
	}

	if err := timeWheel.Start(); err != nil {
		t.Fatalf("failed to start wheel: %v", err)
	}
	defer timeWheel.Stop(nil)

	resources := []int{1, 2, 3, 4, 5}
	scheduler, err := NewPreloadedScheduler(
		resources,
		WithWheel(timeWheel),
		WithIdleTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// First acquire
	value, releaser, err := scheduler.Acquire()
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	t.Logf("Acquired value: %d", value)

	// Release
	if err := releaser.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	t.Log("Released successfully")

	// Second acquire (should reuse the same lease)
	value2, releaser2, err := scheduler.Acquire()
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	t.Logf("Acquired value again: %d", value2)

	// Release again
	if err := releaser2.Release(); err != nil {
		t.Fatalf("second release failed: %v", err)
	}
	t.Log("Second release successful")
}

func TestSchedulerLazyCreation(t *testing.T) {
	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(10*time.Millisecond),
		wheel.WithSlotCount(64),
	)
	if err != nil {
		t.Fatalf("failed to create wheel: %v", err)
	}

	if err := timeWheel.Start(); err != nil {
		t.Fatalf("failed to start wheel: %v", err)
	}
	defer timeWheel.Stop(nil)

	counter := 0
	constructor := func() (int, error) {
		counter++
		return counter * 100, nil
	}

	scheduler, err := NewScheduler(
		constructor,
		5,
		WithWheel(timeWheel),
		WithIdleTimeout(100*time.Millisecond),
		WithRecreationPolicy(RecreateAlways),
	)
	if err != nil {
		t.Fatalf("failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// Acquire should trigger lazy creation
	value, releaser, err := scheduler.Acquire()
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	if value != 100 {
		t.Errorf("expected value 100, got %d", value)
	}

	if counter != 1 {
		t.Errorf("expected constructor called once, got %d times", counter)
	}

	releaser.Release()
	t.Log("Lazy creation test passed")
}

func TestSchedulerCircuitBreaker(t *testing.T) {
	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(10*time.Millisecond),
		wheel.WithSlotCount(64),
	)
	if err != nil {
		t.Fatalf("failed to create wheel: %v", err)
	}

	if err := timeWheel.Start(); err != nil {
		t.Fatalf("failed to start wheel: %v", err)
	}
	defer timeWheel.Stop(nil)

	failCount := 0
	constructor := func() (int, error) {
		failCount++
		return 0, ErrSchedulerClosed // Simulate error
	}

	scheduler, err := NewScheduler(
		constructor,
		5,
		WithWheel(timeWheel),
		WithIdleTimeout(100*time.Millisecond),
		WithRecreationPolicy(RecreateAlways),
		WithCircuitBreaker(&CircuitBreaker{
			FailureThreshold: 3,
			OpenDuration:     1 * time.Second,
		}),
	)
	if err != nil {
		t.Fatalf("failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// Should fail and eventually open circuit breaker
	_, _, err = scheduler.Acquire()
	if err == nil {
		t.Error("expected acquire to fail")
	}

	if failCount < 3 {
		t.Errorf("expected at least 3 failures to open circuit, got %d", failCount)
	}

	t.Log("Circuit breaker test passed")
}

func TestSchedulerRecreateAlways(t *testing.T) {
	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(10*time.Millisecond),
		wheel.WithSlotCount(64),
	)
	if err != nil {
		t.Fatalf("failed to create wheel: %v", err)
	}

	if err := timeWheel.Start(); err != nil {
		t.Fatalf("failed to start wheel: %v", err)
	}
	defer timeWheel.Stop(nil)

	counter := 0
	constructor := func() (int, error) {
		counter++
		return counter, nil
	}

	scheduler, err := NewScheduler(
		constructor,
		2,
		WithWheel(timeWheel),
		WithIdleTimeout(50*time.Millisecond),
		WithRecreationPolicy(RecreateAlways),
	)
	if err != nil {
		t.Fatalf("failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// First acquire
	value1, releaser1, err := scheduler.Acquire()
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	releaser1.Release()

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Second acquire should recreate
	value2, releaser2, err := scheduler.Acquire()
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	releaser2.Release()

	if value1 == value2 {
		t.Error("expected different values after recreation")
	}

	t.Logf("RecreateAlways test passed: value1=%d, value2=%d", value1, value2)
}

func TestSchedulerWithVariant(t *testing.T) {
	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(10*time.Millisecond),
		wheel.WithSlotCount(64),
	)
	if err != nil {
		t.Fatalf("failed to create wheel: %v", err)
	}

	if err := timeWheel.Start(); err != nil {
		t.Fatalf("failed to start wheel: %v", err)
	}
	defer timeWheel.Stop(nil)

	config := Configuration{
		IdleTimeout:      100 * time.Millisecond,
		Wheel:            timeWheel,
		RecreationPolicy: RecreateAlways,
	}

	constructor := func() (string, error) {
		return "test", nil
	}

	scheduler, err := NewSchedulerWith(constructor, 5, config)
	if err != nil {
		t.Fatalf("failed to create scheduler with config: %v", err)
	}
	defer scheduler.Close()

	value, releaser, err := scheduler.Acquire()
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	if value != "test" {
		t.Errorf("expected 'test', got '%s'", value)
	}

	releaser.Release()
	t.Log("With variant test passed")
}

func TestPreloadedSchedulerWithVariant(t *testing.T) {
	timeWheel, err := wheel.NewWheel(
		wheel.WithTickInterval(10*time.Millisecond),
		wheel.WithSlotCount(64),
	)
	if err != nil {
		t.Fatalf("failed to create wheel: %v", err)
	}

	if err := timeWheel.Start(); err != nil {
		t.Fatalf("failed to start wheel: %v", err)
	}
	defer timeWheel.Stop(nil)

	config := Configuration{
		IdleTimeout: 100 * time.Millisecond,
		Wheel:       timeWheel,
	}

	resources := []string{"alpha", "beta", "gamma"}
	scheduler, err := NewPreloadedSchedulerWith(resources, config)
	if err != nil {
		t.Fatalf("failed to create preloaded scheduler with config: %v", err)
	}
	defer scheduler.Close()

	value, releaser, err := scheduler.Acquire()
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	found := false
	for _, r := range resources {
		if value == r {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("acquired value '%s' not in original resources", value)
	}

	releaser.Release()
	t.Log("PreloadedWith variant test passed")
}
