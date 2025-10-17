package buffer

import "errors"

var (
	ErrBufferEmpty   = errors.New("buffer is empty")
	ErrBufferFull    = errors.New("buffer is full")
	ErrBufferClosed  = errors.New("buffer is closed")
	ErrInvalidMode   = errors.New("access mode is invalid")
	ErrInvalidSize   = errors.New("buffer capacity must be positive")
	ErrReuseMismatch = errors.New("buffer reuse violated capacity constraints")
)
