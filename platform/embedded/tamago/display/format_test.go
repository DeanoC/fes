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
