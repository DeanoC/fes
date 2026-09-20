package remotemedia

import (
	"errors"
	"math"
)

type RTPClock struct {
	baseRTP       uint32
	baseCaptureNS int64
	lastCaptureNS int64
	initialized   bool
}

func NewRTPClock(baseRTP uint32) *RTPClock {
	return &RTPClock{baseRTP: baseRTP}
}

func (c *RTPClock) Timestamp(captureMonoNS int64) (uint32, error) {
	if c == nil {
		return 0, errors.New("RTP clock is nil")
	}
	if captureMonoNS < 0 {
		return 0, errors.New("capture timestamp is negative")
	}
	if c.initialized && captureMonoNS <= c.lastCaptureNS {
		return 0, errors.New("capture timestamp did not advance")
	}
	if !c.initialized {
		c.initialized = true
		c.baseCaptureNS = captureMonoNS
		c.lastCaptureNS = captureMonoNS
		return c.baseRTP, nil
	}
	delta := captureMonoNS - c.baseCaptureNS
	if delta < 0 {
		return 0, errors.New("capture timestamp is before clock origin")
	}
	c.lastCaptureNS = captureMonoNS
	return c.baseRTP + uint32(scale90kHz(delta)), nil
}

func scale90kHz(deltaNS int64) int64 {
	quotient := deltaNS / 1_000_000_000
	remainder := deltaNS % 1_000_000_000
	value := quotient * RTPClockRate
	value += (remainder*RTPClockRate + 500_000_000) / 1_000_000_000
	if value < 0 || value > math.MaxInt64 {
		return math.MaxInt64
	}
	return value
}
