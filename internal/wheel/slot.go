package wheel

import "sync"

type wheelSlot struct {
	headEntry *wheelEntry
	tailEntry *wheelEntry
	sync.Mutex
}

func (s *wheelSlot) enqueueEntry(entry *wheelEntry) {
	if entry == nil {
		return
	}
	entry.next = nil
	entry.previous = s.tailEntry
	if s.tailEntry == nil {
		s.headEntry = entry
	} else {
		s.tailEntry.next = entry
	}
	s.tailEntry = entry
}

func (s *wheelSlot) removeEntry(entry *wheelEntry) {
	if entry == nil {
		return
	}

	if entry.previous != nil {
		entry.previous.next = entry.next
	} else {
		s.headEntry = entry.next
	}

	if entry.next != nil {
		entry.next.previous = entry.previous
	} else {
		s.tailEntry = entry.previous
	}

	entry.next = nil
	entry.previous = nil
}

func (s *wheelSlot) detachAllEntries() *wheelEntry {
	headEntry := s.headEntry
	s.headEntry = nil
	s.tailEntry = nil
	return headEntry
}
