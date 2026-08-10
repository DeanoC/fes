//go:build darwin && cgo

package remotemedia

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework VideoToolbox -framework Foundation -framework CoreGraphics
#include "capture_darwin.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

type NativeCapture struct {
	mu     sync.Mutex
	handle unsafe.Pointer
	closed bool
}

const nativeCaptureStartupTimeout = 2 * time.Second

func nativeCaptureAuthorizationStatus() (CaptureAuthorizationStatus, error) {
	status := CaptureAuthorizationStatus(C.mr_capture_video_authorization_status())
	switch status {
	case captureAuthorizationNotDetermined,
		captureAuthorizationRestricted,
		captureAuthorizationDenied,
		captureAuthorizationAuthorized:
		return status, nil
	default:
		return status, fmt.Errorf("AVFoundation returned unknown Camera authorization status %d", status)
	}
}

func nativeCaptureRequestAuthorization() (CaptureAuthorizationStatus, error) {
	var errorOut *C.char
	status := CaptureAuthorizationStatus(C.mr_capture_request_video_authorization(&errorOut))
	if errorOut != nil {
		return status, nativeError(errorOut, "request Camera authorization")
	}
	switch status {
	case captureAuthorizationNotDetermined,
		captureAuthorizationRestricted,
		captureAuthorizationDenied,
		captureAuthorizationAuthorized:
		return status, nil
	default:
		return status, fmt.Errorf("AVFoundation returned unknown Camera authorization status %d after request", status)
	}
}

func requireNativeCaptureAuthorization() error {
	status, err := nativeCaptureAuthorizationStatus()
	if err != nil {
		return err
	}
	if captureAuthorizationNeedsPrompt(status) {
		status, err = nativeCaptureRequestAuthorization()
		if err != nil {
			return err
		}
	}
	return captureAuthorizationError(status)
}

func ListCaptureDevices() ([]CaptureDevice, error) {
	if err := requireNativeCaptureAuthorization(); err != nil {
		return nil, err
	}
	value := C.mr_capture_list_devices()
	if value == nil {
		return nil, errors.New("AVFoundation did not return a capture-device list")
	}
	defer C.mr_capture_free_string(value)
	var devices []CaptureDevice
	if err := json.Unmarshal([]byte(C.GoString(value)), &devices); err != nil {
		return nil, fmt.Errorf("decode capture-device list: %w", err)
	}
	return devices, nil
}

func OpenNativeCapture(config CaptureConfig) (*NativeCapture, error) {
	if err := validateCaptureConfig(config); err != nil {
		return nil, err
	}
	if !IsScreenCaptureDevice(config.Device) {
		if err := requireNativeCaptureAuthorization(); err != nil {
			return nil, err
		}
	}
	device := C.CString(config.Device)
	defer C.free(unsafe.Pointer(device))
	var errorOut *C.char
	handle := C.mr_capture_open(device, C.int(config.Width), C.int(config.Height), C.int(config.FPS.Numerator), C.int(config.FPS.Denominator), C.int(config.Bitrate), C.int(config.GOP), &errorOut)
	if handle == nil {
		operation := "open physical HDMI capture"
		if IsScreenCaptureDevice(config.Device) {
			operation = "open host screen capture"
		}
		return nil, nativeError(errorOut, operation)
	}
	return &NativeCapture{handle: handle}, nil
}

func (c *NativeCapture) Start() error {
	if c == nil {
		return errors.New("capture handle is closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.handle == nil {
		return errors.New("capture handle is closed")
	}
	var errorOut *C.char
	if C.mr_capture_start(c.handle, &errorOut) != 0 {
		return nativeError(errorOut, "start physical HDMI capture")
	}
	if C.mr_capture_wait_for_frame(c.handle, C.int(nativeCaptureStartupTimeout/time.Millisecond), &errorOut) != 0 {
		err := nativeError(errorOut, "start physical HDMI capture")
		C.mr_capture_close(c.handle)
		c.handle = nil
		c.closed = true
		return err
	}
	return nil
}

func (c *NativeCapture) Next(ctx context.Context) (EncodedSample, error) {
	if c == nil {
		return EncodedSample{}, errors.New("capture handle is closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			return EncodedSample{}, ctx.Err()
		default:
		}

		c.mu.Lock()
		if c.closed || c.handle == nil {
			c.mu.Unlock()
			return EncodedSample{}, errors.New("capture handle is closed")
		}
		var native C.MRNativeSample
		var errorOut *C.char
		result := C.mr_capture_next(c.handle, &native, C.int(250), &errorOut)
		if result < 0 {
			err := nativeError(errorOut, "read encoded capture sample")
			c.mu.Unlock()
			return EncodedSample{}, err
		}
		if result > 0 {
			if errorOut != nil {
				err := nativeError(errorOut, "read encoded capture sample")
				c.mu.Unlock()
				return EncodedSample{}, err
			}
			c.mu.Unlock()
			continue
		}
		sample := EncodedSample{
			CaptureMonoNS:  int64(native.capture_mono_ns),
			EncodeDuration: timeDuration(native.encode_duration_ns),
			AVCC:           C.GoBytes(unsafe.Pointer(native.avcc), C.int(native.avcc_len)),
			SPS:            C.GoBytes(unsafe.Pointer(native.sps), C.int(native.sps_len)),
			PPS:            C.GoBytes(unsafe.Pointer(native.pps), C.int(native.pps_len)),
			NALLengthSize:  int(native.nal_length_size),
			Keyframe:       native.keyframe != 0,
			Width:          int(native.width),
			Height:         int(native.height),
		}
		C.mr_capture_sample_free(&native)
		c.mu.Unlock()
		return sample, nil
	}
}

func (c *NativeCapture) Stats() CaptureStats {
	if c == nil {
		return CaptureStats{RuntimeError: "capture handle is closed"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.handle == nil {
		return CaptureStats{RuntimeError: "capture handle is closed"}
	}
	var native C.MRNativeStats
	C.mr_capture_stats(c.handle, &native)
	stats := CaptureStats{
		Width:            int(native.width),
		Height:           int(native.height),
		FPS:              FrameRate{Numerator: int(native.fps_numerator), Denominator: int(native.fps_denominator)},
		CapturedFrames:   uint64(native.captured_frames),
		DroppedFrames:    uint64(native.dropped_frames),
		EncodedFrames:    uint64(native.encoded_frames),
		EncodeErrors:     uint64(native.encode_errors),
		PacketQueueDrops: uint64(native.packet_queue_drops),
		QueueDepth:       int(native.queue_depth),
		QueueHighWater:   int(native.queue_high_water),
	}
	if native.runtime_error != nil {
		stats.RuntimeError = C.GoString(native.runtime_error)
		C.free(unsafe.Pointer(native.runtime_error))
	}
	return stats
}

func (c *NativeCapture) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.handle == nil {
		c.closed = true
		return nil
	}
	C.mr_capture_close(c.handle)
	c.handle = nil
	c.closed = true
	return nil
}

func (c *NativeCapture) RequestKeyframe() error {
	return errors.New("VideoToolbox adapter uses periodic IDR; runtime force-IDR is not supported by this spike")
}

func nativeError(errorOut *C.char, operation string) error {
	if errorOut == nil {
		return errors.New(operation + " failed")
	}
	defer C.free(unsafe.Pointer(errorOut))
	return fmt.Errorf("%s: %s", operation, C.GoString(errorOut))
}

func timeDuration(ns C.int64_t) time.Duration {
	return time.Duration(int64(ns))
}
