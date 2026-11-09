//go:build linux

package menudisplay

import (
	"bytes"
	"math/rand"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// guardedRowBuffer leaves inaccessible pages on both sides of a buffer
// large enough for a full row. Unlike slice sentinels, these catch reads
// outside a row that ends at the boundary as well as writes.
func guardedRowBuffer(t *testing.T) []byte {
	t.Helper()
	page := os.Getpagesize()
	size := (Stride + page - 1) / page * page
	mapping, err := unix.Mmap(-1, 0, size+2*page, unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	buffer := mapping[page : page+size]
	if err := unix.Mprotect(buffer, unix.PROT_READ|unix.PROT_WRITE); err != nil {
		t.Fatal(err)
	}
	return buffer
}

func TestWriteRowGuardPages(t *testing.T) {
	src := guardedRowBuffer(t)
	dst := guardedRowBuffer(t)
	rand.New(rand.NewSource(950)).Read(src)
	if err := unix.Mprotect(src, unix.PROT_READ); err != nil {
		t.Fatal(err)
	}
	widths := []int{Width - 1, Width}
	for pixels := 0; pixels <= 80; pixels++ {
		widths = append(widths, pixels)
	}
	for _, pixels := range widths {
		n := pixels * 4
		// Exercise either boundary independently for each buffer. A row
		// can start at a guard or end at one, including a scalar tail.
		for _, srcAt := range []int{0, len(src) - n} {
			for _, dstAt := range []int{0, len(dst) - n} {
				for i := range dst {
					dst[i] = 0xee
				}
				want := bytes.Clone(dst)
				row := src[srcAt : srcAt+n]
				writeRowScalar(want[dstAt:dstAt+n], row)
				writeRow(dst[dstAt:dstAt+n], row)
				if !bytes.Equal(dst, want) {
					t.Fatalf("%d pixels at src %d, dst %d differ from scalar", pixels, srcAt, dstAt)
				}
			}
		}
	}
}

func TestWriteRowFaultIsAnError(t *testing.T) {
	for _, side := range []string{"source", "destination"} {
		t.Run(side, func(t *testing.T) {
			src := guardedRowBuffer(t)
			dst := guardedRowBuffer(t)
			buffer := dst
			if side == "source" {
				buffer = src
			}
			if err := unix.Mprotect(buffer, unix.PROT_NONE); err != nil {
				t.Fatal(err)
			}
			// Cover a scalar row, a NEON block, and a block with a tail.
			// The protected buffer must unwind through the Go fault guard.
			w := window{}
			for _, n := range []int{4, 32, 36} {
				if err := w.guard(func() { writeRow(dst[:n], src[:n]) }); err == nil || err.Error() != "menu window revoked while drawing" {
					t.Fatalf("protected %s, %d bytes: %v", side, n, err)
				}
			}
		})
	}
}
