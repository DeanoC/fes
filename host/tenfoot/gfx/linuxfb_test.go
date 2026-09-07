package gfx

import (
	"image/color"
	"runtime"
	"testing"
)

func TestLinuxFBPresentBlitsSoftware(t *testing.T) {
	cfg := FBConfig{Width: 8, Height: 4, Stride: 8*4 + 4, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := NewLinuxFB(8, 4, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.BackendName() != BackendLinuxFB {
		t.Fatalf("backend %q", d.BackendName())
	}
	d.BeginFrame()
	d.Clear(RGB(255, 0, 0))
	d.SetBlend(BlendNone)
	d.FillRect(Rect{X: 2, Y: 1, W: 2, H: 1}, RGB(0, 0, 255))
	d.Present()
	assertBGRX(t, dst, cfg, 0, 0, 0, 0, 255, 0)
	assertBGRX(t, dst, cfg, 2, 1, 255, 0, 0, 0)
	snap := d.Snapshot()
	assertRGBA(t, snap, 0, 0, color.RGBA{255, 0, 0, 255})
	assertRGBA(t, snap, 2, 1, color.RGBA{0, 0, 255, 255})
}

func TestPaintLinuxFBSpikePattern(t *testing.T) {
	const w, h = 640, 480
	cfg := FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	PaintLinuxFBSpike(d, w, h)
	d.Present()
	snap := d.Snapshot()
	// Color bars occupy the top two thirds; each bar is 80px wide.
	assertRGBA(t, snap, 40, 40, color.RGBA{255, 255, 255, 255})
	assertRGBA(t, snap, 120, 40, color.RGBA{255, 255, 0, 255})
	assertRGBA(t, snap, 200, 40, color.RGBA{0, 255, 255, 255})
	assertRGBA(t, snap, 280, 40, color.RGBA{0, 255, 0, 255})
	assertRGBA(t, snap, 360, 40, color.RGBA{255, 0, 255, 255})
	assertRGBA(t, snap, 440, 40, color.RGBA{255, 0, 0, 255})
	assertRGBA(t, snap, 520, 40, color.RGBA{0, 0, 255, 255})
	assertRGBA(t, snap, 600, 40, color.RGBA{0, 0, 0, 255})
	assertBGRX(t, dst, cfg, 40, 40, 255, 255, 255, 0)
	assertBGRX(t, dst, cfg, 440, 40, 0, 0, 255, 0)
	assertBGRX(t, dst, cfg, 520, 40, 255, 0, 0, 0)
	// Debugfont "FOGCAST" is opaque light pixels on a dark backing rect.
	found := 0
	for y := 320; y < h; y++ {
		for x := 0; x < w; x++ {
			p := snap.RGBAAt(x, y)
			if p.R == 236 && p.G == 240 && p.B == 248 {
				found++
			}
		}
	}
	if found < 100 {
		t.Fatalf("expected FOGCAST debugfont pixels, found %d", found)
	}
}

func TestOpenLinuxFBNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("OpenLinuxFB talks to a real device on linux")
	}
	if _, err := OpenLinuxFB("/dev/fb0"); err == nil {
		t.Fatal("expected error on non-linux")
	}
}

func TestNewLinuxFBRejectsShortDest(t *testing.T) {
	cfg := FBConfig{Width: 2, Height: 2, Stride: 8, BPP: 32}
	if _, err := NewLinuxFB(2, 2, make([]byte, 4), cfg); err == nil {
		t.Fatal("expected short dest")
	}
}
