//go:build (!darwin && !linux) || (darwin && !cgo)

package remotemedia

import (
	"context"
	"errors"
)

var ErrNativeCaptureUnavailable = errors.New("native AVFoundation/VideoToolbox capture is only available in a cgo-enabled macOS build")

type NativeCapture struct{}

func ListCaptureDevices() ([]CaptureDevice, error) { return nil, ErrNativeCaptureUnavailable }

func OpenNativeCapture(CaptureConfig) (*NativeCapture, error) {
	return nil, ErrNativeCaptureUnavailable
}

func (c *NativeCapture) Start() error { return ErrNativeCaptureUnavailable }
func (c *NativeCapture) Next(context.Context) (EncodedSample, error) {
	return EncodedSample{}, ErrNativeCaptureUnavailable
}
func (c *NativeCapture) Stats() CaptureStats {
	return CaptureStats{RuntimeError: ErrNativeCaptureUnavailable.Error()}
}
func (c *NativeCapture) Close() error           { return nil }
func (c *NativeCapture) RequestKeyframe() error { return ErrNativeCaptureUnavailable }
