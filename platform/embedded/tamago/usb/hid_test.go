package usb

import (
	"errors"
	"reflect"
	"testing"
)

func TestBootKeyboardEdgesAndModifierSnapshots(t *testing.T) {
	var keyboard BootKeyboard
	for _, step := range []struct {
		report [8]byte
		want   []KeyEvent
	}{
		{report: [8]byte{2, 0x5a, 4, 5}, want: []KeyEvent{{4, true, 2}, {5, true, 2}, {0xe1, true, 2}}},
		// Report byte1 is ignored; key-array order is insignificant.
		{report: [8]byte{2, 0, 5, 4}},
		{report: [8]byte{0, 0, 5, 6}, want: []KeyEvent{{4, false, 0}, {0xe1, false, 0}, {6, true, 0}}},
		{report: [8]byte{0}, want: []KeyEvent{{5, false, 0}, {6, false, 0}}},
		{report: [8]byte{0}},
	} {
		got, err := keyboard.Feed(step.report[:])
		if err != nil || (len(got) != 0 || len(step.want) != 0) && !reflect.DeepEqual(got, step.want) {
			t.Fatalf("Feed(%v) = %v, %v; want %v", step.report, got, err, step.want)
		}
	}
}

func TestBadKeyboardReportsRetainLastValidState(t *testing.T) {
	var keyboard BootKeyboard
	_, err := keyboard.Feed([]byte{1, 0, 4, 0, 0, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	for _, report := range [][]byte{
		{}, {0}, make([]byte, 7), make([]byte, 9),
		{0, 0, 4, 4, 0, 0, 0, 0},
		{0, 0, 0xe1, 0, 0, 0, 0, 0},
		{0, 0, 1, 1, 1, 1, 1, 1},
		{0, 0, 2, 0, 0, 0, 0, 0},
		{0, 0, 3, 0, 0, 0, 0, 0},
	} {
		if events, err := keyboard.Feed(report); err == nil || len(events) != 0 {
			t.Fatalf("bad report %v returned %v, %v", report, events, err)
		}
	}
	if events, err := keyboard.Feed([]byte{1, 0, 4, 0, 0, 0, 0, 0}); err != nil || len(events) != 0 {
		t.Fatalf("bad reports changed the saved state: %v, %v", events, err)
	}
	want := []KeyEvent{{4, false, 0}, {0xe0, false, 0}}
	if events := keyboard.Reset(); !reflect.DeepEqual(events, want) {
		t.Fatalf("disconnect releases = %v, want %v", events, want)
	}
	if events := keyboard.Reset(); len(events) != 0 {
		t.Fatalf("second disconnect duplicated releases: %v", events)
	}
}

func TestKeyboardErrorKinds(t *testing.T) {
	var keyboard BootKeyboard
	if _, err := keyboard.Feed([]byte{0, 0, 1, 0, 0, 0, 0, 0}); !errors.Is(err, ErrKeyboardRollover) {
		t.Fatalf("rollover error = %v", err)
	}
	if _, err := keyboard.Feed(nil); !errors.Is(err, ErrBootReport) {
		t.Fatalf("short report error = %v", err)
	}
}

func TestBootKeyboardFeedIntoZeroAllocs(t *testing.T) {
	var keyboard BootKeyboard
	var edges [28]KeyEvent
	press := []byte{0x02, 0, 4, 5, 0, 0, 0, 0}
	release := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	allocs := testing.AllocsPerRun(50, func() {
		n, err := keyboard.FeedInto(press, edges[:])
		if err != nil || n != 3 {
			t.Fatalf("press n=%d err=%v", n, err)
		}
		n, err = keyboard.FeedInto(release, edges[:])
		if err != nil || n != 3 {
			t.Fatalf("release n=%d err=%v", n, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("FeedInto allocated %.2f objects/cycle, want 0", allocs)
	}
}

func TestBootKeyboardFeedIntoUndersizedBufferPreservesState(t *testing.T) {
	var keyboard BootKeyboard
	press := []byte{0x02, 0, 4, 5, 0, 0, 0, 0} // 3 edges (Shift + A + B)
	var tiny [2]KeyEvent
	if n, err := keyboard.FeedInto(press, tiny[:]); err == nil || n != 0 {
		t.Fatalf("undersized FeedInto succeeded: n=%d err=%v", n, err)
	}
	// State must be preserved so retrying with a large enough buffer delivers all 3 edges.
	var full [28]KeyEvent
	n, err := keyboard.FeedInto(press, full[:])
	if err != nil || n != 3 {
		t.Fatalf("retry FeedInto after undersized failure: n=%d err=%v", n, err)
	}
}

func FuzzBootKeyboardReports(f *testing.F) {
	f.Add([]byte{0, 0, 4, 0, 0, 0, 0, 0})
	f.Add([]byte{0, 0, 1, 1, 1, 1, 1, 1})
	f.Fuzz(func(t *testing.T, report []byte) {
		var keyboard BootKeyboard
		_, _ = keyboard.Feed(report)
		_ = keyboard.Reset()
	})
}
