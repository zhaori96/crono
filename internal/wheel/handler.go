package wheel

import (
	"sync/atomic"
)

type Handler interface {
	Cancel() bool
}

// ========== Handler Pool ==========

type handlerPool struct {
	head atomic.Pointer[scheduledHandler]
}

func (p *handlerPool) acquire() *scheduledHandler {
	for {
		currentHead := p.head.Load()
		if currentHead == nil {
			return &scheduledHandler{}
		}
		nextHead := currentHead.next
		if p.head.CompareAndSwap(currentHead, nextHead) {
			currentHead.next = nil
			return currentHead
		}
	}
}

func (p *handlerPool) release(h *scheduledHandler) {
	if h == nil {
		return
	}

	h.wheel = nil
	h.entry = nil
	h.slot = nil
	for {
		currentHead := p.head.Load()
		h.next = currentHead
		if p.head.CompareAndSwap(currentHead, h) {
			return
		}
	}
}

type scheduledHandler struct {
	wheel   *Wheel
	entry   *wheelEntry
	slot    *wheelSlot
	next    *scheduledHandler
	entryId uint64

	canceling atomic.Bool
}

func (h *scheduledHandler) init(
	wheel *Wheel,
	slot *wheelSlot,
	entry *wheelEntry,
) *scheduledHandler {
	h.wheel = wheel
	h.slot = slot
	h.entry = entry
	h.entryId = entry.version.Next()
	return h
}

func (h *scheduledHandler) Cancel() bool {
	if h == nil || h.wheel == nil {
		return false
	}

	if !h.canceling.CompareAndSwap(false, true) {
		return false
	}

	if !h.ensureOwnership() {
		h.canceling.Store(false)
		return false
	}

	h.slot.Lock()
	defer h.slot.Unlock()

	if !h.ensureOwnership() {
		h.canceling.Store(false)
		return false
	}

	h.slot.removeEntry(h.entry)
	h.entry.state = entryStateCancelled
	h.wheel.entryPool.releaseEntry(h.entry, true)
	h.wheel.cancelledCount.Add(1)
	h.canceling.Store(false)
	h.wheel.handlerPool.release(h)
	return true
}

func (h *scheduledHandler) ownsEntry() bool {
	return h.entry != nil &&
		h.wheel != nil &&
		h.slot != nil &&
		h.entry.state == entryStateScheduled &&
		h.entry.version.Current() == h.entryId
}

func (h *scheduledHandler) ensureOwnership() bool {
	if h.ownsEntry() {
		return true
	}
	if h.wheel != nil {
		h.wheel.handlerPool.release(h)
	}
	return false
}
