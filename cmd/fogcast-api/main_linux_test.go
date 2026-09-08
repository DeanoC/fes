//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
)

func TestDefaultCaptureSourceDelegatesToLinuxAdapter(t *testing.T) {
	_, err := defaultCaptureSource(fogcast.MediaConfig{Enabled: true, CaptureDevice: "ShadowCast 3"})
	if err == nil || strings.Contains(err.Error(), "unavailable on this platform") {
		t.Fatalf("error = %v, want the platform adapter to validate the device", err)
	}
}
