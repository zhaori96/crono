package buffer

import "testing"

func BenchmarkStrategicBufferGetRelease(b *testing.B) {
	const capacity = 1024

	strategicBuffer, err := NewFixedBuffer[int](capacity, AccessModeStrategic)
	if err != nil {
		b.Fatalf("failed to create strategic buffer: %v", err)
	}

	for value := 0; value < capacity; value++ {
		if putErr := strategicBuffer.Put(value); putErr != nil {
			b.Fatalf("failed to preload strategic buffer: %v", putErr)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		value, getErr := strategicBuffer.Get()
		if getErr != nil {
			b.Fatalf("get failed: %v", getErr)
		}
		if releaseErr := strategicBuffer.Put(value); releaseErr != nil {
			b.Fatalf("release failed: %v", releaseErr)
		}
	}
}

func BenchmarkSynchronousBufferGetRelease(b *testing.B) {
	const capacity = 512

	synchronousBuffer, err := NewFixedBuffer[int](capacity, AccessModeSynchronous)
	if err != nil {
		b.Fatalf("failed to create synchronous buffer: %v", err)
	}

	for value := 0; value < capacity; value++ {
		if putErr := synchronousBuffer.Put(value); putErr != nil {
			b.Fatalf("failed to preload synchronous buffer: %v", putErr)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		value, getErr := synchronousBuffer.Get()
		if getErr != nil {
			b.Fatalf("get failed: %v", getErr)
		}
		if releaseErr := synchronousBuffer.Put(value); releaseErr != nil {
			b.Fatalf("release failed: %v", releaseErr)
		}
	}
}
