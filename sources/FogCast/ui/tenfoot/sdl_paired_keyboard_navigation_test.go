//go:build sdl3

package tenfoot

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestPairedSDLKeyboardNavigationRemainsAvailable(t *testing.T) {
	app := pointerCatalog(8)
	defer app.Stop()
	app.client = NewClient("http://127.0.0.1:1", nil)
	app.client.paired = true
	before := app.Snapshot().Grid.Focus
	// SDL keycode for Right is its scancode with the SDL scancode mask.
	if dispatchSyntheticSDLKey(app, 1, 1<<30|79, 79, true, time.Now()) {
		t.Fatal("paired SDL navigation quit")
	}
	if after := app.Snapshot().Grid.Focus; after == before {
		t.Fatalf("paired SDL Right did not navigate from %d", before)
	}
}

func TestSDLKitRemovalCannotQueueOldKeyboardDuringDisplayTransition(t *testing.T) {
	app := NewApp(NewClient("http://127.0.0.1:1", nil), 1280, 720, 20)
	defer app.Stop()
	feed := &fakePadFeed{}
	app.SetKitLocal(&fakeLocalCores{}, feed)
	app.mu.Lock()
	app.kitHost, app.kitLocalKeyboardUnavailable = true, true
	app.session = kitDisplaySession()
	app.sessionDisplayBusy, app.sessionDisplayNonHandoff = true, true
	app.sessionDisplayBinding = app.session
	physical := physicalPlayKey{source: 41, key: playHeldKey("a", 4)}
	key := playHIDKey{device: remoteinput.DeviceKeyboard, kind: remoteinput.KindKey, code: remoteinput.KeyA}
	app.playKeyHeld = map[physicalPlayKey]bool{physical: true}
	app.playKeyAdmitted = map[physicalPlayKey]playHIDKey{physical: key}
	app.rememberPlayHIDLocked(remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Code: remoteinput.KeyA, Action: remoteinput.ActionPress})
	app.mu.Unlock()
	dispatchSyntheticSDL(app, int(evKeyRemoved), 41)
	drainSDLKeyQueue(t, app)
	if got := feed.snapshot(); len(got) != 0 {
		t.Fatalf("SDL removal queued old guest input: %+v", got)
	}
}
