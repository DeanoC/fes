//go:build darwin && cgo

package remotemedia

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=11.0
#cgo darwin LDFLAGS: -framework AVFoundation -framework AudioToolbox -Wl,-weak_framework,ScreenCaptureKit -framework CoreGraphics -framework ApplicationServices -framework Foundation
#include "audio_darwin.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// OpenNativeAudioSource opens only the explicitly configured source. It never
// chooses a default CoreAudio device or a default/main display.
func OpenNativeAudioSource(config AudioSourceConfig) (AudioSource, error) {
	if err := validateNativeAudioSourceConfig(config); err != nil {
		return nil, err
	}
	permission := AudioPermissionMicrophone
	if config.Kind == AudioSourceHostOutput {
		if C.mr_audio_host_output_available() == 0 {
			return nil, errors.New("host_output is unavailable on macOS versions earlier than 13")
		}
		permission = AudioPermissionScreenRecording
	}
	status := C.int(0)
	if config.Kind == AudioSourceShadowCastUAC {
		status = C.mr_audio_microphone_authorization_status()
	} else {
		status = C.mr_audio_screen_authorization_status()
	}
	authorization := AudioAuthorizationStatus(status)
	if authorization == AudioAuthorizationNotDetermined {
		var detail *C.char
		if config.Kind == AudioSourceShadowCastUAC {
			authorization = AudioAuthorizationStatus(C.mr_audio_request_microphone_authorization(&detail))
		} else {
			authorization = AudioAuthorizationStatus(C.mr_audio_request_screen_authorization(&detail))
		}
		if detail != nil {
			if authorization != AudioAuthorizationAuthorized {
				return nil, nativeAudioError(detail, "request "+permission.String()+" authorization")
			}
			C.free(unsafe.Pointer(detail))
		}
	}
	if err := AudioAuthorizationError(permission, authorization); err != nil {
		return nil, err
	}

	nativeConfig := C.MRNativeAudioConfig{
		kind:          C.int(nativeAudioKind(config.Kind)),
		sample_rate:   C.int(config.SampleRate),
		channels:      C.int(config.Channels),
		frame_samples: C.int(config.FrameSamples),
	}
	strings := []*C.char{
		C.CString(config.EndpointUID), C.CString(config.EndpointName),
		C.CString(config.Display.HardwareUUID), C.CString(config.Display.EDIDVendor),
		C.CString(config.Display.EDIDModel), C.CString(config.Display.EDIDSerial),
		C.CString(config.DisplayDigest),
	}
	defer func() {
		for _, value := range strings {
			C.free(unsafe.Pointer(value))
		}
	}()
	nativeConfig.endpoint_uid = strings[0]
	nativeConfig.endpoint_name = strings[1]
	nativeConfig.display_hardware_uuid = strings[2]
	nativeConfig.display_vendor = strings[3]
	nativeConfig.display_model = strings[4]
	nativeConfig.display_serial = strings[5]
	nativeConfig.display_digest = strings[6]
	var detail *C.char
	handle := C.mr_audio_open(&nativeConfig, &detail)
	if handle == nil {
		return nil, nativeAudioError(detail, "open native audio source")
	}
	source := &nativeAudioSource{
		handle: handle,
		format: AudioFormat{SampleRate: config.SampleRate, Channels: config.Channels, Encoding: AudioEncodingPCM16LE, FrameSamples: config.FrameSamples},
	}
	return source, nil
}

func nativeAudioKind(kind AudioSourceKind) int {
	if kind == AudioSourceHostOutput {
		return int(C.MR_AUDIO_KIND_HOST_OUTPUT)
	}
	return int(C.MR_AUDIO_KIND_SHADOWCAST_UAC)
}

func nativeAudioError(detail *C.char, operation string) error {
	if detail == nil {
		return errors.New(operation + " failed")
	}
	defer C.free(unsafe.Pointer(detail))
	return fmt.Errorf("%s: %s", operation, C.GoString(detail))
}

type nativeAudioSource struct {
	mu        sync.Mutex
	lifecycle sync.Mutex
	handle    unsafe.Pointer
	format    AudioFormat
	closed    bool
}

func (s *nativeAudioSource) Start() error {
	if s == nil {
		return errors.New("native audio source is unavailable")
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.closed || s.handle == nil {
		s.mu.Unlock()
		return errors.New("native audio source is closed")
	}
	handle := s.handle
	s.mu.Unlock()
	var detail *C.char
	if C.mr_audio_start(handle, &detail) != 0 {
		err := nativeAudioError(detail, "start native audio source")
		var closeDetail *C.char
		if C.mr_audio_close(handle, &closeDetail) != 0 {
			err = errors.Join(err, nativeAudioError(closeDetail, "close native audio source after start failure"))
		} else {
			s.mu.Lock()
			s.handle = nil
			s.closed = true
			s.mu.Unlock()
		}
		return err
	}
	return nil
}

func (s *nativeAudioSource) Next(ctx context.Context) (AudioSample, error) {
	if s == nil {
		return AudioSample{}, errors.New("native audio source is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return AudioSample{}, err
		}
		s.lifecycle.Lock()
		s.mu.Lock()
		if s.closed || s.handle == nil {
			s.mu.Unlock()
			s.lifecycle.Unlock()
			return AudioSample{}, errors.New("native audio source is closed")
		}
		handle := s.handle
		s.mu.Unlock()
		timeout := 250 * time.Millisecond
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
			timeout = time.Until(deadline)
			if timeout <= 0 {
				s.lifecycle.Unlock()
				return AudioSample{}, ctx.Err()
			}
		}
		var native C.MRNativeAudioSample
		var detail *C.char
		result := C.mr_audio_next(handle, &native, C.int(timeout/time.Millisecond), &detail)
		if result > 0 {
			s.lifecycle.Unlock()
			continue
		}
		if result < 0 {
			s.lifecycle.Unlock()
			return AudioSample{}, nativeAudioError(detail, "read native audio sample")
		}
		captureMonoNS := int64(native.capture_mono_ns)
		frames := int(native.frames)
		pcm := C.GoBytes(unsafe.Pointer(native.pcm16), C.int(native.pcm16_len))
		C.mr_audio_sample_free(&native)
		s.lifecycle.Unlock()
		return AudioSample{CaptureMonoNS: captureMonoNS, PCM16: pcm, Frames: frames, Format: s.format}, nil
	}
}

func (s *nativeAudioSource) Stats() AudioStats {
	if s == nil {
		return AudioStats{RuntimeError: AudioRuntimeErrorSourceUnavailable}
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.handle == nil {
		return AudioStats{SourceFormat: s.format, RuntimeError: AudioRuntimeErrorClosed, Shutdown: true}
	}
	var native C.MRNativeAudioStats
	C.mr_audio_stats(s.handle, &native)
	stats := AudioStats{
		SourceFormat:   s.format,
		Callbacks:      uint64(native.callbacks),
		NonZeroSamples: uint64(native.non_zero_samples),
		DroppedFrames:  uint64(native.dropped_frames),
		QueueDepth:     int(native.queue_depth),
		QueueHighWater: int(native.queue_high_water),
	}
	if native.runtime_error != nil {
		stats.RuntimeError = AudioRuntimeErrorCallbackFailed
		C.free(unsafe.Pointer(native.runtime_error))
	}
	return stats
}

func (s *nativeAudioSource) Close() error {
	if s == nil {
		return nil
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.closed || s.handle == nil {
		s.closed = true
		s.mu.Unlock()
		return nil
	}
	handle := s.handle
	s.mu.Unlock()
	var detail *C.char
	if C.mr_audio_close(handle, &detail) != 0 {
		return nativeAudioError(detail, "close native audio source")
	}
	s.mu.Lock()
	s.handle = nil
	s.closed = true
	s.mu.Unlock()
	return nil
}
