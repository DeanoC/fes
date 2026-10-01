package zx81tapes

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestBundledTapes(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Entries() {
		if seen[e.ID] || e.ID == "" || e.Name == "" || e.License == "" || e.SourceURL == "" || e.Controls == "" {
			t.Fatalf("missing or duplicate metadata: %+v", e)
		}
		seen[e.ID] = true
		if len(e.Data) < 128 || len(e.Data) > 16*1024 {
			t.Fatalf("%s: cassette outside media contract", e.ID)
		}
		sum := sha256.Sum256(e.Data)
		if hex.EncodeToString(sum[:]) != e.SHA256 {
			t.Fatalf("%s: bytes do not match recorded provenance", e.ID)
		}
		// .p begins at $4009; D_FILE and VARS must reference the payload.
		dfile := int(binary.LittleEndian.Uint16(e.Data[3:5])) - 0x4009
		vars := int(binary.LittleEndian.Uint16(e.Data[7:9])) - 0x4009
		if dfile < 116 || dfile >= vars || vars >= len(e.Data) || e.Data[dfile] != 0x76 || e.Data[vars] != 0x80 {
			t.Fatalf("%s: invalid display/variable boundaries (%d, %d)", e.ID, dfile, vars)
		}
		got, ok := Lookup(e.ID)
		if !ok || got.SHA256 != e.SHA256 {
			t.Fatalf("%s: lookup failed", e.ID)
		}
		e.Data[0] ^= 0xff
		fresh, _ := Lookup(e.ID)
		if fresh.Data[0] != got.Data[0] {
			t.Fatal("caller mutated embedded tape")
		}
	}
	if _, ok := Lookup("../aritm"); ok {
		t.Fatal("unknown ID accepted")
	}
}

func TestGuessNumberFitsBareMachine(t *testing.T) {
	e, ok := Lookup("guess-number")
	if !ok || e.RAMKB != 1 {
		t.Fatal("missing bare-machine cassette")
	}
	dfile := int(binary.LittleEndian.Uint16(e.Data[3:5])) - 0x4009
	vars := int(binary.LittleEndian.Uint16(e.Data[7:9])) - 0x4009
	if vars-dfile != 25 {
		t.Fatal("bare-machine cassette must have collapsed display")
	}
	for _, ch := range e.Data[dfile:vars] {
		if ch != 0x76 {
			t.Fatal("collapsed display contains nonempty cells")
		}
	}
	// Starting payload plus longest screen line, two six-byte numeric vars,
	// bounded numeric input, calculator workspace and machine-stack reserve.
	const runtimeReserve = 24 + 12 + 64 + 64 + 128
	if 9+len(e.Data)+runtimeReserve > 1024 {
		t.Fatal("program and normal-play workspace exceed 1 KiB")
	}
}
