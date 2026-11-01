//go:build linux

package tenfoot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/testwait"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestNativeHIDUsageKeepsMainKeysAndAddsTheRest(t *testing.T) {
	// The keys tenfoot mapped before #1331 keep their usages.
	kept := map[uint16]uint8{1: 0x29, 2: 0x1e, 11: 0x27, 12: 0x2d, 13: 0x2e, 14: 0x2a, 15: 0x2b, 16: 0x14, 26: 0x2f, 27: 0x30,
		28: 0x28, 29: 0xe0, 30: 0x04, 39: 0x33, 40: 0x34, 41: 0x35, 42: 0xe1, 43: 0x31, 50: 0x10, 51: 0x36, 52: 0x37, 53: 0x38,
		54: 0xe5, 56: 0xe2, 57: 0x2c, 58: 0x39, 97: 0xe4, 100: 0xe6, 102: 0x4a, 103: 0x52, 104: 0x4b, 105: 0x50, 106: 0x4f,
		107: 0x4d, 108: 0x51, 109: 0x4e, 110: 0x49, 111: 0x4c, 125: 0xe3, 126: 0xe7}
	for code, want := range kept {
		if got := nativeHIDUsage(code); got != want {
			t.Fatalf("key %d = %#x, want %#x", code, got, want)
		}
	}
	added := map[uint16]uint8{
		59: 0x3a, 68: 0x43, 87: 0x44, // F1, F10, F11
		69: 0x53, 79: 0x59, 82: 0x62, 83: 0x63, 96: 0x58, 98: 0x54, 55: 0x55, 74: 0x56, 78: 0x57, 117: 0x67, // keypad, Num Lock
		70: 0x47, 99: 0x46, 119: 0x48, // Scroll Lock, Print Screen, Pause
		86: 0x64, 127: 0x65, // 102nd key, Menu
	}
	for code, want := range added {
		if got := nativeHIDUsage(code); got != want || !hidkeys.Valid(got) {
			t.Fatalf("key %d = %#x, want %#x", code, got, want)
		}
	}
	if got := nativeHIDUsage(0xffff); got != 0 {
		t.Fatalf("unmapped key = %#x", got)
	}
}

func TestKitKeyboardForwardsFunctionAndKeypadKeysToHIDCores(t *testing.T) {
	t.Parallel()
	codes := make(chan remoteinput.Event, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/input/event" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Event remoteinput.Event `json:"event"`
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		codes <- body.Event
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	app := pointerCatalog(4)
	app.client = NewClient(server.URL, server.Client())
	app.session = hostclient.SessionResult{State: "active", CoreKeyboardHID: true, Input: &hostclient.SessionInput{State: "attached", Ready: true}}
	for _, code := range []uint16{59, 87, 79, 96, 99, 119, 127} { // F1, F11, KP1, KP Enter, PrtSc, Pause, Menu
		if !app.HandlePlayHIDScancode(nativeKeyName(code), nativeHIDUsage(code), true, time.Now()) {
			t.Fatalf("key %d not consumed", code)
		}
		select {
		case got := <-codes:
			if usage, ok := hidkeys.Usage(got.Code); !ok || usage != nativeHIDUsage(code) || got.Device != remoteinput.DeviceKeyboard {
				t.Fatalf("key %d posted %+v", code, got)
			}
		case <-time.After(testwait.Timeout):
			t.Fatalf("key %d not forwarded", code)
		}
	}
}
