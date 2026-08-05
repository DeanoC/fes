//go:build darwin && cgo

package remotemedia

import (
	"strings"
	"testing"
)

func TestOpenNativeCaptureRejectsUnknownPhysicalDeviceClearly(t *testing.T) {
	_, err := OpenNativeCapture(CaptureConfig{Device: "__fogcast_nonexistent_hdmi_capture_device__"})
	if err == nil {
		t.Fatal("unknown physical capture device was accepted")
	}
	if !strings.Contains(err.Error(), "physical HDMI capture device") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown-device error = %q", err)
	}
}
