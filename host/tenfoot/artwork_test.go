package tenfoot

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestDecodeCoverScalesJPEGSizedPNG(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(0, 0, 40, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 40; x++ {
			src.Set(x, y, color.RGBA{R: 200, G: 10, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCover(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() > coverMaxW || got.Bounds().Dy() > coverMaxH {
		t.Fatalf("size = %s", got.Bounds())
	}
	shot, err := DecodeScreenshot(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if shot.Bounds().Dx() > shotMaxW || shot.Bounds().Dy() > shotMaxH {
		t.Fatalf("screenshot size = %s", shot.Bounds())
	}
	r, g, b, a := got.At(0, 0).RGBA()
	if r>>8 < 150 || g>>8 > 40 || b>>8 > 40 || a>>8 != 255 {
		t.Fatalf("pixel = %d %d %d %d", r, g, b, a)
	}
}

func TestDecodeCoverScalesAcceptedSource(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(0, 0, 512, 640))
	for y := 0; y < 640; y++ {
		for x := 0; x < 512; x++ {
			src.Set(x, y, color.RGBA{R: 10, G: 200, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCover(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != coverMaxW || got.Bounds().Dy() != coverMaxH {
		t.Fatalf("size = %s want %dx%d", got.Bounds(), coverMaxW, coverMaxH)
	}
}

func TestDecodeCoverRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := DecodeCover(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeCoverRejectsOversizedBeforeDecode(t *testing.T) {
	t.Parallel()
	// IHDR-only PNG: DecodeConfig sees 100000x100000, image.Decode would
	// allocate tens of gigabytes. Rejection must happen first.
	_, err := DecodeCover(pngIHDR(100000, 100000))
	if err == nil {
		t.Fatal("expected oversized artwork to be rejected")
	}
	if !strings.Contains(err.Error(), "exceed limit") {
		t.Fatalf("err = %v", err)
	}
	_, err = DecodeCover(pngIHDR(coverDecodeMaxEdge+1, 1))
	if err == nil {
		t.Fatal("expected over-width artwork to be rejected")
	}
}

func pngIHDR(width, height uint32) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 2
	writePNGChunk(&b, "IHDR", ihdr)
	writePNGChunk(&b, "IEND", nil)
	return b.Bytes()
}

func writePNGChunk(w *bytes.Buffer, typ string, data []byte) {
	_ = binary.Write(w, binary.BigEndian, uint32(len(data)))
	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte(typ))
	_, _ = crc.Write(data)
	w.Write([]byte(typ))
	w.Write(data)
	_ = binary.Write(w, binary.BigEndian, crc.Sum32())
}

func TestDecodeStillScalesBackdropLargerThanCover(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			src.Set(x, y, color.RGBA{R: 10, G: 10, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeStill(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() > stillMaxW || got.Bounds().Dy() > stillMaxH {
		t.Fatalf("size = %s", got.Bounds())
	}
	if got.Bounds().Dx() <= coverMaxW {
		t.Fatalf("still was clamped to cover size %s", got.Bounds())
	}
}

func TestCoverDestRectPreservesAspect(t *testing.T) {
	t.Parallel()
	x, y, w, h := coverDestRect(10, 20, 210, 284, 256, 256)
	if w != 210 || h != 210 || x != 10 || y != 57 {
		t.Fatalf("square dest = %v %v %v %v", x, y, w, h)
	}
	x, y, w, h = coverDestRect(0, 0, 210, 284, 256, 320)
	if w != 210 || h != 262.5 || x != 0 || y != 10.75 {
		t.Fatalf("portrait dest = %v %v %v %v", x, y, w, h)
	}
	x, y, w, h = coverDestRect(5, 5, 210, 284, 200, 300)
	if h != 284 || w <= 0 || w >= 210 {
		t.Fatalf("2:3 dest = %v %v %v %v", x, y, w, h)
	}
	if y < 4.99 || y > 5.01 {
		t.Fatalf("2:3 should fill height, y=%v", y)
	}
	if x <= 5 {
		t.Fatalf("2:3 should pillarbox, x=%v", x)
	}
}
