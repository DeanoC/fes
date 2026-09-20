package remotemedia

import "fmt"

// CaptureAuthorizationStatus is the normalized AVFoundation camera
// authorization state used by the Darwin capture adapter.
type CaptureAuthorizationStatus uint8

const (
	captureAuthorizationNotDetermined CaptureAuthorizationStatus = iota
	captureAuthorizationRestricted
	captureAuthorizationDenied
	captureAuthorizationAuthorized
)

func captureAuthorizationStatusName(status CaptureAuthorizationStatus) string {
	switch status {
	case captureAuthorizationNotDetermined:
		return "not determined"
	case captureAuthorizationRestricted:
		return "restricted"
	case captureAuthorizationDenied:
		return "denied"
	case captureAuthorizationAuthorized:
		return "authorized"
	default:
		return "unknown"
	}
}

func captureAuthorizationError(status CaptureAuthorizationStatus) error {
	if status == captureAuthorizationAuthorized {
		return nil
	}
	return fmt.Errorf("AVFoundation Camera authorization is %s; grant Camera access to the FogCast helper in System Settings > Privacy & Security > Camera", captureAuthorizationStatusName(status))
}

func captureAuthorizationNeedsPrompt(status CaptureAuthorizationStatus) bool {
	return status == captureAuthorizationNotDetermined
}
