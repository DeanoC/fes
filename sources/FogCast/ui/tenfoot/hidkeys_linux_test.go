//go:build linux

package tenfoot

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestNativeHIDUsageKeepsEveryPreviouslyMappedKey(t *testing.T) {
	// The old mapping covered all letters, digits, modifiers, punctuation and
	// navigation keys. Keep every one pinned while the shared table adds keys.
	legacy := map[uint16]uint8{
		1: 0x29, 12: 0x2d, 13: 0x2e, 14: 0x2a, 15: 0x2b,
		26: 0x2f, 27: 0x30, 28: 0x28, 29: 0xe0, 39: 0x33, 40: 0x34, 41: 0x35,
		42: 0xe1, 43: 0x31, 51: 0x36, 52: 0x37, 53: 0x38, 54: 0xe5, 56: 0xe2,
		57: 0x2c, 58: 0x39, 97: 0xe4, 100: 0xe6, 102: 0x4a, 103: 0x52,
		104: 0x4b, 105: 0x50, 106: 0x4f, 107: 0x4d, 108: 0x51, 109: 0x4e,
		110: 0x49, 111: 0x4c, 125: 0xe3, 126: 0xe7,
	}
	for code := uint16(2); code <= 11; code++ {
		legacy[code] = uint8(code + 28)
	}
	letterCodes := []uint16{30, 48, 46, 32, 18, 33, 34, 35, 23, 36, 37, 38, 50, 49, 24, 25, 16, 19, 31, 20, 22, 47, 17, 45, 21, 44}
	for i, code := range letterCodes {
		legacy[code] = uint8(0x04 + i)
	}
	if len(legacy) != 71 {
		t.Fatalf("legacy mapping has %d entries, want 71", len(legacy))
	}
	for code, want := range legacy {
		if got := nativeHIDUsage(code); got != want {
			t.Errorf("legacy key %d = %#x, want %#x", code, got, want)
		}
	}
}

func TestNativeHIDUsageMapsAddedKeys(t *testing.T) {
	added := map[uint16]uint8{
		59: 0x3a, 68: 0x43, 87: 0x44, // F1, F10, F11
		69: 0x53, 79: 0x59, 82: 0x62, 83: 0x63, 96: 0x58, 98: 0x54, 55: 0x55, 74: 0x56, 78: 0x57, 117: 0x67, // keypad, Num Lock
		70: 0x47, 99: 0x46, 119: 0x48, // Scroll Lock, Print Screen, Pause
		86: 0x64, 127: 0x65, // 102nd key, Menu
	}
	for code, want := range added {
		if got := nativeHIDUsage(code); got != want || !hidkeys.Valid(got) {
			t.Errorf("key %d = %#x, want %#x", code, got, want)
		}
	}
	if got := nativeHIDUsage(0xffff); got != 0 {
		t.Fatalf("unmapped key = %#x, want 0", got)
	}
}

func TestNativeInputKeyForwardsPressesAndReleasesToKitLocalFeed(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	display, err := gfx.NewMenuDisplayWithPresenter(&countingSessionMenu{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	app.mu.Lock()
	app.session.CoreKeyboard = false
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()

	in := &nativeInput{fd: 1, kind: InputKeyboard}
	now := time.Now()
	keys := []struct {
		linux uint16
		usage uint8
	}{
		{59, 0x3a},  // F1
		{87, 0x44},  // F11
		{79, 0x59},  // keypad 1
		{96, 0x58},  // keypad Enter
		{99, 0x46},  // Print Screen
		{119, 0x48}, // Pause
		{127, 0x65}, // Menu
	}
	for _, key := range keys {
		if in.key(app, key.linux, 1, now) {
			t.Fatalf("key %d requested Stop on press", key.linux)
		}
		if in.key(app, key.linux, 0, now) {
			t.Fatalf("key %d requested Stop on release", key.linux)
		}
	}
	drainLocalKeyQueue(t, app)
	events := feed.snapshot()
	if len(events) != len(keys)*2 {
		t.Fatalf("local feed got %d events, want %d: %+v", len(events), len(keys)*2, events)
	}
	for i, key := range keys {
		code, ok := hidkeys.Code(key.usage)
		if !ok {
			t.Fatalf("test usage %#x is invalid", key.usage)
		}
		for j, action := range []remoteinput.Action{remoteinput.ActionPress, remoteinput.ActionRelease} {
			got := events[i*2+j]
			if got.Device != remoteinput.DeviceKeyboard || got.Kind != remoteinput.KindKey || got.Code != code || got.Action != action {
				t.Errorf("key %d event %d = %+v, want keyboard usage %#x action %d", key.linux, j, got, key.usage, action)
			}
		}
	}
}
