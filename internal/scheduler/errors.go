package scheduler

import "errors"

var (
	ErrNilBuffer             = errors.New("scheduler: buffer cannot be nil")
	ErrNilWheel              = errors.New("scheduler: wheel cannot be nil")
	ErrNilConstructor        = errors.New("scheduler: constructor cannot be nil")
	ErrZeroCapacity          = errors.New("scheduler: capacity must be greater than zero")
	ErrInvalidIdleTimeout    = errors.New("scheduler: idle timeout must be greater than zero")
	ErrSchedulerClosed       = errors.New("scheduler: scheduler is closed")
	ErrLeaseInactive         = errors.New("scheduler: lease is inactive")
	ErrLeaseExpired          = errors.New("scheduler: lease has already expired")
	ErrNonPositiveTimeout    = errors.New("scheduler: timeout must be greater than zero")
	ErrWheelNotRunning       = errors.New("scheduler: wheel is not running")
	ErrHandleUnavailable     = errors.New("scheduler: handle is not available")
	ErrInvalidState          = errors.New("scheduler: invalid lease state transition")
	ErrCircuitBreakerOpen    = errors.New("scheduler: circuit breaker is open")
	ErrMaxRetriesExceeded    = errors.New("scheduler: max constructor retries exceeded")
	ErrNoAvailableResources  = errors.New("scheduler: no available resources")
)
