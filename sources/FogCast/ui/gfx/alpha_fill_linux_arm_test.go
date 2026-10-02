package gfx

import (
	"bytes"
	"testing"

	"golang.org/x/sys/cpu"
	"golang.org/x/sys/unix"
)

func TestNEONAlphaFillPageBoundaries(t *testing.T) {
	if !cpu.ARM.HasNEON {
		t.Skip("NEON unavailable")
	}
	page := unix.Getpagesize()
	mapped, err := unix.Mmap(-1, 0, 3*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Munmap(mapped)
	for _, guard := range [][]byte{mapped[:page], mapped[2*page:]} {
		if err := unix.Mprotect(guard, unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	c := RGBA(18, 25, 40, 180)
	a := uint16(c.A)
	source := [8]uint16{uint16(c.R)*a + 1, uint16(c.G)*a + 1, uint16(c.B)*a + 1, a*255 + 1}
	copy(source[4:], source[:4])
	for _, n := range []int{32, 64, 96, 128, page} {
		for _, start := range []int{page, 2*page - n} {
			row := mapped[start : start+n]
			for i := range row {
				row[i] = uint8(i)
			}
			want := append([]byte(nil), row...)
			for i := 0; i < n; i += 4 {
				blendOver(want, i, c.R, c.G, c.B, c.A)
			}
			fillAlphaNEON(row, &source, uint32(255-c.A))
			if !bytes.Equal(row, want) {
				t.Fatalf("boundary start=%d length=%d differs", start, n)
			}
		}
	}
}
