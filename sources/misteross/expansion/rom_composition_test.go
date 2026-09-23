package expansion

import (
	"bytes"
	"context"
	"testing"
)

func TestComposeROMPreservesVerifiedOverlay(t *testing.T) {
	base, m := romFixture()
	loaded, err := loadRBF(base)
	if err != nil {
		t.Fatal(err)
	}
	setCramBit(loaded.cram, 1800, 100, 1)
	cart := saveRBF(loaded)
	asset, err := NewAsset(Manifest{Format: 1, Device: Device, Slot: Slot, SlotMajor: 1, Map: Map, RecipeSHA256: digest([]byte("recipe")), Revision: "0123456789abcdef0123456789abcdef01234567", ShellBuildID: "0123456789abcdef0123456789abcdef", ShellPackageID: digest([]byte("package")), ShellSHA256: digest(base), CartSHA256: digest(cart), CartSize: int64(len(cart))}, cart)
	if err != nil {
		t.Fatal(err)
	}
	shell := Shell{PackageID: digest([]byte("package")), BuildID: "0123456789abcdef0123456789abcdef", Payload: base, Slot: Slot, SlotMajor: 1}
	_, overlay, linked, err := ComposeROM(context.Background(), shell, asset, m, bytes.Repeat([]byte{0xff}, 1024))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(overlay, linked) {
		t.Fatal("ROM was not patched")
	}
	decoded, err := loadRBF(linked)
	if err != nil {
		t.Fatal(err)
	}
	if cramBit(decoded.cram, 1800, 100) != 1 {
		t.Fatal("lost expansion")
	}
	m.Blocks[0].WordBits[0][0] = uint32(100*cramWidth + 1800)
	if _, _, _, err := ComposeROM(context.Background(), shell, asset, m, make([]byte, 1024)); err == nil {
		t.Fatal("overlapping expansion destination accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ComposeContext(ctx, shell, asset); err == nil {
		t.Fatal("cancel ignored")
	}
}
