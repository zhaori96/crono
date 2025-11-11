package wheel

import "errors"

var (
	ErrInvalidTickInterval = errors.New("wheel: tick interval must be greater than zero")
	ErrInvalidSlotCount    = errors.New("wheel: slot count must be greater than zero")
	ErrInvalidMaxRounds    = errors.New("wheel: max rounds must be greater than zero")
	ErrWheelAlreadyRunning = errors.New("wheel: wheel is already running")
	ErrWheelNotRunning     = errors.New("wheel: wheel is not running")
	ErrNilExpirable        = errors.New("wheel: expirable target cannot be nil")
	ErrNegativeTimeout     = errors.New("wheel: timeout cannot be negative")
	ErrTimeoutOverflow     = errors.New("wheel: timeout exceeds maximum supported rounds")
	ErrInactiveHandle      = errors.New("wheel: handle is not active")
	ErrEntryNotScheduled   = errors.New("wheel: entry is not scheduled")
	ErrEntryExpired        = errors.New("wheel: entry has already expired")
)
