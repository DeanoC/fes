package remotemedia

import "testing"

func TestValidateCaptureConfigRejectsInvalidRates(t *testing.T) {
	for _, config := range []CaptureConfig{
		{FPS: FrameRate{Numerator: 60, Denominator: 0}},
		{FPS: FrameRate{Numerator: -1, Denominator: 1}},
		{Width: -1},
		{Bitrate: -1},
	} {
		if err := validateCaptureConfig(config); err == nil {
			t.Fatalf("invalid capture config accepted: %#v", config)
		}
	}
}

func TestValidateCaptureConfigAllowsUnsetAutoDetectFields(t *testing.T) {
	if err := validateCaptureConfig(CaptureConfig{}); err != nil {
		t.Fatalf("unset capture config rejected: %v", err)
	}
}

func TestValidateCaptureConfigRejectsPartiallySetFrameRates(t *testing.T) {
	for _, rate := range []FrameRate{
		{Numerator: 0, Denominator: 1},
		{Numerator: 60, Denominator: 0},
	} {
		if err := validateCaptureConfig(CaptureConfig{FPS: rate}); err == nil {
			t.Fatalf("partially set capture rate accepted: %#v", rate)
		}
	}
}

func TestIsScreenCaptureDeviceRecognizesScreenSelector(t *testing.T) {
	for _, test := range []struct {
		device string
		want   bool
	}{
		{device: "screen", want: true},
		{device: "screen:0", want: true},
		{device: " ShadowCast 3 ", want: false},
		{device: "", want: false},
	} {
		if got := IsScreenCaptureDevice(test.device); got != test.want {
			t.Fatalf("IsScreenCaptureDevice(%q) = %t, want %t", test.device, got, test.want)
		}
	}
}
