package scheduler

import "time"

type Option func(*configuration)

type configuration struct {
	idleTimeout         time.Duration
	releaseErrorHandler func(error)
}

const defaultIdleTimeout = 30 * time.Second

func defaultConfiguration() configuration {
	return configuration{
		idleTimeout: defaultIdleTimeout,
	}
}

func WithIdleTimeout(timeout time.Duration) Option {
	return func(configurationData *configuration) {
		if timeout > 0 {
			configurationData.idleTimeout = timeout
		}
	}
}

func WithReleaseErrorHandler(handler func(error)) Option {
	return func(configurationData *configuration) {
		if handler != nil {
			configurationData.releaseErrorHandler = handler
		}
	}
}
