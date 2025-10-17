package buffer

type FixedBuffer[T any] interface {
	Get() (T, error)
	Put(T) error
	Release(T) error
	Closed() bool
	Close()
	Metrics() Metrics
	State() StateSnapshot
}

func NewFixedBuffer[T any](capacity int, mode AccessMode) (FixedBuffer[T], error) {
	if capacity <= 0 {
		return nil, ErrInvalidSize
	}

	switch mode {
	case AccessModeSynchronous:
		return newSynchronousBuffer[T](capacity), nil
	case AccessModeAsynchronous:
		return newStrategicBuffer[T](capacity, false), nil
	case AccessModeStrategic:
		return newStrategicBuffer[T](capacity, true), nil
	default:
		return nil, ErrInvalidMode
	}
}
