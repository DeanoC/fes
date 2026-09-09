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

	xdraw "golang.org/x/image/draw"
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

func TestScaleToFitDiffersFromNearestOnCheckerboard(t *testing.T) {
	t.Parallel()
	const srcN, dstN = 32, 16
	src := image.NewRGBA(image.Rect(0, 0, srcN, srcN))
	black := color.RGBA{0, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	for y := 0; y < srcN; y++ {
		for x := 0; x < srcN; x++ {
			if (x+y)%2 == 0 {
				src.Set(x, y, white)
			} else {
				src.Set(x, y, black)
			}
		}
	}
	got := scaleToFit(src, dstN, dstN)
	near := scaleToFitWith(src, dstN, dstN, xdraw.NearestNeighbor)
	if got.Bounds() != near.Bounds() || got.Bounds().Dx() != dstN {
		t.Fatalf("size got=%s near=%s", got.Bounds(), near.Bounds())
	}
	// Integer nearest of a 1px checkerboard at 2× downscale samples only even
	// source pixels, so every dest pixel is white. Catmull–Rom mixes neighbours.
	r, g, b, a := near.At(0, 0).RGBA()
	if r>>8 != 255 || g>>8 != 255 || b>>8 != 255 || a>>8 != 255 {
		t.Fatalf("nearest (0,0) = %d %d %d %d want white", r, g, b, a)
	}
	identical := true
	var mse float64
	n := 0
	for y := 0; y < dstN; y++ {
		for x := 0; x < dstN; x++ {
			pg := got.RGBAAt(x, y)
			pn := near.RGBAAt(x, y)
			if pg != pn {
				identical = false
			}
			dr := float64(pg.R) - float64(pn.R)
			dg := float64(pg.G) - float64(pn.G)
			db := float64(pg.B) - float64(pn.B)
			mse += dr*dr + dg*dg + db*db
			n++
		}
	}
	mse /= float64(n)
	if identical {
		t.Fatal("Catmull-Rom downscale matched nearest-neighbour")
	}
	if mse < 100 {
		t.Fatalf("MSE vs nearest = %v, want a clear filter difference", mse)
	}
	cr, cg, cb, _ := got.At(dstN/2, dstN/2).RGBA()
	if cr>>8 == 255 && cg>>8 == 255 && cb>>8 == 255 {
		t.Fatal("quality downscale still sampled only even checkerboard pixels")
	}
}

func TestDecodeCoverDownscaleDiffersFromNearest(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(0, 0, 512, 640))
	for y := 0; y < 640; y++ {
		for x := 0; x < 512; x++ {
			if (x/8+y/8)%2 == 0 {
				src.Set(x, y, color.RGBA{R: 240, G: 20, B: 20, A: 255})
			} else {
				src.Set(x, y, color.RGBA{R: 20, G: 20, B: 240, A: 255})
			}
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
	near := scaleToFitWith(src, coverMaxW, coverMaxH, xdraw.NearestNeighbor)
	if pixelsEqual(got, near) {
		t.Fatal("DecodeCover matched nearest-neighbour downscale")
	}
}

func pixelsEqual(a, b *image.RGBA) bool {
	if a == nil || b == nil || a.Bounds() != b.Bounds() {
		return false
	}
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return false
			}
		}
	}
	return true
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

func TestCoverFillRectCropsToFill(t *testing.T) {
	t.Parallel()
	x, y, w, h := CoverFillRect(0, 0, 640, 480, 16, 8)
	if w != 960 || h != 480 || x != -160 || y != 0 {
		t.Fatalf("wide fill = %v %v %v %v", x, y, w, h)
	}
	x, y, w, h = CoverFillRect(0, 0, 640, 480, 8, 8)
	if w != 640 || h != 640 || x != 0 || y != -80 {
		t.Fatalf("square fill = %v %v %v %v", x, y, w, h)
	}
	fitX, fitY, fitW, fitH := CoverDestRect(0, 0, 640, 480, 16, 8)
	fillX, fillY, fillW, fillH := CoverFillRect(0, 0, 640, 480, 16, 8)
	if fillW <= fitW && fillH <= fitH {
		t.Fatalf("fill dest %v,%v,%v,%v was not larger than fit %v,%v,%v,%v", fillX, fillY, fillW, fillH, fitX, fitY, fitW, fitH)
	}
}
