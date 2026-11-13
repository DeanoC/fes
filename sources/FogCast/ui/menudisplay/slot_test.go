package menudisplay

import (
	"bytes"
	"math/rand"
	"testing"
)

// On ARM and ARM64 writeRow takes the NEON path for whole 32-byte blocks;
// it must match the scalar conversion at every width and alignment and stop
// at the row end. Source and destination need not have the same alignment.
func TestWriteRowMatchesScalar(t *testing.T) {
	random := rand.New(rand.NewSource(949))
	widths := []int{Width - 1, Width, Width + 1}
	for pixels := 0; pixels <= 80; pixels++ {
		widths = append(widths, pixels)
	}
	for _, pixels := range widths {
		for srcOffset := 0; srcOffset < 32; srcOffset++ {
			src := make([]byte, srcOffset+pixels*4+32)
			random.Read(src)
			before := bytes.Clone(src)
			row := src[srcOffset : srcOffset+pixels*4]
			for dstOffset := 0; dstOffset < 32; dstOffset++ {
				got := bytes.Repeat([]byte{0xee}, dstOffset+pixels*4+32)
				want := bytes.Clone(got)
				writeRow(got[dstOffset:dstOffset+pixels*4], row)
				writeRowScalar(want[dstOffset:dstOffset+pixels*4], row)
				if !bytes.Equal(got, want) {
					t.Fatalf("%d pixels at src offset %d, dst offset %d differ from scalar", pixels, srcOffset, dstOffset)
				}
			}
			if !bytes.Equal(src, before) {
				t.Fatalf("%d pixels at src offset %d changed the source", pixels, srcOffset)
			}
		}
	}
	// Alpha stays in the fourth byte, which a session plane blends with.
	// Eight pixels are one NEON block on ARM and ARM64.
	src := bytes.Repeat([]byte{0x11, 0x22, 0x33, 0xff, 0x44, 0x55, 0x66, 0x80}, 4)
	got := make([]byte, len(src))
	writeRow(got, src)
	if want := bytes.Repeat([]byte{0x33, 0x22, 0x11, 0xff, 0x66, 0x55, 0x44, 0x80}, 4); !bytes.Equal(got, want) {
		t.Fatalf("pixels % x", got)
	}
}

func BenchmarkWriteFullFrame(b *testing.B) {
	slot, frame := make([]byte, FrameBytes), make([]byte, FrameBytes)
	b.SetBytes(FrameBytes)
	for i := 0; i < b.N; i++ {
		writeRect(slot, frame, fullFrame[0])
	}
}

func BenchmarkWriteFullFrameScalar(b *testing.B) {
	slot, frame := make([]byte, FrameBytes), make([]byte, FrameBytes)
	b.SetBytes(FrameBytes)
	for i := 0; i < b.N; i++ {
		writeRowScalar(slot, frame)
	}
}
