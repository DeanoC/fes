package remotemedia

import "testing"

func TestRTPClockMapsMonotonicCapturePTSTo90kHz(t *testing.T) {
	clock := NewRTPClock(0xfffffff0)
	got, err := clock.Timestamp(10_000_000_000)
	if err != nil {
		t.Fatalf("first timestamp: %v", err)
	}
	if got != 0xfffffff0 {
		t.Fatalf("first timestamp = %#x, want %#x", got, uint32(0xfffffff0))
	}
	got, err = clock.Timestamp(10_011_111_111)
	if err != nil {
		t.Fatalf("second timestamp: %v", err)
	}
	want := uint32(0x3d8)
	if got != want {
		t.Fatalf("second timestamp = %#x, want %#x", got, want)
	}
	if _, err := clock.Timestamp(10_011_111_110); err == nil {
		t.Fatal("backwards capture PTS accepted")
	}
}

func TestRTPClockHandlesLongIntervalsWithoutWallClock(t *testing.T) {
	clock := NewRTPClock(42)
	if _, err := clock.Timestamp(0); err != nil {
		t.Fatalf("first timestamp: %v", err)
	}
	got, err := clock.Timestamp(2_000_000_000)
	if err != nil {
		t.Fatalf("second timestamp: %v", err)
	}
	if got != 180042 {
		t.Fatalf("timestamp = %d, want 180042", got)
	}
}

func TestRTPClockRejectsRepeatedCapturePTS(t *testing.T) {
	clock := NewRTPClock(42)
	if _, err := clock.Timestamp(1_000_000_000); err != nil {
		t.Fatalf("first timestamp: %v", err)
	}
	if _, err := clock.Timestamp(1_000_000_000); err == nil {
		t.Fatal("repeated capture PTS accepted")
	}
}
