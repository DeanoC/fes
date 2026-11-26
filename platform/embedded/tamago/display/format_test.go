package display

import (
	"encoding/binary"
	"testing"
)

func TestConfigMatchesQEMURAMFBLayout(t *testing.T) {
	c, err := Config(0x54000000, 1280, 720, 5120)
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 28 || binary.BigEndian.Uint64(c[:8]) != 0x54000000 ||
		binary.BigEndian.Uint32(c[8:12]) != XRGB8888 || binary.BigEndian.Uint32(c[12:16]) != 0 ||
		binary.BigEndian.Uint32(c[16:20]) != 1280 || binary.BigEndian.Uint32(c[20:24]) != 720 ||
		binary.BigEndian.Uint32(c[24:]) != 5120 {
		t.Fatalf("invalid wire config: %x", c)
	}
}

func TestConfigRejectsUnsupportedGeometry(t *testing.T) {
	for _, c := range []struct {
		address               uint64
		width, height, stride int
	}{
		{0, 1280, 720, 5120}, {1, 0, 720, 5120}, {1, 1280, 0, 5120},
		{1, 1280, 720, 5119}, {1, 4097, 720, 16388}, {1, 1280, 2161, 5120},
		{^uint64(0) - 1, 1280, 720, 5120},
	} {
		if _, err := Config(c.address, c.width, c.height, c.stride); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
}

func TestDecodeConfigRoundTripAndRejections(t *testing.T) {
	wire, err := Config(0x54000000, 1280, 720, 5120)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeConfig(wire)
	if err != nil {
		t.Fatalf("DecodeConfig valid: %v", err)
	}
	want := RAMFBConfig{
		Address: 0x54000000,
		FourCC:  XRGB8888,
		Flags:   0,
		Width:   1280,
		Height:  720,
		Stride:  5120,
	}
	if got != want {
		t.Fatalf("DecodeConfig = %+v want %+v", got, want)
	}

	// Wrong FourCC.
	badFourCC := wire
	binary.BigEndian.PutUint32(badFourCC[8:12], 0x34324241)
	if _, err := DecodeConfig(badFourCC); err == nil {
		t.Fatal("accepted unsupported FourCC")
	}

	// Non-zero flags.
	badFlags := wire
	binary.BigEndian.PutUint32(badFlags[12:16], 1)
	if _, err := DecodeConfig(badFlags); err == nil {
		t.Fatal("accepted non-zero flags")
	}

	// Overflowing uint32 width (> MaxWidth and > math.MaxInt32 on 32-bit targets).
	badWidth := wire
	binary.BigEndian.PutUint32(badWidth[16:20], 0x80000010)
	if _, err := DecodeConfig(badWidth); err == nil {
		t.Fatal("accepted overflowing uint32 width")
	}

	// Wrapping address range.
	badAddr := wire
	binary.BigEndian.PutUint64(badAddr[0:8], ^uint64(0)-1024)
	if _, err := DecodeConfig(badAddr); err == nil {
		t.Fatal("accepted wrapping address range")
	}

	// Stride smaller than width*4.
	badStride := wire
	binary.BigEndian.PutUint32(badStride[24:28], 1280*4-4)
	if _, err := DecodeConfig(badStride); err == nil {
		t.Fatal("accepted undersized stride")
	}
}
