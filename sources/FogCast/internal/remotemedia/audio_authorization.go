package remotemedia

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
)

type AudioPermission uint8

const (
	AudioPermissionMicrophone AudioPermission = iota + 1
	AudioPermissionScreenRecording
)

func (permission AudioPermission) String() string {
	switch permission {
	case AudioPermissionMicrophone:
		return "Microphone"
	case AudioPermissionScreenRecording:
		return "Screen Recording"
	default:
		return "Audio capture"
	}
}

type AudioAuthorizationStatus uint8

const (
	AudioAuthorizationNotDetermined AudioAuthorizationStatus = iota
	AudioAuthorizationRestricted
	AudioAuthorizationDenied
	AudioAuthorizationAuthorized
)

func (status AudioAuthorizationStatus) String() string {
	switch status {
	case AudioAuthorizationNotDetermined:
		return "not determined"
	case AudioAuthorizationRestricted:
		return "restricted"
	case AudioAuthorizationDenied:
		return "denied"
	case AudioAuthorizationAuthorized:
		return "authorized"
	default:
		return "unknown"
	}
}

// AudioAuthorizationError intentionally names the FogCast helper rather than
// another application's grant: macOS TCC authorization is per signed bundle.
func AudioAuthorizationError(permission AudioPermission, status AudioAuthorizationStatus) error {
	if status == AudioAuthorizationAuthorized {
		return nil
	}
	return fmt.Errorf("FogCast %s authorization is %s; grant %s access to the signed FogCast helper in System Settings > Privacy & Security", permission, status, permission)
}

func validateNativeAudioSourceConfig(config AudioSourceConfig) error {
	if err := ValidateAudioConfig(AudioConfig{Enabled: true, Source: config}); err != nil {
		return err
	}
	if config.FrameSamples != DefaultAudioFrameSamples {
		return errors.New("native audio source frame samples must match the 240-sample transport contract")
	}
	return nil
}

// ConvertFloat32PCM copies AVFoundation/ScreenCaptureKit float PCM into a
// fixed PCM16LE sample. Inputs are either one interleaved slice or one planar
// slice per channel; the native callback owns no memory after this returns.
func ConvertFloat32PCM(input [][]float32, planar bool, format AudioFormat, captureMonoNS int64) (AudioSample, error) {
	if err := ValidateAudioFormat(format); err != nil {
		return AudioSample{}, err
	}
	if captureMonoNS < 0 {
		return AudioSample{}, errors.New("audio capture timestamp cannot be negative")
	}
	var frames int
	if planar {
		if len(input) != format.Channels {
			return AudioSample{}, errors.New("planar audio channel count does not match format")
		}
		frames = len(input[0])
		for _, channel := range input {
			if len(channel) != frames {
				return AudioSample{}, errors.New("planar audio channels have unequal lengths")
			}
		}
	} else {
		if len(input) != 1 || len(input[0])%format.Channels != 0 {
			return AudioSample{}, errors.New("interleaved audio layout does not match format")
		}
		frames = len(input[0]) / format.Channels
	}
	if frames <= 0 || frames > format.FrameSamples {
		return AudioSample{}, fmt.Errorf("audio callback has %d frames, want one to %d", frames, format.FrameSamples)
	}
	pcm := make([]byte, frames*format.Channels*2)
	for frame := 0; frame < frames; frame++ {
		for channel := 0; channel < format.Channels; channel++ {
			var value float32
			if planar {
				value = input[channel][frame]
			} else {
				value = input[0][frame*format.Channels+channel]
			}
			sample := float32ToPCM16(value)
			offset := (frame*format.Channels + channel) * 2
			pcm[offset], pcm[offset+1] = byte(sample), byte(uint16(sample)>>8)
		}
	}
	return AudioSample{CaptureMonoNS: captureMonoNS, PCM16: pcm, Frames: frames, Format: format}, nil
}

func float32ToPCM16(value float32) int16 {
	if value >= 1 {
		return math.MaxInt16
	}
	if value <= -1 {
		return math.MinInt16
	}
	return int16(math.Round(float64(value) * math.MaxInt16))
}

type audioSampleQueue struct {
	mu     sync.Mutex
	sample AudioSample
	has    bool
	closed bool
	drops  uint64
	notify chan struct{}
}

func newAudioSampleQueue() *audioSampleQueue {
	return &audioSampleQueue{notify: make(chan struct{}, 1)}
}

func (q *audioSampleQueue) Offer(sample AudioSample) (bool, error) {
	if err := ValidateAudioSample(sample); err != nil {
		return false, err
	}
	// The queue is the native-to-transport handoff: callbacks may aggregate
	// locally, but only complete five-millisecond transport packets enter it.
	if sample.Frames != DefaultAudioFrameSamples {
		return false, fmt.Errorf("audio transport queue requires %d-frame samples", DefaultAudioFrameSamples)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false, errors.New("audio sample queue is closed")
	}
	replaced := q.has
	if replaced {
		q.drops += uint64(q.sample.Frames)
	}
	q.sample = sample.Clone()
	q.has = true
	select {
	case q.notify <- struct{}{}:
	default:
	}
	return replaced, nil
}

func (q *audioSampleQueue) Next(ctx context.Context) (AudioSample, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		q.mu.Lock()
		if q.has {
			sample := q.sample.Clone()
			q.sample = AudioSample{}
			q.has = false
			q.mu.Unlock()
			return sample, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return AudioSample{}, false
		}
		select {
		case <-ctx.Done():
			return AudioSample{}, false
		case <-q.notify:
		}
	}
}

func (q *audioSampleQueue) Close() {
	q.mu.Lock()
	q.closed = true
	select {
	case q.notify <- struct{}{}:
	default:
	}
	q.mu.Unlock()
}

func (q *audioSampleQueue) Drops() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.drops
}
