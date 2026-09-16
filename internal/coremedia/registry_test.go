package coremedia

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestLookupColecoControllersDiagnostic(t *testing.T) {
	data, ok := Lookup("fes.coleco")
	if !ok {
		t.Fatal("Lookup(fes.coleco) reported no default media")
	}
	if len(data) != 2299 {
		t.Fatalf("len(data) = %d, want 2299", len(data))
	}
	digest := sha256.Sum256(data)
	if got := hex.EncodeToString(digest[:]); got != "ef9443c2787cd02b6d78d233d015b0bbf3fb21d53d1a5890497cdbb7897f053c" {
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
