package playhid

import (
	"testing"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestChromeStopEscBackspaceNotLetterS(t *testing.T) {
	t.Parallel()
	if !ChromeStop("escape") || !ChromeStop("Backspace") {
		t.Fatal("Esc/Backspace must remain session-stop chrome")
	}
	if ChromeStop("s") || ChromeStop("return") || ChromeStop("") {
		t.Fatal("letter s and mapped core keys are not chrome stop")
	}
}

func TestEventZX81KeepsLetterSAndDropsEsc(t *testing.T) {
	t.Parallel()
	got, ok := Event("s", true, true)
	if !ok || got.Device != remoteinput.DeviceKeyboard || got.Kind != remoteinput.KindKey || got.Code != zx81keys.Letter('S') {
		t.Fatalf("ZX81 S = %+v ok=%v", got, ok)
	}
	if _, ok := Event("escape", true, true); ok {
		t.Fatal("Esc must not post onto the ZX81 matrix")
	}
	enter, ok := Event("return", true, true)
	if !ok || enter.Code != zx81keys.KeyEnter {
		t.Fatalf("ZX81 enter = %+v ok=%v", enter, ok)
	}
}

func TestEventNativeUsesGamepadNotZX81(t *testing.T) {
	t.Parallel()
	up, ok := Event("up", true, false)
	if !ok || up.Device != remoteinput.DeviceGamepad || up.Kind != remoteinput.KindButton || up.Code != remoteinput.ButtonDPadUp {
		t.Fatalf("native up = %+v ok=%v", up, ok)
	}
	s, ok := Event("s", true, false)
	if !ok || s.Code != remoteinput.ButtonDPadDown || s.Code >= 200 {
		t.Fatalf("native s must be D-pad, not axis/ZX81: %+v ok=%v", s, ok)
	}
	if s.Code == zx81keys.Letter('S') {
		t.Fatal("native s leaked ZX81 encoding")
	}
	a, ok := Event("z", true, false)
	if !ok || a.Code != remoteinput.ButtonA {
		t.Fatalf("native z = %+v ok=%v", a, ok)
	}
	if _, ok := Event("j", true, false); ok {
		t.Fatal("unmapped native letters must not post ZX81 codes")
	}
}

func TestPhysicalEventCarriesHIDUsages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		linux uint16
		usage uint8
	}{{30, 0x04}, {103, 0x52}, {1, 0x29}, {14, 0x2a}, {42, 0xe1}, {54, 0xe5}, {29, 0xe0}, {28, 0x28}, {11, 0x27}} {
		e, ok := PhysicalEvent(tc.linux, true)
		usage, isHID := hidkeys.Usage(e.Code)
		if !ok || !isHID || usage != tc.usage || e.Device != remoteinput.DeviceKeyboard || e.Kind != remoteinput.KindKey || e.Action != remoteinput.ActionPress {
			t.Fatalf("KEY %d = %+v ok=%v", tc.linux, e, ok)
		}
	}
	if _, ok := PhysicalEvent(304, true); ok { // BTN_SOUTH
		t.Fatal("gamepad button mapped as a keyboard key")
	}
}

// The kit sends HID usages; matrix and native cores must still receive
// exactly the keys the ZX81/arrow mapper produced before.
func TestStreamEventKeepsLegacyCoresOnTheirKeys(t *testing.T) {
	t.Parallel()
	for linux := uint16(1); linux < 200; linux++ {
		physical, ok := PhysicalEvent(linux, true)
		if !ok {
			continue
		}
		var legacy remoteinput.Event
		legacyOK := false
		if key, ok := zx81keys.FromLinuxKey(linux); ok {
			legacy, legacyOK = keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, key, true), true
		} else if code, ok := map[uint16]remoteinput.Code{103: remoteinput.KeyUp, 105: remoteinput.KeyLeft, 106: remoteinput.KeyRight, 108: remoteinput.KeyDown}[linux]; ok {
			legacy, legacyOK = keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, code, true), true
		}
		for _, mode := range []KeyboardMode{MatrixKeys, NativeKeys} {
			got, gotOK := StreamEvent(physical, mode)
			want, wantOK := remoteinput.Event{}, false
			if legacyOK {
				want, wantOK = StreamEvent(legacy, mode)
			}
			if gotOK != wantOK || got != want {
				t.Fatalf("KEY %d mode %d: got %+v/%v want %+v/%v", linux, mode, got, gotOK, want, wantOK)
			}
		}
	}
}

func TestStreamEventHIDKeepsPhysicalKeysIncludingChrome(t *testing.T) {
	t.Parallel()
	for _, linux := range []uint16{1, 14, 30, 58, 125} { // Esc, Backspace, A, CapsLock, LeftMeta
		e, _ := PhysicalEvent(linux, false)
		got, ok := StreamEvent(e, HIDKeys)
		if !ok || got != e {
			t.Fatalf("KEY %d = %+v ok=%v", linux, got, ok)
		}
	}
	zx, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, zx81keys.Letter('Q'), true), HIDKeys)
	if usage, _ := hidkeys.Usage(zx.Code); !ok || usage != 0x14 || zx.Action != remoteinput.ActionPress {
		t.Fatalf("legacy Q = %+v ok=%v", zx, ok)
	}
	arrow, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, remoteinput.KeyLeft, false), HIDKeys)
	if usage, _ := hidkeys.Usage(arrow.Code); !ok || usage != 0x50 || arrow.Action != remoteinput.ActionRelease {
		t.Fatalf("legacy left = %+v ok=%v", arrow, ok)
	}
	if _, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, remoteinput.KeyEscape, true), HIDKeys); ok {
		t.Fatal("unmapped legacy code reached an HID core")
	}
}

func TestStreamEventNativeRewritesKeyboardToGamepad(t *testing.T) {
	t.Parallel()
	zx, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, zx81keys.Letter('W'), true), NativeKeys)
	if !ok || zx.Device != remoteinput.DeviceGamepad || zx.Code != remoteinput.ButtonDPadUp {
		t.Fatalf("native W = %+v ok=%v", zx, ok)
	}
	arrow, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, remoteinput.KeyLeft, true), NativeKeys)
	if !ok || arrow.Code != remoteinput.ButtonDPadLeft {
		t.Fatalf("native arrow = %+v ok=%v", arrow, ok)
	}
	if _, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, zx81keys.Letter('J'), true), NativeKeys); ok {
		t.Fatal("native J must not keep a ZX81 code")
	}
	keep, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, zx81keys.Letter('J'), true), MatrixKeys)
	if !ok || keep.Code != zx81keys.Letter('J') {
		t.Fatalf("fes.keyboard J = %+v ok=%v", keep, ok)
	}
	pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	got, ok := StreamEvent(pad, NativeKeys)
	if !ok || got != pad {
		t.Fatalf("gamepad pass-through = %+v ok=%v", got, ok)
	}
}
