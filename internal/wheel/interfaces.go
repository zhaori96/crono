package wheel

import "time"

type Expirable interface {
	Expire()
	Expired() bool
}

type Handle interface {
	Cancel() bool
	Reset(time.Duration) error
	KeepAlive() error
}
