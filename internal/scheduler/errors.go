package scheduler

import "errors"

var (
	ErrNilBuffer          = errors.New("scheduler: buffer cannot be nil")
	ErrNilWheel           = errors.New("scheduler: wheel cannot be nil")
	ErrInvalidIdleTimeout = errors.New("scheduler: idle timeout must be greater than zero")
	ErrSchedulerClosed    = errors.New("scheduler: scheduler is closed")
	ErrLeaseInactive      = errors.New("scheduler: lease is inactive")
	ErrLeaseExpired       = errors.New("scheduler: lease has already expired")
	ErrNonPositiveTimeout = errors.New("scheduler: timeout must be greater than zero")
	ErrWheelNotRunning    = errors.New("scheduler: wheel is not running")
	ErrHandleUnavailable  = errors.New("scheduler: handle is not available")
)
