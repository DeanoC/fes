//go:build darwin && cgo

package remotemedia

import "testing"

func TestNativeCaptureAuthorizationStatusIsNormalized(t *testing.T) {
	status, err := nativeCaptureAuthorizationStatus()
	if err != nil {
		t.Fatalf("authorization status: %v", err)
	}
	switch status {
	case captureAuthorizationNotDetermined,
		captureAuthorizationRestricted,
		captureAuthorizationDenied,
		captureAuthorizationAuthorized:
	default:
		t.Fatalf("authorization status = %d, want a normalized status", status)
	}
}
