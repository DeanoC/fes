package remotemedia

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrCaptureUnavailable = errors.New("physical HDMI capture is unavailable")

type CaptureConfig struct {
	Device  string
	Width   int
	Height  int
	FPS     FrameRate
	Bitrate int
	GOP     int
}

type CaptureDevice struct {
	Name     string `json:"name"`
	UniqueID string `json:"unique_id"`
	External bool   `json:"external"`
}

type CaptureStats struct {
	Width            int       `json:"width"`
	Height           int       `json:"height"`
	FPS              FrameRate `json:"fps"`
	CapturedFrames   uint64    `json:"captured_frames"`
	DroppedFrames    uint64    `json:"dropped_frames"`
	EncodedFrames    uint64    `json:"encoded_frames"`
	EncodeErrors     uint64    `json:"encode_errors"`
	PacketQueueDrops uint64    `json:"packet_queue_drops"`
	QueueDepth       int       `json:"queue_depth"`
	QueueHighWater   int       `json:"queue_high_water"`
	RuntimeError     string    `json:"runtime_error,omitempty"`
}

func (s CaptureStats) SourceFPS() float64 {
	if s.FPS.Numerator <= 0 || s.FPS.Denominator <= 0 {
		return 0
	}
	return float64(s.FPS.Numerator) / float64(s.FPS.Denominator)
}

type EncodedSample struct {
	CaptureMonoNS  int64
	EncodeDuration time.Duration
	AVCC           []byte
	SPS            []byte
	PPS            []byte
	NALLengthSize  int
	Keyframe       bool
	Width          int
	Height         int
}

type CaptureSource interface {
	Start() error
	Next(context.Context) (EncodedSample, error)
	Stats() CaptureStats
	Close() error
}

// IsScreenCaptureDevice reports whether the Darwin backend should capture the
// host's main display rather than an external UVC device.
func IsScreenCaptureDevice(device string) bool {
	return strings.HasPrefix(strings.TrimSpace(device), "screen")
}

type KeyframeRequester interface {
	RequestKeyframe() error
}

func validateCaptureConfig(config CaptureConfig) error {
	if config.Width < 0 || config.Height < 0 {
		return errors.New("capture width and height cannot be negative")
	}
	if config.FPS.Numerator < 0 || config.FPS.Denominator < 0 {
		return errors.New("capture frame rate cannot be negative")
	}
	if (config.FPS.Numerator == 0) != (config.FPS.Denominator == 0) {
		return errors.New("capture frame rate numerator and denominator must both be set or unset")
	}
	if config.Bitrate < 0 || config.GOP < 0 {
		return errors.New("capture bitrate and GOP cannot be negative")
	}
	return nil
}
