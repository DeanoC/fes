package menudisplay

import (
	"bytes"
	"math/rand"
	"testing"
)

// On ARM writeRow takes the NEON path for whole 32-byte blocks; it must match
// the scalar conversion at every width and alignment and stop at the row end.
func TestWriteRowMatchesScalar(t *testing.T) {
	random := rand.New(rand.NewSource(949))
	for pixels := 1; pixels <= 80; pixels++ {
		for offset := 0; offset < 8; offset++ {
			src := make([]byte, offset+pixels*4+8)
			random.Read(src)
			got := bytes.Repeat([]byte{0xee}, len(src))
			want := bytes.Repeat([]byte{0xee}, len(src))
			row := src[offset : offset+pixels*4]
			writeRow(got[offset:offset+pixels*4], row)
			writeRowScalar(want[offset:offset+pixels*4], row)
			if !bytes.Equal(got, want) {
				t.Fatalf("%d pixels at offset %d differ from scalar", pixels, offset)
			}
		}
	}
	// Alpha stays in the fourth byte, which a session plane blends with.
	// Eight pixels are one NEON block on ARM.
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
