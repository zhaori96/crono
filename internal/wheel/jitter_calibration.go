package wheel

import (
	"errors"
	"math"
)

var errInvalidCalibrationSampleCount = errors.New("sample count must be greater than zero")

type JitterReport struct {
	MaximumAdditionalTicks uint32
	SampleCount            uint64
	Histogram              []uint64
	Mean                   float64
	StandardDeviation      float64
}

func CalibrateDeterministicJitter(maximumAdditionalTicks uint32, sampleCount uint64) (JitterReport, error) {
	if sampleCount == 0 {
		return JitterReport{}, errInvalidCalibrationSampleCount
	}

	histogramLength := maximumAdditionalTicks + 1
	histogram := make([]uint64, histogramLength)

	var cumulative float64
	var cumulativeSquares float64
	jitterRange := uint64(histogramLength)

	for index := uint64(1); index <= sampleCount; index++ {
		jitterValue := uint32(mixDeterministic(index) % jitterRange)
		histogram[jitterValue]++
		valueAsFloat := float64(jitterValue)
		cumulative += valueAsFloat
		cumulativeSquares += valueAsFloat * valueAsFloat
	}

	meanValue := cumulative / float64(sampleCount)
	variance := (cumulativeSquares / float64(sampleCount)) - (meanValue * meanValue)
	if variance < 0 {
		variance = 0
	}

	return JitterReport{
		MaximumAdditionalTicks: maximumAdditionalTicks,
		SampleCount:            sampleCount,
		Histogram:              histogram,
		Mean:                   meanValue,
		StandardDeviation:      math.Sqrt(variance),
	}, nil
}
