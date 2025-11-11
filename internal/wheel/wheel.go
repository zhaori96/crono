package wheel

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhaori96/crono/internal/util"
)

type Expirable interface {
	Expire() bool
	Expired() bool
}

type Wheel struct {
	_ util.NoCopy

	tickInterval time.Duration
	slotCount    uint32
	maxRounds    uint32

	slots []wheelSlot

	position atomic.Uint32
	running  atomic.Bool

	deterministicJitterMax uint32
	jitterCounter          atomic.Uint64

	control sync.Mutex

	stopSignal    chan struct{}
	stoppedSignal chan struct{}
	entryPool     wheelEntryPool
	handlerPool   handlerPool

	onExpireCallback     func(Expirable)
	onRescheduleCallback func(Expirable, time.Duration)

	scheduledCount   atomic.Uint64
	expiredCount     atomic.Uint64
	cancelledCount   atomic.Uint64
	rescheduledCount atomic.Uint64
}

func NewWheel(options ...Option) (*Wheel, error) {
	configurationParameters := defaultConfiguration()
	for _, option := range options {
		option(&configurationParameters)
	}

	if configurationParameters.tickInterval <= 0 {
		return nil, ErrInvalidTickInterval
	}

	if configurationParameters.slotCount == 0 {
		return nil, ErrInvalidSlotCount
	}

	if configurationParameters.maxRounds == 0 {
		return nil, ErrInvalidMaxRounds
	}

	wheel := &Wheel{
		tickInterval:           normalizeTickInterval(configurationParameters.tickInterval),
		slotCount:              configurationParameters.slotCount,
		maxRounds:              configurationParameters.maxRounds,
		deterministicJitterMax: configurationParameters.deterministicJitterMax,
		slots:                  make([]wheelSlot, configurationParameters.slotCount),
		onExpireCallback:       configurationParameters.onExpire,
		onRescheduleCallback:   configurationParameters.onReschedule,
	}

	return wheel, nil
}

func (w *Wheel) ScheduledCount() uint64 {
	return w.scheduledCount.Load()
}

func (w *Wheel) ExpiredCount() uint64 {
	return w.expiredCount.Load()
}

func (w *Wheel) CancelledCount() uint64 {
	return w.cancelledCount.Load()
}

func (w *Wheel) RescheduledCount() uint64 {
	return w.rescheduledCount.Load()
}

func (w *Wheel) TickInterval() time.Duration {
	return w.tickInterval
}

func (w *Wheel) SlotCount() uint32 {
	return w.slotCount
}

func (w *Wheel) MaxRounds() uint32 {
	return w.maxRounds
}

func (w *Wheel) Position() uint32 {
	return w.position.Load()
}

func (w *Wheel) Running() bool {
	return w.running.Load()
}

func (w *Wheel) Schedule(
	target Expirable,
	timeout time.Duration,
) (Handler, error) {
	if target == nil {
		return nil, ErrNilExpirable
	}
	if timeout < 0 {
		return nil, ErrNegativeTimeout
	}
	if !w.Running() {
		return nil, ErrWheelNotRunning
	}

	entry := w.entryPool.acquireEntry()
	entry.target = target

	slot, err := w.insertEntryIntoSlot(entry, timeout)
	if err != nil {
		entry.state = entryStateIdle
		entry.target = target
		entry.roundsLeft = 0
		w.entryPool.releaseEntry(entry, true)
		return nil, err
	}

	w.scheduledCount.Add(1)
	handler := w.handlerPool.acquire().init(w, slot, entry)
	return handler, nil
}

func (w *Wheel) Start() error {
	w.control.Lock()
	defer w.control.Unlock()

	if w.running.Load() {
		return ErrWheelAlreadyRunning
	}

	defer w.position.Store(0)

	stopSignalChannel := make(chan struct{})
	stoppedSignalChannel := make(chan struct{})
	w.stopSignal = stopSignalChannel
	w.stoppedSignal = stoppedSignalChannel

	ticker := time.NewTicker(w.tickInterval)
	w.running.Store(true)

	go w.run(ticker, stopSignalChannel, stoppedSignalChannel)
	return nil
}

func (w *Wheel) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	w.control.Lock()
	if !w.running.CompareAndSwap(true, false) || w.stopSignal == nil {
		w.control.Unlock()
		return ErrWheelNotRunning
	}

	stopSignalChannel := w.stopSignal
	stoppedSignalChannel := w.stoppedSignal
	w.stopSignal = nil
	w.stoppedSignal = nil
	w.control.Unlock()

	close(stopSignalChannel)

	select {
	case <-stoppedSignalChannel:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Wheel) run(
	ticker *time.Ticker,
	stopSignal <-chan struct{},
	stoppedSignal chan<- struct{},
) {
	defer func() {
		ticker.Stop()
		w.drainAllSlots()
		close(stoppedSignal)
	}()

	for {
		select {
		case <-stopSignal:
			return
		case <-ticker.C:
			w.processTick()
		}
	}
}

func (w *Wheel) processTick() {
	slotIndex := w.advancePosition()
	w.processSlot(slotIndex)
}

func (w *Wheel) advancePosition() uint32 {
	for {
		currentPosition := w.position.Load()
		nextPosition := (currentPosition + 1) % w.slotCount
		if w.position.CompareAndSwap(currentPosition, nextPosition) {
			return nextPosition
		}
	}
}

func (w *Wheel) processSlot(index uint32) {
	slot := &w.slots[index]
	slot.Lock()
	defer slot.Unlock()

	currentEntry := slot.headEntry
	for currentEntry != nil {
		nextEntry := currentEntry.next
		if currentEntry.roundsLeft > 0 {
			currentEntry.roundsLeft--
		} else {
			slot.removeEntry(currentEntry)
			w.expireEntry(currentEntry, true)
		}
		currentEntry = nextEntry
	}
}

func (w *Wheel) expireEntry(entry *wheelEntry, reenqueue bool) {
	if entry == nil || !entry.isScheduled() {
		return
	}
	defer w.entryPool.releaseEntry(entry, reenqueue)

	target := entry.target
	if target == nil {
		w.expiredCount.Add(1)
		return
	}

	entry.target = nil
	if !target.Expire() {
		entry.state = entryStateCancelled
		w.cancelledCount.Add(1)
		return
	}

	entry.state = entryStateExpired
	if w.onExpireCallback != nil {
		w.onExpireCallback(target)
	}

	w.expiredCount.Add(1)
}

func (w *Wheel) resolvePlacement(timeout time.Duration) (uint32, uint32) {
	tickCount := w.calculateTickCount(timeout)
	tickCount = w.applyDeterministicJitter(tickCount)

	roundsRequired := uint32(tickCount / uint64(w.slotCount))
	offset := uint32(tickCount % uint64(w.slotCount))
	return offset, roundsRequired
}

func (w *Wheel) calculateTickCount(timeout time.Duration) uint64 {
	if timeout <= 0 {
		return 1
	}

	tickCount := uint64(timeout / w.tickInterval)
	if timeout%w.tickInterval != 0 {
		tickCount++
	}

	if tickCount == 0 {
		tickCount = 1
	}

	return tickCount
}

func (w *Wheel) applyDeterministicJitter(tickCount uint64) uint64 {
	if w.deterministicJitterMax == 0 || tickCount <= 1 {
		return tickCount
	}

	counterValue := w.jitterCounter.Add(1)
	jitterRange := uint64(w.deterministicJitterMax) + 1
	jitterValue := mixDeterministic(counterValue) % jitterRange

	if math.MaxUint64-tickCount < jitterValue {
		return math.MaxUint64
	}

	return tickCount + jitterValue
}

func (w *Wheel) drainAllSlots() {
	for index := range w.slots {
		slot := &w.slots[index]
		slot.Lock()
		headEntry := slot.detachAllEntries()
		slot.Unlock()

		for headEntry != nil {
			nextEntry := headEntry.next
			headEntry.next = nil
			headEntry.previous = nil
			if headEntry.state == entryStateScheduled {
				w.expireEntry(headEntry, false)
			} else {
				headEntry.target = nil
				headEntry.roundsLeft = 0
				w.entryPool.releaseEntry(headEntry, false)
			}
			headEntry = nextEntry
		}
	}
}

func (w *Wheel) insertEntryIntoSlot(
	entry *wheelEntry,
	timeout time.Duration,
) (*wheelSlot, error) {
	slotOffset, requiredRounds := w.resolvePlacement(timeout)
	if requiredRounds > w.maxRounds {
		return nil, ErrTimeoutOverflow
	}

	slotIndex := (w.Position() + slotOffset) % w.slotCount
	slot := &w.slots[slotIndex]

	slot.Lock()
	defer slot.Unlock()
	if !w.running.Load() {
		return nil, ErrWheelNotRunning
	}
	entry.roundsLeft = requiredRounds
	entry.state = entryStateScheduled
	slot.enqueueEntry(entry)

	return slot, nil
}

func (w *Wheel) rescheduleEntry(entry *wheelEntry, timeout time.Duration) (*wheelSlot, error) {
	slot, err := w.insertEntryIntoSlot(entry, timeout)
	if err != nil {
		return nil, err
	}

	w.rescheduledCount.Add(1)
	if w.onRescheduleCallback != nil && entry.target != nil {
		w.onRescheduleCallback(entry.target, timeout)
	}

	return slot, nil
}

func normalizeTickInterval(interval time.Duration) time.Duration {
	if interval < time.Microsecond {
		return time.Microsecond
	}
	return interval
}

func mixDeterministic(value uint64) uint64 {
	value ^= value >> 33
	value *= 0xff51afd7ed558ccd
	value ^= value >> 33
	value *= 0xc4ceb9fe1a85ec53
	value ^= value >> 33
	return value
}
