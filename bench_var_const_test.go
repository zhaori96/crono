package crono

import "testing"

// Simula o cenário real da wheel
type WheelWithMask struct {
	slotMask uint32
}

type WheelWithModulo struct {
	slotCount uint32
}

func BenchmarkMaskVariable(b *testing.B) {
	wheel := &WheelWithMask{slotMask: 63}
	position := uint32(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		position = (position + 1) & wheel.slotMask
	}
}

func BenchmarkModuloVariable(b *testing.B) {
	wheel := &WheelWithModulo{slotCount: 64}
	position := uint32(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		position = (position + 1) % wheel.slotCount
	}
}

func BenchmarkModuloConstant(b *testing.B) {
	position := uint32(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		position = (position + 1) % 64
	}
}

func BenchmarkMaskConstant(b *testing.B) {
	position := uint32(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		position = (position + 1) & 63
	}
}
