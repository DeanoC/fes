package remotemedia

import (
	"context"
	"errors"
)

// AudioSinkConfig exposes only target-private capabilities. Device paths,
// process layout, and platform APIs remain implementation-private.
type AudioSinkConfig struct {
	Format       AudioFormat
	Capabilities []AudioSinkCapability
}

type AudioSinkCapability uint8

const (
	AudioSinkCapabilityPCM16 AudioSinkCapability = iota + 1
)

func ValidateAudioSinkConfig(config AudioSinkConfig) error {
	if err := ValidateAudioFormat(config.Format); err != nil {
		return err
	}
	hasPCM16 := false
	for _, capability := range config.Capabilities {
		switch capability {
		case AudioSinkCapabilityPCM16:
			if hasPCM16 {
				return errors.New("duplicate audio sink capability")
			}
			hasPCM16 = true
		default:
			return errors.New("unknown audio sink capability")
		}
	}
	if !hasPCM16 {
		return errors.New("audio sink must declare PCM16 capability")
	}
	return nil
}

type AudioSinkStats struct {
	WrittenFrames  uint64                `json:"written_frames"`
	DroppedFrames  uint64                `json:"dropped_frames"`
	QueueDepth     int                   `json:"queue_depth"`
	QueueHighWater int                   `json:"queue_high_water"`
	RuntimeError   AudioRuntimeErrorCode `json:"runtime_error,omitempty"`
	Shutdown       bool                  `json:"shutdown"`
}

type AudioSink interface {
	Open(context.Context, AudioSinkConfig) error
	Ready(context.Context) error
	Write(context.Context, AudioFrame) error
	Drain(context.Context) error
	Mute(context.Context) error
	Reset(context.Context) error
	Stats() AudioSinkStats
	Close(context.Context) error
}

type AudioPlayoutStats struct {
	PushedFrames   uint64 `json:"pushed_frames"`
	PlayedFrames   uint64 `json:"played_frames"`
	DroppedFrames  uint64 `json:"dropped_frames"`
	QueueDepth     int    `json:"queue_depth"`
	QueueHighWater int    `json:"queue_high_water"`
	Underruns      uint64 `json:"underruns"`
	Overruns       uint64 `json:"overruns"`
	SequenceGaps   uint64 `json:"sequence_gaps"`
}

type AudioPlayout interface {
	Push(context.Context, AudioFrame) error
	Run(context.Context, AudioSink) error
	Stop(context.Context) error
	Stats() AudioPlayoutStats
}
