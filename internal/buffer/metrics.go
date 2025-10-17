package buffer

type Metrics struct {
	Capacity  uint32
	Occupancy int32
}

func (m Metrics) Available() uint32 {
	if m.Occupancy <= 0 {
		return m.Capacity
	}

	if remaining := int64(m.Capacity) - int64(m.Occupancy); remaining > 0 {
		return uint32(remaining)
	}

	return 0
}

func (m Metrics) Utilization() float64 {
	if m.Capacity == 0 {
		return 0
	}
	return float64(m.Occupancy) / float64(m.Capacity)
}

type StateSnapshot struct {
	Head uint32
	Tail uint32
}
