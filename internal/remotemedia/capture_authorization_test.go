package remotemedia

import (
	"strings"
	"testing"
)

func TestCaptureAuthorizationErrorForUnresolvedStatuses(t *testing.T) {
	for _, tc := range []struct {
		status CaptureAuthorizationStatus
		want   string
	}{
		{captureAuthorizationNotDetermined, "not determined"},
		{captureAuthorizationRestricted, "restricted"},
		{captureAuthorizationDenied, "denied"},
	} {
		err := captureAuthorizationError(tc.status)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Camera") {
			t.Fatalf("status %v error = %v", tc.status, err)
		}
	}
}

func TestCaptureAuthorizationErrorAcceptsAuthorized(t *testing.T) {
	if err := captureAuthorizationError(captureAuthorizationAuthorized); err != nil {
		t.Fatalf("authorized status error = %v", err)
	}
}
