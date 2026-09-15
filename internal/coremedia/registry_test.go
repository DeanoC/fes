package coremedia

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestLookupColecoGraphicsDiagnostic(t *testing.T) {
	data, ok := Lookup("fes.coleco")
	if !ok {
		t.Fatal("Lookup(fes.coleco) reported no default media")
	}
	if len(data) != 989 {
		t.Fatalf("len(data) = %d, want 989", len(data))
	}
	digest := sha256.Sum256(data)
	if got := hex.EncodeToString(digest[:]); got != "9f9fa280b141e0538a571bb66f1e2447f691eecb853ae05547720f2ccc20783c" {
		t.Fatalf("sha256 = %s", got)
	}

	data[0] ^= 0xff
	again, ok := Lookup("fes.coleco")
	if !ok || again[0] == data[0] {
		t.Fatal("Lookup returned mutable registry storage")
	}
}

func TestLookupWithoutDefaultMedia(t *testing.T) {
	for _, coreID := range []string{"fes.pong", "fes.unknown"} {
		if data, ok := Lookup(coreID); ok || data != nil {
			t.Fatalf("Lookup(%q) = %x, %t; want no asset", coreID, data, ok)
		}
	}
}
