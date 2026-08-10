package remotemedia

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	AudioEncodingPCM16LE = "pcm_s16le"
	AudioSampleRate      = 48_000
)

type AudioFormat struct {
	SampleRate   int    `json:"sample_rate"`
	Channels     int    `json:"channels"`
	Encoding     string `json:"encoding"`
	FrameSamples int    `json:"frame_samples"`
}

func ValidateAudioFormat(format AudioFormat) error {
	if format.SampleRate != AudioSampleRate {
		return fmt.Errorf("audio sample rate must be %d Hz", AudioSampleRate)
	}
	if format.Channels < 1 || format.Channels > 2 {
		return errors.New("audio channels must be one or two")
	}
	if format.Encoding != AudioEncodingPCM16LE {
		return fmt.Errorf("audio encoding must be %q", AudioEncodingPCM16LE)
	}
	if format.FrameSamples <= 0 {
		return errors.New("audio frame samples must be positive")
	}
	return nil
}

type AudioSample struct {
	CaptureMonoNS int64       `json:"capture_mono_ns"`
	PCM16         []byte      `json:"-"`
	Frames        int         `json:"frames"`
	Format        AudioFormat `json:"format"`
}

func (sample AudioSample) Clone() AudioSample {
	sample.PCM16 = append([]byte(nil), sample.PCM16...)
	return sample
}

func ValidateAudioSample(sample AudioSample) error {
	if sample.CaptureMonoNS < 0 {
		return errors.New("audio capture timestamp cannot be negative")
	}
	if err := ValidateAudioFormat(sample.Format); err != nil {
		return err
	}
	if sample.Frames <= 0 {
		return errors.New("audio frames must be positive")
	}
	return validatePCM16(sample.PCM16, sample.Frames, sample.Format.Channels)
}

type AudioFrame struct {
	Sequence       uint16      `json:"sequence"`
	Timestamp      uint32      `json:"timestamp"`
	ReceiverMonoNS int64       `json:"receiver_mono_ns"`
	PCM16          []byte      `json:"-"`
	Frames         int         `json:"frames"`
	Format         AudioFormat `json:"format"`
}

func (frame AudioFrame) Clone() AudioFrame {
	frame.PCM16 = append([]byte(nil), frame.PCM16...)
	return frame
}

func ValidateAudioFrame(frame AudioFrame) error {
	if frame.ReceiverMonoNS < 0 {
		return errors.New("audio receiver timestamp cannot be negative")
	}
	if err := ValidateAudioFormat(frame.Format); err != nil {
		return err
	}
	if frame.Frames <= 0 {
		return errors.New("audio frames must be positive")
	}
	return validatePCM16(frame.PCM16, frame.Frames, frame.Format.Channels)
}

func validatePCM16(pcm []byte, frames, channels int) error {
	if channels <= 0 || frames <= 0 {
		return errors.New("PCM16 layout must have positive frames and channels")
	}
	bytesPerFrame := channels * 2
	if frames > len(pcm)/bytesPerFrame || len(pcm)/bytesPerFrame != frames || len(pcm)%bytesPerFrame != 0 {
		return fmt.Errorf("PCM16 byte length %d does not match %d frames and %d channels", len(pcm), frames, channels)
	}
	return nil
}

type AudioStats struct {
	SourceFormat   AudioFormat           `json:"source_format"`
	Callbacks      uint64                `json:"callbacks"`
	NonZeroSamples uint64                `json:"non_zero_samples"`
	DroppedFrames  uint64                `json:"dropped_frames"`
	QueueDepth     int                   `json:"queue_depth"`
	QueueHighWater int                   `json:"queue_high_water"`
	RuntimeError   AudioRuntimeErrorCode `json:"runtime_error,omitempty"`
	Shutdown       bool                  `json:"shutdown"`
}

type AudioRuntimeErrorCode string

const (
	AudioRuntimeErrorNone              AudioRuntimeErrorCode = ""
	AudioRuntimeErrorSourceUnavailable AudioRuntimeErrorCode = "source_unavailable"
	AudioRuntimeErrorCallbackFailed    AudioRuntimeErrorCode = "callback_failed"
	AudioRuntimeErrorSinkUnavailable   AudioRuntimeErrorCode = "sink_unavailable"
	AudioRuntimeErrorSinkWriteFailed   AudioRuntimeErrorCode = "sink_write_failed"
	AudioRuntimeErrorClosed            AudioRuntimeErrorCode = "closed"
)

func CountNonZeroPCM16(pcm []byte) (uint64, error) {
	if len(pcm)%2 != 0 {
		return 0, errors.New("PCM16 byte length must be even")
	}
	var count uint64
	for index := 0; index < len(pcm); index += 2 {
		if pcm[index] != 0 || pcm[index+1] != 0 {
			count++
		}
	}
	return count, nil
}

func (stats *AudioStats) RecordPCM16(pcm []byte) error {
	if stats == nil {
		return errors.New("audio stats are nil")
	}
	count, err := CountNonZeroPCM16(pcm)
	if err != nil {
		return err
	}
	stats.Callbacks++
	stats.NonZeroSamples += count
	return nil
}

func (stats *AudioStats) RecordRuntimeError(code AudioRuntimeErrorCode) {
	if stats != nil {
		stats.RuntimeError = code
	}
}

type AudioSource interface {
	Start() error
	Next(context.Context) (AudioSample, error)
	Stats() AudioStats
	Close() error
}

type AudioSourceKind string

const (
	AudioSourceShadowCastUAC AudioSourceKind = "shadowcast_uac"
	AudioSourceHostOutput    AudioSourceKind = "host_output"
)

type AudioEndpoint struct {
	UID         string
	DisplayName string
	SampleRate  int
	Channels    int
}

type AudioDisplayIdentity struct {
	HardwareUUID      string
	EDIDVendor        string
	EDIDModel         string
	EDIDSerial        string
	ExplicitSelection bool
	SelectionContext  string
}

// IsConfigurationFingerprint reports whether the display lacks persistent
// identity material and its digest is therefore selection-specific.
func (identity AudioDisplayIdentity) IsConfigurationFingerprint() bool {
	return norm.NFC.String(identity.HardwareUUID) == "" && norm.NFC.String(identity.EDIDSerial) == ""
}

func (identity AudioDisplayIdentity) RequiresExplicitOperatorSelection() bool {
	return identity.IsConfigurationFingerprint() && (!identity.ExplicitSelection || strings.TrimSpace(identity.SelectionContext) == "")
}

type AudioSourceConfig struct {
	Kind           AudioSourceKind
	EndpointName   string
	EndpointUID    string
	EndpointDigest string
	Display        AudioDisplayIdentity
	DisplayDigest  string
	SampleRate     int
	Channels       int
	FrameSamples   int
	Enabled        bool
}

type AudioTransportConfig struct {
	RTPDestination          string
	ControlAddress          string
	SSRC                    uint32
	MTU                     int
	FormatCapabilityVersion uint32
}

type AudioConfig struct {
	Enabled   bool
	Source    AudioSourceConfig
	Transport AudioTransportConfig
}

type AudioSourceFactory func(AudioSourceConfig) (AudioSource, error)

func ValidateAudioConfig(config AudioConfig) error {
	if !config.Enabled {
		return nil
	}
	if !config.Source.Enabled {
		return errors.New("enabled audio config requires an enabled source")
	}
	if config.Source.Kind != AudioSourceShadowCastUAC && config.Source.Kind != AudioSourceHostOutput {
		return fmt.Errorf("unknown audio source kind %q", config.Source.Kind)
	}
	if err := ValidateAudioFormat(AudioFormat{SampleRate: config.Source.SampleRate, Channels: config.Source.Channels, Encoding: AudioEncodingPCM16LE, FrameSamples: config.Source.FrameSamples}); err != nil {
		return err
	}
	switch config.Source.Kind {
	case AudioSourceShadowCastUAC:
		if config.Source.EndpointUID == "" || config.Source.EndpointName == "" || config.Source.EndpointDigest == "" {
			return errors.New("shadowcast UAC source endpoint identity is required")
		}
		endpoint := AudioEndpoint{UID: config.Source.EndpointUID, DisplayName: config.Source.EndpointName, SampleRate: config.Source.SampleRate, Channels: config.Source.Channels}
		digest, err := CanonicalAudioEndpointDigest(endpoint)
		if err != nil {
			return err
		}
		if config.Source.EndpointDigest != digest {
			return errors.New("audio source endpoint digest does not match its identity")
		}
	case AudioSourceHostOutput:
		if config.Source.DisplayDigest == "" {
			return errors.New("host output source display digest is required")
		}
		digest, err := CanonicalAudioDisplayDigest(config.Source.Display)
		if err != nil {
			return err
		}
		if config.Source.DisplayDigest != digest {
			return errors.New("host output source display digest does not match its identity")
		}
	}
	return nil
}

func CanonicalAudioEndpointDigest(endpoint AudioEndpoint) (string, error) {
	if endpoint.UID == "" || endpoint.DisplayName == "" {
		return "", errors.New("audio endpoint UID and display name are required")
	}
	if err := ValidateAudioFormat(AudioFormat{SampleRate: endpoint.SampleRate, Channels: endpoint.Channels, Encoding: AudioEncodingPCM16LE, FrameSamples: 1}); err != nil {
		return "", err
	}
	encoded := make([]byte, 0, 128)
	encoded = append(encoded, "fogcast-audio-endpoint-v1\x00"...)
	encoded = appendLengthPrefixed(encoded, norm.NFC.String(endpoint.UID))
	encoded = appendLengthPrefixed(encoded, collapseWhitespace(norm.NFC.String(endpoint.DisplayName)))
	encoded = appendUint32(encoded, uint32(endpoint.SampleRate))
	encoded = appendUint32(encoded, uint32(endpoint.Channels))
	encoded = appendLengthPrefixed(encoded, AudioEncodingPCM16LE)
	return sha256Digest(encoded), nil
}

func CanonicalAudioDisplayDigest(identity AudioDisplayIdentity) (string, error) {
	if identity.RequiresExplicitOperatorSelection() {
		return "", errors.New("display without persistent identity requires explicit operator selection")
	}
	encoded := make([]byte, 0, 128)
	encoded = append(encoded, "fogcast-display-v1\x00"...)
	encoded = appendLengthPrefixed(encoded, norm.NFC.String(identity.HardwareUUID))
	encoded = appendLengthPrefixed(encoded, norm.NFC.String(identity.EDIDVendor))
	encoded = appendLengthPrefixed(encoded, norm.NFC.String(identity.EDIDModel))
	encoded = appendLengthPrefixed(encoded, norm.NFC.String(identity.EDIDSerial))
	return sha256Digest(encoded), nil
}

func appendLengthPrefixed(dst []byte, value string) []byte {
	dst = appendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

func appendUint32(dst []byte, value uint32) []byte {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], value)
	return append(dst, raw[:]...)
}

func collapseWhitespace(value string) string { return strings.Join(strings.Fields(value), " ") }

func sha256Digest(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("sha256:%x", digest[:])
}
