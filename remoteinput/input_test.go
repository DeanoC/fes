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

func TestStateRejectsUnsupportedEvent(t *testing.T) {
	s := State{}
	if err := s.Apply(Event{Device: 9, Kind: KindKey, Action: ActionPress, Code: KeyA}); err == nil {
		t.Fatal("unsupported device accepted")
	}
}
