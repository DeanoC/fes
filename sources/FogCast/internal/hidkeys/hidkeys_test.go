package hidkeys

import (
	"testing"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestEncodeFollowsTheRowLayout(t *testing.T) {
	rows := Encode(map[uint8]bool{0x04: true, 0x0f: true, 0x10: true, 0x29: true, 0x7f: true, 0xe0: true, 0xe7: true, 0x03: true, 0x80: true, 0x05: false})
	want := Rows{1<<4 | 1<<15, 1 << 0, 1 << 9, 0, 0, 0, 0, 1 << 15, 1<<0 | 1<<7}
	if rows != want {
		t.Fatalf("rows %04x want %04x", rows, want)
	}
	if rows[0]&0x000f != 0 || rows[8]&0xff00 != 0 {
		t.Fatal("reserved bits set")
	}
	for _, usage := range []uint8{0, 3, 0x80, 0xa5, 0xdf, 0xe8, 0xff} {
		if Valid(usage) {
			t.Fatalf("usage %#x representable", usage)
		}
		if _, ok := Code(usage); ok {
			t.Fatalf("usage %#x encoded", usage)
		}
	}
	code, ok := Code(0x29)
	if back, isHID := Usage(code); !ok || !isHID || back != 0x29 || code != Base+0x29 {
		t.Fatal("escape code round trip")
	}
	if _, ok := Usage(zx81keys.Letter('A')); ok {
		t.Fatal("ZX81 code read as HID")
	}
}

func TestLinuxKeysMapToDistinctUsages(t *testing.T) {
	seen := map[uint8]uint16{}
	for linux, usage := range linuxUsage {
		if !Valid(usage) {
			t.Fatalf("KEY %d maps to unrepresentable %#x", linux, usage)
		}
		if other, dup := seen[usage]; dup {
			t.Fatalf("KEY %d and %d share usage %#x", linux, other, usage)
		}
		seen[usage] = linux
	}
	for linux, want := range map[uint16]uint8{1: 0x29, 14: 0x2a, 28: 0x28, 30: 0x04, 44: 0x1d, 57: 0x2c, 103: 0x52, 125: 0xe3} {
		if got, ok := FromLinuxKey(linux); !ok || got != want {
			t.Fatalf("KEY %d = %#x", linux, got)
		}
	}
}

func TestLegacyCodesRoundTrip(t *testing.T) {
	for usage := uint8(0x04); usage <= 0x52; usage++ {
		legacy, ok := Legacy(usage)
		if !ok {
			continue
		}
		back, ok := FromLegacy(legacy)
		if !ok || (back != usage && !(usage == 0xe5 && back == 0xe1)) {
			t.Fatalf("usage %#x -> %d -> %#x", usage, legacy, back)
		}
	}
	if code, ok := Legacy(0xe5); !ok || code != zx81keys.KeyShift {
		t.Fatal("right shift is the ZX81 shift")
	}
	if _, ok := Legacy(0x29); ok {
		t.Fatal("escape has no legacy form")
	}
	if code, ok := Legacy(0x50); !ok || code != remoteinput.KeyLeft {
		t.Fatal("left arrow legacy form")
	}
}
