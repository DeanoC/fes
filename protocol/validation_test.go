package protocol_test

import (
	"testing"

	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestValidateGameID(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"megadrive-test": true,
		"snes-test":      true,
		"MegaDrive":      false,
		"two words":      false,
		"../escape":      false,
		"":               false,
	}
	for input, wantValid := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			err := protocol.ValidateGameID(input)
			if (err == nil) != wantValid {
				t.Fatalf("ValidateGameID(%q) error = %v, want valid=%v", input, err, wantValid)
			}
		})
	}
}

func TestValidateSystem(t *testing.T) {
	t.Parallel()
	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES} {
		if err := protocol.ValidateSystem(system); err != nil {
			t.Fatalf("ValidateSystem(%q): %v", system, err)
		}
	}
	if err := protocol.ValidateSystem("nes"); err == nil {
		t.Fatal("ValidateSystem(nes) succeeded")
	}
}
