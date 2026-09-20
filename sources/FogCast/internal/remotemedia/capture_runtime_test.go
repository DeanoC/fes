//go:build darwin && cgo && remote_play_hardware

package remotemedia

import (
	"context"
	"testing"
	"time"
)

const temporaryCaptureDevice = "ShadowCast 3"

func openTemporaryCapture(t *testing.T) *NativeCapture {
	t.Helper()
	devices, err := ListCaptureDevices()
	if err != nil {
		t.Skipf("capture enumeration unavailable: %v", err)
	}
	for _, device := range devices {
		if device.Name == temporaryCaptureDevice && device.UniqueID != "" {
			capture, err := OpenNativeCapture(CaptureConfig{Device: device.UniqueID})
			if err != nil {
				t.Skipf("capture unavailable: %v", err)
			}
			return capture
		}
	}
	capture, err := OpenNativeCapture(CaptureConfig{Device: temporaryCaptureDevice})
	if err != nil {
		t.Skipf("capture unavailable: %v", err)
	}
	return capture
}

func TestTemporaryNativeOpenClose(t *testing.T) {
	capture := openTemporaryCapture(t)
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTemporaryNativeStartCancelClose(t *testing.T) {
	capture := openTemporaryCapture(t)
	if err := capture.Start(); err != nil {
		t.Skipf("capture did not start: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = capture.Next(ctx)
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTemporaryNativeCapturesEncodedSample(t *testing.T) {
	capture := openTemporaryCapture(t)
	defer capture.Close()
	if err := capture.Start(); err != nil {
		t.Skipf("capture did not start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sample, err := capture.Next(ctx)
	if err != nil {
		t.Skipf("no HDMI sample available: %v", err)
	}
	if len(sample.AVCC) == 0 || sample.NALLengthSize == 0 || sample.Width <= 0 || sample.Height <= 0 {
		t.Fatalf("encoded sample = %#v", sample)
	}
}
