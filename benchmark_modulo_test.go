package crono

import "testing"

// Benchmark usando % (módulo)
func BenchmarkModulo(b *testing.B) {
	slotCount := uint32(64)
	position := uint32(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		position = (position + 1) % slotCount
	}
}

// Benchmark usando & (máscara)
func BenchmarkMask(b *testing.B) {
	slotMask := uint32(63)
	position := uint32(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		position = (position + 1) & slotMask
	}
}

// Benchmark do custo de normalização
func BenchmarkNormalize(b *testing.B) {
	inputs := []uint32{1, 7, 15, 30, 50, 63, 64, 100, 127, 128}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = normalizeSlotCount(inputs[i%len(inputs)])
	}
}

func normalizeSlotCount(slotCount uint32) uint32 {
	if slotCount == 0 {
		return 1
	}

	// Verifica se já é potência de 2
	if (slotCount & (slotCount - 1)) == 0 {
		return slotCount
	}

	// Arredonda para próxima potência de 2
	slotCount--
	slotCount |= slotCount >> 1
	slotCount |= slotCount >> 2
	slotCount |= slotCount >> 4
	slotCount |= slotCount >> 8
	slotCount |= slotCount >> 16
	slotCount++

	return slotCount
}
