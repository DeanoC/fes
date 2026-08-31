package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/remotemedia"
)

func TestCommandUsesCanonicalName(t *testing.T) {
	if commandName != "remote-play-sender" {
		t.Fatalf("commandName = %q, want remote-play-sender", commandName)
	}
}

func TestSenderRequiresPhysicalCaptureDevice(t *testing.T) {
	var stderr bytes.Buffer
	err := runSender(context.Background(), nil, &bytes.Buffer{}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "physical HDMI capture device") {
		t.Fatalf("error = %v, want physical capture requirement", err)
	}
}

func TestParseFrameRatePreservesRationalRate(t *testing.T) {
	rate, err := parseFrameRate("60000/1001")
	if err != nil {
		t.Fatalf("parse FPS: %v", err)
	}
	if rate.Numerator != 60000 || rate.Denominator != 1001 {
		t.Fatalf("rate = %#v", rate)
	}
}

func TestProbeMarksSyntheticFramesFalseAndWritesReport(t *testing.T) {
	var stdout bytes.Buffer
	if err := runProbe(nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("probe: %v", err)
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report["synthetic_frames"] != false || report["physical_capture_required"] != true {
		t.Fatalf("probe report = %#v", report)
	}
}

func TestRequireCaptureDevicesRejectsEmptyPhysicalDeviceList(t *testing.T) {
	if err := requireCaptureDevices(nil); err == nil {
		t.Fatal("empty physical capture-device list was accepted")
	}
}

func TestRequireCaptureDevicesAcceptsDetectedDevice(t *testing.T) {
	if err := requireCaptureDevices([]remotemedia.CaptureDevice{{Name: "USB HDMI"}}); err != nil {
		t.Fatalf("detected capture device rejected: %v", err)
	}
}

func TestCaptureDeviceInventoryIsSkippedForScreenCapture(t *testing.T) {
	if shouldEnumerateCaptureDevices("screen") {
		t.Fatal("screen capture should not require an external UVC inventory")
	}
	if !shouldEnumerateCaptureDevices("ShadowCast 3") {
		t.Fatal("physical capture should require an external UVC inventory")
	}
}
