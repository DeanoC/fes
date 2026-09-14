package gfx

import (
	"testing"
)

func TestParseFBSysfsMiSTerGeometry(t *testing.T) {
	cfg, err := ParseFBSysfs("640,480\n", "32\n", "2560\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 640 || cfg.Height != 480 || cfg.Stride != 2560 || cfg.BPP != 32 {
		t.Fatalf("cfg %+v", cfg)
	}
	if cfg.String() != "640x480 stride=2560 bpp=32" {
		t.Fatalf("string %q", cfg.String())
	}
}

func TestValidateFBConfigRejects(t *testing.T) {
	if err := ValidateFBConfig(FBConfig{Width: 640, Height: 480, Stride: 2560, BPP: 16}); err == nil {
		t.Fatal("expected 16bpp reject")
	}
	if err := ValidateFBConfig(FBConfig{Width: 640, Height: 480, Stride: 1000, BPP: 32}); err == nil {
		t.Fatal("expected short stride reject")
	}
	if _, err := ParseFBSysfs("nope", "32", "2560"); err == nil {
		t.Fatal("expected virtual_size reject")
	}
}

func TestBlitRGBAHonorsStrideAndBGRX(t *testing.T) {
	const w, h = 4, 2
	srcStride := w * 4
	src := make([]byte, srcStride*h)
	// (0,0) red, (1,0) green, (2,0) blue, (3,0) white
	putRGBA(src, srcStride, 0, 0, 255, 0, 0, 255)
	putRGBA(src, srcStride, 1, 0, 0, 255, 0, 255)
	putRGBA(src, srcStride, 2, 0, 0, 0, 255, 255)
	putRGBA(src, srcStride, 3, 0, 255, 255, 255, 255)
	const extra = 8
	cfg := FBConfig{Width: w, Height: h, Stride: w*4 + extra, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	for i := range dst {
		dst[i] = 0xAA
	}
	if err := BlitRGBA(dst, cfg, src, w, h, srcStride); err != nil {
		t.Fatal(err)
	}
	assertBGRX(t, dst, cfg, 0, 0, 0, 0, 255, 0)
	assertBGRX(t, dst, cfg, 1, 0, 0, 255, 0, 0)
	assertBGRX(t, dst, cfg, 2, 0, 255, 0, 0, 0)
	assertBGRX(t, dst, cfg, 3, 0, 255, 255, 255, 0)
	// Padding after the visible pixels on row 0 stays 0xAA.
	pad := dst[w*4 : cfg.Stride]
	for i, b := range pad {
		if b != 0xAA {
			t.Fatalf("pad[%d]=%#x", i, b)
		}
	}
}

func TestBlitRGBAMiSTerPackedStride(t *testing.T) {
	cfg := FBConfig{Width: 640, Height: 480, Stride: 2560, BPP: 32}
	src := make([]byte, 640*4*480)
	putRGBA(src, 640*4, 0, 0, 12, 34, 56, 255)
	putRGBA(src, 640*4, 639, 479, 1, 2, 3, 255)
	dst := make([]byte, cfg.Height*cfg.Stride)
	if err := BlitRGBA(dst, cfg, src, 640, 480, 640*4); err != nil {
		t.Fatal(err)
	}
	assertBGRX(t, dst, cfg, 0, 0, 56, 34, 12, 0)
	assertBGRX(t, dst, cfg, 639, 479, 3, 2, 1, 0)
}

func TestBlitRGBARejectsShortBuffers(t *testing.T) {
	cfg := FBConfig{Width: 2, Height: 2, Stride: 8, BPP: 32}
	src := make([]byte, 8)
	dst := make([]byte, 8)
	if err := BlitRGBA(dst, cfg, src, 2, 2, 8); err == nil {
		t.Fatal("expected short source")
	}
	src = make([]byte, 16)
	if err := BlitRGBA(dst, cfg, src, 2, 2, 8); err == nil {
		t.Fatal("expected short dest")
	}
}

func putRGBA(pix []byte, stride, x, y int, r, g, b, a byte) {
	off := y*stride + x*4
	pix[off] = r
	pix[off+1] = g
	pix[off+2] = b
	pix[off+3] = a
}

func assertBGRX(t *testing.T, dst []byte, cfg FBConfig, x, y int, b, g, r, xx byte) {
	t.Helper()
	gotB, gotG, gotR, gotX, err := SampleBGRX(dst, cfg, x, y)
	if err != nil {
		t.Fatal(err)
	}
	if gotB != b || gotG != g || gotR != r || gotX != xx {
		t.Fatalf("(%d,%d)=%d,%d,%d,%d want %d,%d,%d,%d", x, y, gotB, gotG, gotR, gotX, b, g, r, xx)
	}
}
