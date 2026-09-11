package playhid

import (
	"testing"

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

func TestPhysicalEventArrowsAndZX81(t *testing.T) {
	t.Parallel()
	a, ok := PhysicalEvent(30, true) // KEY_A
	if !ok || a.Code != zx81keys.Letter('A') || a.Device != remoteinput.DeviceKeyboard {
		t.Fatalf("KEY_A = %+v ok=%v", a, ok)
	}
	up, ok := PhysicalEvent(linuxKeyUp, true)
	if !ok || up.Code != remoteinput.KeyUp || up.Device != remoteinput.DeviceKeyboard {
		t.Fatalf("KEY_UP = %+v ok=%v", up, ok)
	}
	if _, ok := PhysicalEvent(1, true); ok { // KEY_ESC
		t.Fatal("Escape is chrome, not a physical play HID event")
	}
}

func TestStreamEventNativeRewritesKeyboardToGamepad(t *testing.T) {
	t.Parallel()
	zx, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, zx81keys.Letter('W'), true), false)
	if !ok || zx.Device != remoteinput.DeviceGamepad || zx.Code != remoteinput.ButtonDPadUp {
		t.Fatalf("native W = %+v ok=%v", zx, ok)
	}
	arrow, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, remoteinput.KeyLeft, true), false)
	if !ok || arrow.Code != remoteinput.ButtonDPadLeft {
		t.Fatalf("native arrow = %+v ok=%v", arrow, ok)
	}
	if _, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, zx81keys.Letter('J'), true), false); ok {
		t.Fatal("native J must not keep a ZX81 code")
	}
	keep, ok := StreamEvent(keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, zx81keys.Letter('J'), true), true)
	if !ok || keep.Code != zx81keys.Letter('J') {
		t.Fatalf("fes.keyboard J = %+v ok=%v", keep, ok)
	}
	pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	got, ok := StreamEvent(pad, false)
	if !ok || got != pad {
		t.Fatalf("gamepad pass-through = %+v ok=%v", got, ok)
	}
}
