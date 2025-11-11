package buffer

type Metrics struct {
	Capacity  int
	Occupancy int
}

func (m Metrics) Available() int {
	if m.Occupancy <= 0 {
		return m.Capacity
	}

	if remaining := m.Capacity - m.Occupancy; remaining > 0 {
		return remaining
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
