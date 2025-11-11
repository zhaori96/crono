package buffer

type FixedBuffer[T any] interface {
	Get() (T, error)
	//GetWhen(func(T) bool) (T, bool, error)
	Put(T) error
	Closed() bool
	Close()
	Metrics() Metrics
	State() StateSnapshot
}

func NewFixedBuffer[T any](
	capacity int,
	mode AccessMode,
	startItems ...T,
) (FixedBuffer[T], error) {
	if capacity <= 0 && len(startItems) == 0 {
		return nil, ErrInvalidSize
	}

	switch mode {
	case AccessModeSynchronous:
		return newSynchronousBuffer(capacity, startItems...), nil
	case AccessModeAsynchronous:
		return newStrategicBuffer(capacity, false, startItems...), nil
	case AccessModeStrategic:
		return newStrategicBuffer(capacity, true, startItems...), nil
	default:
		return nil, ErrInvalidMode
	}
}
