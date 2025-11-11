package scheduler

import (
	"time"

	"github.com/zhaori96/crono/internal/wheel"
)

const (
	defaultIdleTimeout           = 30 * time.Second
	defaultRecreationPolicy      = RecreateAlways
	defaultMaxConstructorRetries = 10
)

type Option func(*Configuration)

// Configuration holds all settings for creating a Scheduler.
// This type allows building complex configurations declaratively,
// which can then be passed to NewSchedulerWith or NewPreloadedSchedulerWith.
type Configuration struct {
	IdleTimeout time.Duration

	Wheel *wheel.Wheel

	RecreationPolicy RecreationPolicy
	InitialResources []any

	Destructor func(any)
	OnExpired  func(any)

	ConstructorErrorHandler func(error)
	ReleaseErrorHandler     func(error)
	MaxConstructorRetries   uint32

	CircuitBreaker *CircuitBreaker
	Backoff        *Backoff

	ownsWheel bool
}

func (c *Configuration) Resolve() error {
	if c.IdleTimeout <= 0 {
		return ErrInvalidIdleTimeout
	}

	if c.CircuitBreaker == nil {
		c.CircuitBreaker = NewDefaultCircuitBreaker()
	}

	if c.Backoff == nil {
		c.Backoff = NewDefaultBackoff()
	}

	if err := resolveWheel(c); err != nil {
		return err
	}

	return nil
}

func defaultConfiguration() Configuration {
	return Configuration{
		IdleTimeout:           defaultIdleTimeout,
		RecreationPolicy:      defaultRecreationPolicy,
		MaxConstructorRetries: defaultMaxConstructorRetries,
	}
}

func WithIdleTimeout(timeout time.Duration) Option {
	return func(config *Configuration) {
		if timeout > 0 {
			config.IdleTimeout = timeout
		}
	}
}

func WithReleaseErrorHandler(handler func(error)) Option {
	return func(configuration *Configuration) {
		if handler != nil {
			configuration.ReleaseErrorHandler = handler
		}
	}
}

func WithOnExpired[T any](callback func(T)) Option {
	return func(configuration *Configuration) {
		if callback != nil {
			configuration.OnExpired = func(v any) {
				callback(v.(T))
			}
		}
	}
}

func WithWheel(timeWheel *wheel.Wheel) Option {
	return func(configuration *Configuration) {
		configuration.Wheel = timeWheel
	}
}

func WithRecreationPolicy(policy RecreationPolicy) Option {
	return func(configuration *Configuration) {
		configuration.RecreationPolicy = policy
	}
}

func WithInitialResources[T any](resources []T) Option {
	return func(configuration *Configuration) {
		if len(resources) > 0 {
			configuration.InitialResources = make([]any, len(resources))
			for index, resource := range resources {
				configuration.InitialResources[index] = resource
			}
		}
	}
}

func WithDestructor[T any](destructor func(T)) Option {
	return func(configuration *Configuration) {
		if destructor != nil {
			configuration.Destructor = func(v any) {
				if typedValue, ok := v.(T); ok {
					destructor(typedValue)
				}
			}
		}
	}
}

func WithConstructorErrorHandler(handler func(error)) Option {
	return func(configuration *Configuration) {
		if handler != nil {
			configuration.ConstructorErrorHandler = handler
		}
	}
}

func WithMaxConstructorRetries(maxRetries uint32) Option {
	return func(configuration *Configuration) {
		if maxRetries > 0 {
			configuration.MaxConstructorRetries = maxRetries
		}
	}
}

func WithCircuitBreaker(circuitBreaker *CircuitBreaker) Option {
	return func(configuration *Configuration) {
		configuration.CircuitBreaker = circuitBreaker
	}
}

func WithBackoff(backoff *Backoff) Option {
	return func(configuration *Configuration) {
		configuration.Backoff = backoff
	}
}

// // configurationToOptions converts a public Configuration struct into functional options.
// // This allows the With constructor variants to reuse the main constructors.
// func configurationToOptions(config Configuration) []Option {
// 	options := make([]Option, 0, 10)

// 	if config.IdleTimeout > 0 {
// 		options = append(options, WithIdleTimeout(config.IdleTimeout))
// 	}

// 	if config.Wheel != nil {
// 		options = append(options, WithWheel(config.Wheel))
// 	}

// 	if config.RecreationPolicy != RecreateNever {
// 		options = append(options, WithRecreationPolicy(config.RecreationPolicy))
// 	}

// 	if len(config.InitialResources) > 0 {
// 		options = append(options, func(configuration *Configuration) {
// 			configuration.InitialResources = config.InitialResources
// 		})
// 	}

// 	if config.Destructor != nil {
// 		options = append(options, func(configuration *Configuration) {
// 			configuration.Destructor = config.Destructor
// 		})
// 	}

// 	if config.OnExpired != nil {
// 		options = append(options, func(configuration *Configuration) {
// 			configuration.OnExpired = config.OnExpired
// 		})
// 	}

// 	if config.ConstructorErrorHandler != nil {
// 		options = append(options, WithConstructorErrorHandler(config.ConstructorErrorHandler))
// 	}

// 	if config.ReleaseErrorHandler != nil {
// 		options = append(options, WithReleaseErrorHandler(config.ReleaseErrorHandler))
// 	}

// 	if config.MaxConstructorRetries > 0 {
// 		options = append(options, WithMaxConstructorRetries(config.MaxConstructorRetries))
// 	}

// 	if config.CircuitBreaker != nil {
// 		options = append(options, WithCircuitBreaker(config.CircuitBreaker))
// 	}

// 	if config.Backoff != nil {
// 		options = append(options, WithBackoff(config.Backoff))
// 	}

// 	return options
// }
