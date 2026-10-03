package joymatrix

import (
	"testing"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestDesired(t *testing.T) {
	pressed := []remoteinput.Code{remoteinput.ButtonDPadUp, remoteinput.ButtonDPadRight, remoteinput.ButtonDPadDown, remoteinput.ButtonDPadLeft, remoteinput.ButtonA, remoteinput.ButtonB, remoteinput.ButtonStart, remoteinput.ButtonSelect}
	for _, core := range []string{"fes.coleco", "fes.sms", "fes.sg1000"} {
		if !Supports(core) {
			t.Fatalf("unsupported %s", core)
		}
		got := Desired(core, remoteinput.Snapshot{Pressed: pressed})
		want := []remoteinput.Code{zx81keys.KeyShift, zx81keys.Letter('Z'), zx81keys.Letter('X'), zx81keys.Letter('C'), zx81keys.Letter('V')}
		if core == "fes.coleco" {
			want = append(want, zx81keys.Letter('Q'))
		}
		if len(got) != len(want) {
			t.Fatalf("%s: %v", core, got)
		}
		for _, key := range want {
			if !got[key] {
				t.Fatalf("%s missing %d", core, key)
			}
		}
	}
	if Supports("fes.zx81") {
		t.Fatal("ZX81 is not a joystick matrix core")
	}
}

func TestAxisDeadzone(t *testing.T) {
	for _, value := range []int16{-8000, 0, 8000} {
		if got := Desired("fes.sms", remoteinput.Snapshot{Axes: map[remoteinput.Code]int16{remoteinput.AxisLeftX: value}}); len(got) != 0 {
			t.Fatalf("%d: %v", value, got)
		}
	}
	for _, test := range []struct {
		axis  remoteinput.Code
		value int16
		key   remoteinput.Code
	}{
		{remoteinput.AxisLeftX, -8001, zx81keys.Letter('C')},
		{remoteinput.AxisLeftX, 8001, zx81keys.Letter('Z')},
		{remoteinput.AxisLeftY, -8001, zx81keys.KeyShift},
		{remoteinput.AxisLeftY, 8001, zx81keys.Letter('X')},
	} {
		got := Desired("fes.sms", remoteinput.Snapshot{Axes: map[remoteinput.Code]int16{test.axis: test.value}})
		if len(got) != 1 || !got[test.key] {
			t.Fatalf("%+v: %v", test, got)
		}
	}
}
