package zx81keys

import "testing"

func TestFromLinuxKeyZX81LettersAndDigits(t *testing.T) {
	t.Parallel()
	if got, ok := FromLinuxKey(linuxKeyA); !ok || got != Letter('A') {
		t.Fatalf("KEY_A = %d ok=%v", got, ok)
	}
	if got, ok := FromLinuxKey(linuxKeyEnter); !ok || got != KeyEnter {
		t.Fatalf("KEY_ENTER = %d ok=%v", got, ok)
	}
	if got, ok := FromLinuxKey(linuxKey1); !ok || got != Digit(1) {
		t.Fatalf("KEY_1 = %d ok=%v", got, ok)
	}
	if got, ok := FromLinuxKey(linuxKey0); !ok || got != Digit(0) {
		t.Fatalf("KEY_0 = %d ok=%v", got, ok)
	}
	if _, ok := FromLinuxKey(linuxKeyEsc); ok {
		t.Fatal("Escape is not a ZX81 matrix key")
	}
	if _, ok := FromLinuxKey(304); ok {
		t.Fatal("BTN_SOUTH is not a keyboard key")
	}
}
