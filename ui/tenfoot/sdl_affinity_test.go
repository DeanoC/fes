//go:build sdl3

package tenfoot

import "testing"

func TestSDLHotplugEventsClaimAndRestoreAffinity(t *testing.T) {
	app := pointerCatalog(4)

	if dispatchSyntheticSDL(app, int(evKeyAdded), 11) {
		t.Fatal("keyboard add quit")
	}
	if got := app.Affinity(); got.Kind != InputKeyboard || got.ID != 11 {
		t.Fatalf("keyboard add = %#v", got)
	}

	if dispatchSyntheticSDL(app, int(evMouseAdded), 12) {
		t.Fatal("mouse add quit")
	}
	if got := app.Affinity(); got.Kind != InputMouse || got.ID != 12 {
		t.Fatalf("mouse add = %#v", got)
	}

	if dispatchSyntheticSDL(app, int(evMouseRemoved), 12) {
		t.Fatal("mouse remove quit")
	}
	if got := app.Affinity(); got.Kind != InputKeyboard || got.ID != 11 {
		t.Fatalf("mouse remove = %#v", got)
	}

	if dispatchSyntheticSDL(app, int(evKeyRemoved), 11) {
		t.Fatal("keyboard remove quit")
	}
	if got := app.Affinity(); got.Kind != InputNone {
		t.Fatalf("keyboard remove = %#v", got)
	}
}
