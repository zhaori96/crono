package wheel

import (
	"sync/atomic"
	"unsafe"

	"github.com/zhaori96/crono/internal/util"
)

type entryState uint32

const (
	entryStateIdle entryState = iota
	entryStateScheduled
	entryStateExpired
	entryStateCancelled
)

type wheelEntry struct {
	_ util.NoCopy

	next     *wheelEntry
	previous *wheelEntry

	target     Expirable
	roundsLeft uint32

	state   entryState
	version Sequence
}

func (p *wheelEntry) isScheduled() bool {
	return p.state == entryStateScheduled
}

type wheelEntryPool struct {
	head atomic.Pointer[wheelEntry]
}

func (p *wheelEntryPool) acquireEntry() *wheelEntry {
	for {
		head := p.head.Load()
		if head == nil {
			return &wheelEntry{}
		}
		nextHead := head.next
		if p.head.CompareAndSwap(head, nextHead) {
			clearEntry(head)
			return head
		}
	}
}

func (p *wheelEntryPool) releaseEntry(
	entry *wheelEntry,
	reenqueue bool,
) {
	if entry == nil {
		return
	}

	clearEntry(entry)
	if !reenqueue {
		return
	}

	for {
		head := p.head.Load()
		entry.next = head
		if p.head.CompareAndSwap(head, entry) {
			return
		}
	}
}

func clearEntry(entry *wheelEntry) {
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&entry.previous)), nil)
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&entry.next)), nil)
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&entry.target)), nil)

	entry.state = entryStateIdle
	entry.roundsLeft = 0
}
