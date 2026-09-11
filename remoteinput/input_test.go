package remoteinput

import "testing"

func TestNormalizeKeyboard(t *testing.T) {
	tests := []struct {
		name, key string
		want      Code
		ok        bool
	}{
		{"escape", "Escape", KeyEscape, true}, {"a", "a", KeyA, true}, {"left", "ArrowLeft", KeyLeft, true}, {"unknown", "F12", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeKeyboard(tt.key, true)
			if (err == nil) != tt.ok || got.Code != tt.want {
				t.Fatalf("got %#v err=%v", got, err)
			}
		})
	}
}

func TestNormalizeGamepad(t *testing.T) {
	got, err := NormalizeGamepad("dpad-up", true)
	if err != nil || got.Device != DeviceGamepad || got.Code != ButtonDPadUp || got.Action != ActionPress {
		t.Fatalf("got %#v err=%v", got, err)
	}
}

func TestNormalizeGamepadCDoesNotRepurposeSelect(t *testing.T) {
	c, err := NormalizeGamepad("c", true)
	if err != nil {
		t.Fatal(err)
	}
	selectButton, err := NormalizeGamepad("select", true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != ButtonC {
		t.Fatalf("C = %#v, want ButtonC", c)
	}
	if c.Code != 108 {
		t.Fatalf("ButtonC = %d, want code 108", c.Code)
	}
	if selectButton.Code != ButtonSelect {
		t.Fatalf("select = %#v, want ButtonSelect", selectButton)
	}
	if selectButton.Code != 107 {
		t.Fatalf("ButtonSelect = %d, want code 107", selectButton.Code)
	}
	if c.Code == selectButton.Code {
		t.Fatalf("C = %#v, select = %#v", c, selectButton)
	}
}

func TestStateSnapshotIsAuthoritativeAndReleaseAll(t *testing.T) {
	var s State
	if err := s.Apply(Event{Device: DeviceKeyboard, Kind: KindKey, Action: ActionPress, Code: KeyA}); err != nil {
		t.Fatal(err)
	}
	if !s.Pressed(KeyA) {
		t.Fatal("key not pressed")
	}
	snap := s.Snapshot()
	if len(snap.Pressed) != 1 || snap.Pressed[0] != KeyA {
		t.Fatalf("snapshot %#v", snap)
	}
	s.ReleaseAll()
	if len(s.Snapshot().Pressed) != 0 {
		t.Fatal("state not released")
	}
}

func TestNormalizeAxisClamps(t *testing.T) {
	got, err := NormalizeAxis("left-x", 40000)
	if err != nil || got.Value != 32767 {
		t.Fatalf("got %#v err=%v", got, err)
	}
}

func TestEventForCodeKeepsZX81AsKeyboardNotAxis(t *testing.T) {
	t.Parallel()
	key := EventForCode(KeyA, ActionPress)
	if key.Device != DeviceKeyboard || key.Kind != KindKey {
		t.Fatalf("KeyA = %+v", key)
	}
	button := EventForCode(ButtonA, ActionPress)
	if button.Device != DeviceGamepad || button.Kind != KindButton {
		t.Fatalf("ButtonA = %+v", button)
	}
	axis := EventForCode(AxisLeftX, ActionAbsolute)
	if axis.Device != DeviceGamepad || axis.Kind != KindAxis {
		t.Fatalf("AxisLeftX = %+v", axis)
	}
	zx81 := EventForCode(256, ActionPress)
	if zx81.Device != DeviceKeyboard || zx81.Kind != KindKey {
		t.Fatalf("ZX81 matrix code replayed as %+v", zx81)
	}
}

func TestStateRejectsUnsupportedEvent(t *testing.T) {
	s := State{}
	if err := s.Apply(Event{Device: 9, Kind: KindKey, Action: ActionPress, Code: KeyA}); err == nil {
		t.Fatal("unsupported device accepted")
	}
}

func TestSNESAdditionalButtonsRetainWireCodes(t *testing.T) {
	for name, want := range map[string]Code{"select": 107, "c": 108, "x": 109, "y": 110, "l": 111, "r": 112} {
		event, err := NormalizeGamepad(name, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if event.Code != want || event.Device != DeviceGamepad || event.Kind != KindButton {
			t.Fatalf("%s: %+v", name, event)
		}
		var state State
		if err := state.Apply(event); err != nil {
			t.Fatal(err)
		}
		if !state.Pressed(want) {
			t.Fatal("button not retained")
		}
		state.ReleaseAll()
		if len(state.Snapshot().Pressed) != 0 {
			t.Fatal("button stuck after release")
		}
	}
}
