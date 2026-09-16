package shared

import "testing"

func TestPortableSystemUsesHandheldCatalogIDs(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"gb", "gbc", "gba", "gg", "lynx", "ws", "wsc", "psp", "nds"} {
		if !PortableSystem(id) {
			t.Fatalf("%s should be portable", id)
		}
	}
	for _, id := range []string{"", "megadrive", "snes", "nes", "pong"} {
		if PortableSystem(id) {
			t.Fatalf("%s should not be portable", id)
		}
	}
}
