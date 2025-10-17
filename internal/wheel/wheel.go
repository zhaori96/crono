package wheel

import (
	"context"
	"errors"
	"math"
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhaori96/crono/internal/util"
)

type Wheel struct {
	_ util.NoCopy

	tickInterval time.Duration
	slotCount    uint32
	maxRounds    uint32
	slotMask     uint32
	slotPolicy   SlotPolicy

	slots []wheelSlot

	position atomic.Uint32
	running  atomic.Bool

	deterministicJitterMax uint32
	jitterCounter          atomic.Uint64

	control sync.Mutex

	stopSignal    chan struct{}
	stoppedSignal chan struct{}
	entryPool     wheelEntryPool

	onExpireCallback     func(Expirable)
	onRescheduleCallback func(Expirable, time.Duration)

	scheduledCount   atomic.Uint64
	expiredCount     atomic.Uint64
	cancelledCount   atomic.Uint64
	rescheduledCount atomic.Uint64
}

type wheelSlot struct {
	headEntry *wheelEntry
	tailEntry *wheelEntry
	mutex     sync.Mutex
}

type wheelEntry struct {
	_ util.NoCopy

	nextEntry     *wheelEntry
	previousEntry *wheelEntry

	target     Expirable
	roundsLeft uint32

	state entryState
}

type entryState uint32

const (
	entryStateIdle entryState = iota
	entryStateScheduled
	entryStateExpired
	entryStateCancelled
)

type scheduledHandle struct {
	wheelReference *Wheel
	entryReference *wheelEntry
	slotReference  *wheelSlot
	deadline       time.Duration
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

type wheelEntryPool struct {
	headPointer atomic.Pointer[wheelEntry]
}

var (
	errInvalidTick    = errors.New("tick interval must be greater than zero")
	errInvalidSlots   = errors.New("slot count must be greater than zero")
	errOverflowRounds = errors.New("max rounds must be greater than zero")
	errAlreadyRunning = errors.New("wheel already started")
	errNotRunning     = errors.New("wheel is not running")
)

func NewWheel(options ...Option) (*Wheel, error) {
	configurationParameters := defaultConfiguration()
	for _, option := range options {
		option(&configurationParameters)
	}

	if configurationParameters.tickInterval <= 0 {
		return nil, errInvalidTick
	}

	if configurationParameters.slotCount == 0 {
		return nil, errInvalidSlots
	}

	if configurationParameters.maxRounds == 0 {
		return nil, errOverflowRounds
	}

	normalizedSlotCount := normalizeSlotCount(configurationParameters.slotCount)

	wheel := &Wheel{
		tickInterval:           normalizeTickInterval(configurationParameters.tickInterval),
		slotCount:              normalizedSlotCount,
		slotMask:               normalizedSlotCount - 1,
		maxRounds:              configurationParameters.maxRounds,
		slotPolicy:             configurationParameters.slotPolicy,
		deterministicJitterMax: configurationParameters.deterministicJitterMax,
		slots:                  make([]wheelSlot, normalizedSlotCount),
		onExpireCallback:       configurationParameters.onExpire,
		onRescheduleCallback:   configurationParameters.onReschedule,
	}

	return wheel, nil
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

func (w *Wheel) Schedule(target Expirable, timeout time.Duration) (Handle, error) {
	if target == nil {
		return nil, errors.New("expirable target cannot be nil")
	}
	if timeout < 0 {
		return nil, errors.New("timeout cannot be negative")
	}
	if !w.Running() {
		return nil, errNotRunning
	}

	slotOffset, requiredRounds := w.resolvePlacement(timeout)
	if requiredRounds > w.maxRounds {
		return nil, errors.New("timeout exceeds maximum supported rounds")
	}

	scheduledEntry := w.entryPool.acquireEntry()
	scheduledEntry.target = target
	scheduledEntry.roundsLeft = requiredRounds
	scheduledEntry.state = entryStateScheduled

	wheelSlotIndex := (w.Position() + slotOffset) & w.slotMask
	slotReference := &w.slots[wheelSlotIndex]
	slotReference.mutex.Lock()
	w.enqueueEntryWithPolicy(slotReference, scheduledEntry)
	slotReference.mutex.Unlock()

	w.scheduledCount.Add(1)

	return &scheduledHandle{
		wheelReference: w,
		entryReference: scheduledEntry,
		slotReference:  slotReference,
		deadline:       timeout,
	}, nil
}

func (w *Wheel) Start() error {
	w.control.Lock()
	defer w.control.Unlock()

	if w.running.Load() {
		return errAlreadyRunning
	}

	w.position.Store(0)

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
	if !w.running.Load() || w.stopSignal == nil {
		w.control.Unlock()
		return errNotRunning
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

func (w *Wheel) run(ticker *time.Ticker, stopSignal <-chan struct{}, stoppedSignal chan<- struct{}) {
	defer func() {
		ticker.Stop()
		w.running.Store(false)
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
	slotMask := w.slotMask
	for {
		currentPosition := w.position.Load()
		nextPosition := (currentPosition + 1) & slotMask
		if w.position.CompareAndSwap(currentPosition, nextPosition) {
			return nextPosition
		}
	}
}

func (w *Wheel) processSlot(slotIndex uint32) {
	slotReference := &w.slots[slotIndex]
	slotReference.mutex.Lock()
	currentEntry := slotReference.headEntry
	for currentEntry != nil {
		nextEntry := currentEntry.nextEntry
		if currentEntry.roundsLeft > 0 {
			currentEntry.roundsLeft--
		} else {
			slotReference.removeEntry(currentEntry)
			w.expireEntry(currentEntry)
		}
		currentEntry = nextEntry
	}
	slotReference.mutex.Unlock()
}

func (w *Wheel) expireEntry(entryReference *wheelEntry) {
	if entryReference == nil {
		return
	}
	if entryReference.state != entryStateScheduled {
		return
	}
	entryReference.state = entryStateExpired
	target := entryReference.target
	entryReference.target = nil
	if target != nil && !target.Expired() {
		target.Expire()
		if w.onExpireCallback != nil {
			w.onExpireCallback(target)
		}
	}
	w.expiredCount.Add(1)
	w.entryPool.releaseEntry(entryReference)
}

func (w *Wheel) resolvePlacement(timeout time.Duration) (uint32, uint32) {
	tickCount := w.calculateTickCount(timeout)
	tickCount = w.applyDeterministicJitter(tickCount)

	roundsRequired := uint32(tickCount / uint64(w.slotCount))
	offset := uint32(tickCount % uint64(w.slotCount))
	return offset, roundsRequired
}

func (h *scheduledHandle) Cancel() bool {
	if h == nil || h.entryReference == nil {
		return false
	}

	if !h.wheelReference.Running() {
		return false
	}

	slotReference := h.slotReference
	slotReference.mutex.Lock()
	defer slotReference.mutex.Unlock()

	if h.entryReference.state != entryStateScheduled {
		return h.entryReference.state == entryStateExpired
	}

	slotReference.removeEntry(h.entryReference)
	h.entryReference.state = entryStateCancelled
	h.wheelReference.entryPool.releaseEntry(h.entryReference)
	h.entryReference = nil
	h.wheelReference.cancelledCount.Add(1)
	return true
}

func (h *scheduledHandle) Reset(timeout time.Duration) error {
	if h == nil || h.entryReference == nil {
		return errors.New("handle is not active")
	}
	if timeout < 0 {
		return errors.New("timeout cannot be negative")
	}
	if !h.wheelReference.Running() {
		return errNotRunning
	}

	slotReference := h.slotReference
	slotReference.mutex.Lock()
	currentState := h.entryReference.state
	if currentState != entryStateScheduled {
		slotReference.mutex.Unlock()
		if currentState == entryStateExpired {
			return nil
		}
		return errors.New("entry is not scheduled")
	}
	slotReference.removeEntry(h.entryReference)
	slotReference.mutex.Unlock()

	slotOffset, requiredRounds := h.wheelReference.resolvePlacement(timeout)
	if requiredRounds > h.wheelReference.maxRounds {
		return errors.New("timeout exceeds maximum supported rounds")
	}

	newSlotIndex := (h.wheelReference.Position() + slotOffset) & h.wheelReference.slotMask
	newSlotReference := &h.wheelReference.slots[newSlotIndex]
	newSlotReference.mutex.Lock()
	h.entryReference.roundsLeft = requiredRounds
	h.entryReference.state = entryStateScheduled
	h.wheelReference.enqueueEntryWithPolicy(newSlotReference, h.entryReference)
	newSlotReference.mutex.Unlock()

	h.slotReference = newSlotReference
	h.deadline = timeout
	h.wheelReference.rescheduledCount.Add(1)
	if h.wheelReference.onRescheduleCallback != nil && h.entryReference.target != nil {
		h.wheelReference.onRescheduleCallback(h.entryReference.target, timeout)
	}
	return nil
}

func (h *scheduledHandle) KeepAlive() error {
	if h == nil {
		return errors.New("handle is not active")
	}
	return h.Reset(h.deadline)
}

func normalizeTickInterval(interval time.Duration) time.Duration {
	if interval <= time.Microsecond {
		return time.Microsecond
	}
	return interval
}

func (w *Wheel) enqueueEntryWithPolicy(slotReference *wheelSlot, entryReference *wheelEntry) {
	switch w.slotPolicy {
	case SlotPolicyInsertionOrder:
		slotReference.enqueueEntry(entryReference)
	default:
		slotReference.enqueueEntry(entryReference)
	}
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

func mixDeterministic(value uint64) uint64 {
	value ^= value >> 33
	value *= 0xff51afd7ed558ccd
	value ^= value >> 33
	value *= 0xc4ceb9fe1a85ec53
	value ^= value >> 33
	return value
}

func normalizeSlotCount(slotCount uint32) uint32 {
	if slotCount == 0 {
		return 1
	}

	if bits.OnesCount32(slotCount) == 1 {
		return slotCount
	}

	nextPowerOfTwo := uint32(1 << (bits.Len32(slotCount)))
	if nextPowerOfTwo == 0 {
		return math.MaxUint32
	}
	return nextPowerOfTwo
}

func (p *wheelEntryPool) acquireEntry() *wheelEntry {
	for {
		currentHead := p.headPointer.Load()
		if currentHead == nil {
			return &wheelEntry{}
		}
		nextHead := currentHead.nextEntry
		if p.headPointer.CompareAndSwap(currentHead, nextHead) {
			currentHead.nextEntry = nil
			currentHead.previousEntry = nil
			currentHead.state = entryStateIdle
			currentHead.roundsLeft = 0
			currentHead.target = nil
			return currentHead
		}
	}
}

func (p *wheelEntryPool) releaseEntry(entryReference *wheelEntry) {
	if entryReference == nil {
		return
	}
	entryReference.target = nil
	entryReference.roundsLeft = 0
	entryReference.state = entryStateIdle

	for {
		currentHead := p.headPointer.Load()
		entryReference.nextEntry = currentHead
		if p.headPointer.CompareAndSwap(currentHead, entryReference) {
			return
		}
	}
}

func (s *wheelSlot) enqueueEntry(entryReference *wheelEntry) {
	if entryReference == nil {
		return
	}
	entryReference.nextEntry = nil
	entryReference.previousEntry = s.tailEntry
	if s.tailEntry == nil {
		s.headEntry = entryReference
	} else {
		s.tailEntry.nextEntry = entryReference
	}
	s.tailEntry = entryReference
}

func (s *wheelSlot) removeEntry(entryReference *wheelEntry) {
	if entryReference == nil {
		return
	}
	if entryReference.previousEntry != nil {
		entryReference.previousEntry.nextEntry = entryReference.nextEntry
	} else {
		s.headEntry = entryReference.nextEntry
	}
	if entryReference.nextEntry != nil {
		entryReference.nextEntry.previousEntry = entryReference.previousEntry
	} else {
		s.tailEntry = entryReference.previousEntry
	}
	entryReference.nextEntry = nil
	entryReference.previousEntry = nil
}

func (s *wheelSlot) detachAllEntries() *wheelEntry {
	headEntry := s.headEntry
	s.headEntry = nil
	s.tailEntry = nil
	return headEntry
}
