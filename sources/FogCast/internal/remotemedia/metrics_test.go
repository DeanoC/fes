package remotemedia

import (
	"testing"
	"time"
)

func TestMetricsSnapshotReportsFormatRateBitrateTimingAndDrops(t *testing.T) {
	metrics := NewMetrics(time.Unix(100, 0))
	metrics.SetSourceFormat(1280, 720, FrameRate{Numerator: 60, Denominator: 1})
	metrics.RecordEncodedFrame(2_000)
	metrics.RecordEncodedFrame(3_000)
	metrics.RecordEncodeDuration(2 * time.Millisecond)
	metrics.RecordEncodeDuration(4 * time.Millisecond)
	metrics.RecordPacket(2)
	metrics.RecordKeyframe()
	metrics.RecordCaptureDrop()
	metrics.RecordQueueDrop()
	metrics.RecordEncodeError()
	metrics.RecordPacketizationError()
	metrics.RecordUDPSendError()

	snapshot := metrics.Snapshot(time.Unix(101, 0), 1, 1)
	if snapshot.Resolution.Width != 1280 || snapshot.Resolution.Height != 720 {
		t.Fatalf("resolution = %#v", snapshot.Resolution)
	}
	if snapshot.EncodedFrames != 2 || snapshot.Keyframes != 1 || snapshot.EncodedBytes != 5_000 {
		t.Fatalf("frame counters = %#v", snapshot)
	}
	if snapshot.EncodedFPS != 2 || snapshot.BitrateBPS != 40_000 {
		t.Fatalf("rate counters = %#v", snapshot)
	}
	if snapshot.EncodeTiming.P95Millis != 4 || snapshot.EncodeTiming.P50Millis != 2 {
		t.Fatalf("timing = %#v", snapshot.EncodeTiming)
	}
	if snapshot.Drops.Capture != 1 || snapshot.Drops.Queue != 1 || snapshot.Errors.Encode != 1 || snapshot.Errors.Packetization != 1 || snapshot.Errors.UDPSend != 1 {
		t.Fatalf("drop/error counters = %#v/%#v", snapshot.Drops, snapshot.Errors)
	}
}

func TestMetricsApplyCaptureStatsKeepsCaptureFactsInSnapshot(t *testing.T) {
	metrics := NewMetrics(time.Unix(100, 0))
	metrics.ApplyCaptureStats(CaptureStats{Width: 1920, Height: 1080, FPS: FrameRate{Numerator: 60000, Denominator: 1001}, DroppedFrames: 3, PacketQueueDrops: 2, EncodeErrors: 2, QueueDepth: 1, QueueHighWater: 1, RuntimeError: "device stopped"})
	snapshot := metrics.Snapshot(time.Unix(101, 0), 0, 0)
	if snapshot.Resolution != (Resolution{Width: 1920, Height: 1080}) || snapshot.SourceFPS < 59.9 || snapshot.SourceFPS > 60.0 {
		t.Fatalf("source facts = %#v", snapshot)
	}
	if snapshot.Drops.Capture != 3 || snapshot.Drops.Queue != 0 || snapshot.Drops.PacketQueue != 2 || snapshot.Errors.Encode != 2 || snapshot.Errors.Capture != 1 || snapshot.RuntimeError != "device stopped" {
		t.Fatalf("capture facts = %#v", snapshot)
	}
}

func TestMetricsSourceFPSUsesDetectedFormatWhenFramesAreDropped(t *testing.T) {
	metrics := NewMetrics(time.Unix(100, 0))
	metrics.SetSourceFormat(1280, 720, FrameRate{Numerator: 60, Denominator: 1})
	metrics.RecordCapture(1_000_000_000)
	metrics.RecordCapture(1_100_000_000)

	snapshot := metrics.Snapshot(time.Unix(101, 0), 0, 0)
	if snapshot.SourceFPS != 60 {
		t.Fatalf("source FPS = %v, want detected 60 fps despite dropped samples", snapshot.SourceFPS)
	}
}
