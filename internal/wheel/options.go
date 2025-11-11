package wheel

import "time"

type Option func(*configuration)

type configuration struct {
	tickInterval           time.Duration
	slotCount              uint32
	maxRounds              uint32
	deterministicJitterMax uint32
	onExpire               func(Expirable)
	onReschedule           func(Expirable, time.Duration)
}

const (
	defaultTickInterval = 10 * time.Millisecond
	defaultSlotCount    = 64
)

func defaultConfiguration() configuration {
	return configuration{
		tickInterval:           defaultTickInterval,
		slotCount:              defaultSlotCount,
		maxRounds:              ^uint32(0),
		deterministicJitterMax: 0,
	}
}

func WithTickInterval(interval time.Duration) Option {
	return func(configurationData *configuration) {
		if interval > 0 {
			configurationData.tickInterval = interval
		}
	}
}

func WithSlotCount(count uint32) Option {
	return func(configurationData *configuration) {
		if count > 0 {
			configurationData.slotCount = count
		}
	}
}

func WithMaxRounds(rounds uint32) Option {
	return func(configurationData *configuration) {
		if rounds > 0 {
			configurationData.maxRounds = rounds
		}
	}
}

func WithExpireHook(callback func(Expirable)) Option {
	return func(configurationData *configuration) {
		if callback != nil {
			configurationData.onExpire = callback
		}
	}
}

func WithRescheduleHook(callback func(Expirable, time.Duration)) Option {
	return func(configurationData *configuration) {
		if callback != nil {
			configurationData.onReschedule = callback
		}
	}
}

func WithDeterministicJitterSpan(maximumAdditionalTicks uint32) Option {
	return func(configurationData *configuration) {
		configurationData.deterministicJitterMax = maximumAdditionalTicks
	}
}
