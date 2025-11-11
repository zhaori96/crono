package buffer

import "errors"

var (
	ErrBufferEmpty   = errors.New("buffer: buffer is empty")
	ErrBufferFull    = errors.New("buffer: buffer is full")
	ErrBufferClosed  = errors.New("buffer: buffer is closed")
	ErrInvalidMode   = errors.New("buffer: access mode is invalid")
	ErrInvalidSize   = errors.New("buffer: capacity must be positive")
	ErrReuseMismatch = errors.New("buffer: buffer reuse violated capacity constraints")
)
