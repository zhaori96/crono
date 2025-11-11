package scheduler

import (
	"sync/atomic"
	"time"
)

// CircuitBreakerState represents the current state of the circuit breaker.
type CircuitBreakerState int

const (
	// CircuitBreakerClosed means the circuit is functioning normally.
	CircuitBreakerClosed CircuitBreakerState = iota

	// CircuitBreakerOpen means the circuit is open due to too many failures.
	// Constructor calls are blocked until the circuit transitions to half-open.
	CircuitBreakerOpen

	// CircuitBreakerHalfOpen means the circuit is testing if the system has recovered.
	// A single success closes the circuit; a single failure reopens it.
	CircuitBreakerHalfOpen
)

// CircuitBreaker configuration for protecting against repeated constructor failures.
type CircuitBreaker struct {
	// Enabled determines if the circuit breaker is active.
	Enabled bool

	// FailureThreshold is the number of consecutive constructor failures
	// required to open the circuit.
	FailureThreshold uint32

	// OpenDuration is how long the circuit stays open before transitioning
	// to half-open state to test recovery.
	OpenDuration time.Duration

	// OnOpen is called when the circuit transitions to open state.
	OnOpen func()

	// OnClose is called when the circuit transitions to closed state.
	OnClose func()

	// OnHalfOpen is called when the circuit transitions to half-open state.
	OnHalfOpen func()

	// NotifyChannel receives state change notifications.
	// The channel is user-provided and should be buffered to avoid blocking.
	NotifyChannel chan<- CircuitBreakerState
}

func NewDefaultCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		Enabled:          true,
		FailureThreshold: 3,
		OpenDuration:     time.Minute,
	}
}

type circuitBreaker struct {
	configuration    *CircuitBreaker
	state            atomic.Uint32
	consecutiveFails atomic.Uint32
	lastStateChange  atomic.Int64
}

func newCircuitBreaker(configuration *CircuitBreaker) *circuitBreaker {
	if configuration == nil {
		return nil
	}

	newCircuitBreaker := &circuitBreaker{
		configuration: configuration,
	}

	newCircuitBreaker.state.Store(uint32(CircuitBreakerClosed))
	newCircuitBreaker.lastStateChange.Store(time.Now().UnixNano())

	return newCircuitBreaker
}

func (cb *circuitBreaker) IsOpen() bool {
	if cb == nil || !cb.configuration.Enabled {
		return false
	}

	currentState := CircuitBreakerState(cb.state.Load())

	// Check if should transition from open to half-open
	if currentState == CircuitBreakerOpen {
		lastChange := time.Unix(0, cb.lastStateChange.Load())
		if time.Since(lastChange) >= cb.configuration.OpenDuration {
			cb.transitionTo(CircuitBreakerHalfOpen)
			return false
		}
		return true
	}

	return false
}

func (cb *circuitBreaker) RecordFailure() {
	if cb == nil || !cb.configuration.Enabled {
		return
	}

	fails := cb.consecutiveFails.Add(1)
	currentState := CircuitBreakerState(cb.state.Load())

	switch currentState {
	case CircuitBreakerHalfOpen:
		// In half-open, single failure reopens circuit
		cb.transitionTo(CircuitBreakerOpen)
	case CircuitBreakerClosed:
		// Check if should open
		if fails >= cb.configuration.FailureThreshold {
			cb.transitionTo(CircuitBreakerOpen)
		}
	}
}

func (cb *circuitBreaker) RecordSuccess() {
	if cb == nil || !cb.configuration.Enabled {
		return
	}

	cb.consecutiveFails.Store(0)
	currentState := CircuitBreakerState(cb.state.Load())

	if currentState == CircuitBreakerHalfOpen {
		// Success in half-open closes circuit
		cb.transitionTo(CircuitBreakerClosed)
	}
}

func (cb *circuitBreaker) transitionTo(newState CircuitBreakerState) {
	oldState := CircuitBreakerState(cb.state.Swap(uint32(newState)))

	if oldState == newState {
		return
	}

	cb.lastStateChange.Store(time.Now().UnixNano())

	if newState == CircuitBreakerOpen {
		cb.consecutiveFails.Store(0)
	}

	// Call callbacks
	switch newState {
	case CircuitBreakerOpen:
		if cb.configuration.OnOpen != nil {
			cb.configuration.OnOpen()
		}
	case CircuitBreakerClosed:
		if cb.configuration.OnClose != nil {
			cb.configuration.OnClose()
		}
	case CircuitBreakerHalfOpen:
		if cb.configuration.OnHalfOpen != nil {
			cb.configuration.OnHalfOpen()
		}
	}

	// Notify channel (non-blocking)
	if cb.configuration.NotifyChannel != nil {
		select {
		case cb.configuration.NotifyChannel <- newState:
		default:
		}
	}
}
