//go:build darwin && cgo

package remotemedia

import (
	"strings"
	"testing"
)

func TestOpenNativeCaptureRejectsUnknownPhysicalDeviceClearly(t *testing.T) {
	status, err := nativeCaptureAuthorizationStatus()
	if err != nil {
		t.Fatal(err)
	}
	if authorizationErr := captureAuthorizationError(status); authorizationErr != nil {
		t.Skipf("physical-device check requires Camera authorization: %v", authorizationErr)
	}
	_, openErr := OpenNativeCapture(CaptureConfig{Device: "__fogcast_nonexistent_hdmi_capture_device__"})
	if openErr == nil {
		t.Fatal("unknown physical capture device was accepted")
	}
	if !strings.Contains(openErr.Error(), "physical HDMI capture device") || !strings.Contains(openErr.Error(), "not found") {
		t.Fatalf("unknown-device error = %q", openErr)
	}
}
