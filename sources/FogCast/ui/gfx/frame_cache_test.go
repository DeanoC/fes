package gfx

import (
	"bytes"
	"image"
	"testing"
)

func TestFrameCacheMatchesImmediateAndSkipsStaticFrames(t *testing.T) {
	plain, _ := NewSoftware(64, 48)
	cached, _ := NewSoftware(64, 48)
	cache := NewFrameCache(cached)
	image := opaqueRGBA(12, 10)
	a, _ := plain.CreateRGBA(image)
	b, _ := cache.CreateRGBA(image)
	scene := func(d Device, tex Texture, focus int) {
		d.BeginFrame()
		d.Clear(RGB(10, 20, 30))
		d.SetBlend(BlendAlpha)
		d.FillRect(Rect{X: float32(focus), Y: 1, W: 30, H: 20}, RGBA(100, 200, 40, 96))
		d.Draw(tex, &Rect{X: 1, Y: 0, W: 10, H: 10}, Rect{X: 5, Y: 3, W: 20, H: 20})
		d.DrawTextWeight(2, 30, "Go", 12, WeightBold, RGB(230, 240, 255))
		d.SetBlend(BlendNone)
		d.Present()
	}
	for _, focus := range []int{0, 0, 3, 3, 0} {
		scene(plain, a, focus)
		scene(cache, b, focus)
		if !bytes.Equal(plain.Framebuffer().Pix, cached.Framebuffer().Pix) {
			t.Fatalf("pixels differ at focus %d", focus)
		}
	}
	if cache.revision != 3 {
		t.Fatalf("rendered %d times, want 3", cache.revision)
	}
}

func TestFrameCacheInvalidatesTextureUpdatesAndRemovedDraws(t *testing.T) {
	recorder := NewRecorder()
	cache := NewFrameCache(recorder)
	tex, _ := cache.CreateRGBA(opaqueRGBA(4, 4))
	scene := func(draw bool) {
		cache.BeginFrame()
		cache.Clear(RGB(0, 0, 0))
		if draw {
			cache.Draw(tex, nil, Rect{W: 4, H: 4})
		}
		cache.Present()
	}
	scene(true)
	scene(true)
	if cache.revision != 1 {
		t.Fatal("identical commands rasterized")
	}
	if err := cache.UpdateRGBA(tex, opaqueRGBA(4, 4)); err != nil {
		t.Fatal(err)
	}
	scene(true)
	if cache.revision != 2 {
		t.Fatal("updated texture skipped")
	}
	scene(false)
	if cache.revision != 3 {
		t.Fatal("removed draw skipped")
	}
}

func TestFrameCacheIncrementalFramesAlwaysRender(t *testing.T) {
	software, _ := NewSoftware(2, 2)
	cache := NewFrameCache(software)
	software.Clear(RGB(0, 0, 0))
	for i := 0; i < 2; i++ {
		cache.BeginFrame()
		cache.SetBlend(BlendAlpha)
		cache.FillRect(Rect{W: 2, H: 2}, RGBA(255, 0, 0, 128))
		cache.Present()
	}
	if software.Framebuffer().Pix[0] != 191 {
		t.Fatalf("incremental alpha=%d", software.Framebuffer().Pix[0])
	}
}

func TestFrameCacheTextureMutationPreservesDrawOrder(t *testing.T) {
	software, _ := NewSoftware(2, 1)
	cache := NewFrameCache(software)
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Pix = []byte{255, 0, 0, 255}
	tex, _ := cache.CreateRGBA(img)
	cache.BeginFrame()
	cache.Clear(RGB(0, 0, 0))
	cache.Draw(tex, nil, Rect{W: 1, H: 1})
	img.Pix = []byte{0, 255, 0, 255}
	cache.UpdateRGBA(tex, img)
	cache.Draw(tex, nil, Rect{X: 1, W: 1, H: 1})
	cache.Destroy(tex)
	cache.Present()
	if !bytes.Equal(software.Framebuffer().Pix, []byte{255, 0, 0, 255, 0, 255, 0, 255}) {
		t.Fatalf("draw order %v", software.Framebuffer().Pix)
	}
}

func BenchmarkFrameCacheStaticScene(b *testing.B) {
	software, _ := NewSoftware(1280, 720)
	cache := NewFrameCache(software)
	tex, _ := cache.CreateRGBA(opaqueRGBA(128, 128))
	scene := func() {
		cache.BeginFrame()
		cache.Clear(RGB(10, 20, 30))
		for i := 0; i < 12; i++ {
			cache.Draw(tex, nil, Rect{X: float32(i % 4 * 240), Y: float32(i / 4 * 200), W: 180, H: 180})
		}
		cache.DrawText(20, 650, "Choose a game", 24, RGB(255, 255, 255))
		cache.Present()
	}
	scene()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scene()
	}
}

func TestFrameCacheMutationAfterDrawInvalidatesNextFrame(t *testing.T) {
	for _, destroy := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "destroy"}[destroy], func(t *testing.T) {
			software, _ := NewSoftware(1, 1)
			cache := NewFrameCache(software)
			img := image.NewRGBA(image.Rect(0, 0, 1, 1))
			img.Pix = []byte{255, 0, 0, 255}
			tex, _ := cache.CreateRGBA(img)
			frame := func() { cache.BeginFrame(); cache.Clear(RGB(0, 0, 0)); cache.Draw(tex, nil, Rect{W: 1, H: 1}) }
			frame()
			if destroy {
				cache.Destroy(tex)
			} else {
				img.Pix = []byte{0, 255, 0, 255}
				if err := cache.UpdateRGBA(tex, img); err != nil {
					t.Fatal(err)
				}
			}
			cache.Present()
			if software.Framebuffer().Pix[0] != 255 {
				t.Fatal("mutation changed earlier draw")
			}
			frame()
			cache.Present()
			want := []byte{0, 255, 0, 255}
			if destroy {
				want = []byte{0, 0, 0, 255}
			}
			if !bytes.Equal(software.Framebuffer().Pix, want) {
				t.Fatalf("stale cached texture after mutation: %v", software.Framebuffer().Pix)
			}
		})
	}
}

type backdropPainter struct {
	*Software
	clears int
}

func (d *backdropPainter) Clear(color Color) { d.clears++; d.Software.Clear(color) }

func TestFrameCacheBackdropRestoresPixelsAndInvalidates(t *testing.T) {
	plain, _ := NewSoftware(64, 48)
	sw, _ := NewSoftware(64, 48)
	painter := &backdropPainter{Software: sw}
	cache := NewFrameCache(painter)
	img := opaqueRGBA(8, 8)
	a, _ := plain.CreateRGBA(img)
	b, _ := cache.CreateRGBA(img)
	scene := func(d Device, tex Texture, base, overlay int, checkpoint bool) {
		d.BeginFrame()
		d.SetBlend(BlendNone)
		d.Clear(RGB(uint8(base), 20, 30))
		d.Draw(tex, nil, Rect{W: 32, H: 32})
		d.SetBlend(BlendAlpha)
		d.FillRect(Rect{W: 64, H: 48}, RGBA(8, 8, 12, 180))
		if checkpoint {
			if c, ok := d.(interface{ CacheBackdrop() }); ok {
				c.CacheBackdrop()
			}
		}
		// Moving translucent overlays expose pixels painted by the previous frame.
		d.FillRect(Rect{X: float32(overlay), Y: 5, W: 12, H: 12}, RGBA(200, 80, 10, 128))
		d.SetBlend(BlendNone)
		d.Present()
	}
	check := func(base, overlay int, checkpoint bool) {
		t.Helper()
		scene(plain, a, base, overlay, checkpoint)
		scene(cache, b, base, overlay, checkpoint)
		if !bytes.Equal(plain.fb.Pix, sw.fb.Pix) {
			t.Fatal("backdrop pixels differ")
		}
	}
	check(10, 0, true)
	check(10, 5, true)
	check(10, 15, true)
	if painter.clears != 1 {
		t.Fatalf("unchanged backdrop rasterized %d times", painter.clears)
	}
	check(20, 20, true)
	if painter.clears != 2 {
		t.Fatal("changed backdrop reused")
	}
	img.Pix[0] ^= 255
	plain.UpdateRGBA(a, img)
	cache.UpdateRGBA(b, img)
	check(20, 25, true)
	if painter.clears != 3 {
		t.Fatal("updated texture reused")
	}
	// A changed overlay resource must not evict a backdrop that never draws it.
	unrelated, _ := cache.CreateRGBA(opaqueRGBA(1, 1))
	cache.UpdateRGBA(unrelated, opaqueRGBA(1, 1))
	check(20, 26, true)
	cache.Destroy(unrelated)
	check(20, 27, true)
	if painter.clears != 3 {
		t.Fatal("overlay-only mutation evicted backdrop")
	}
	check(20, 30, false)
	check(20, 35, true)
	check(20, 35, true)
	if painter.clears != 5 {
		t.Fatalf("close/reopen or identical frame rasterized incorrectly: %d", painter.clears)
	}
}

func TestBackdropMutationDuringFramePreservesOrder(t *testing.T) {
	for _, beforeCheckpoint := range []bool{false, true} {
		plain, _ := NewSoftware(4, 2)
		sw, _ := NewSoftware(4, 2)
		cache := NewFrameCache(sw)
		red := image.NewRGBA(image.Rect(0, 0, 1, 1))
		red.Pix = []byte{255, 0, 0, 255}
		green := image.NewRGBA(image.Rect(0, 0, 1, 1))
		green.Pix = []byte{0, 255, 0, 255}
		a, _ := plain.CreateRGBA(red)
		b, _ := cache.CreateRGBA(red)
		scene := func(d Device, tex Texture, mutate bool) {
			d.BeginFrame()
			d.Clear(RGB(0, 0, 0))
			d.Draw(tex, nil, Rect{W: 1, H: 1})
			if mutate && beforeCheckpoint {
				d.UpdateRGBA(tex, green)
			}
			if c, ok := d.(interface{ CacheBackdrop() }); ok {
				c.CacheBackdrop()
			}
			if mutate && !beforeCheckpoint {
				d.UpdateRGBA(tex, green)
			}
			d.Draw(tex, nil, Rect{X: 2, W: 1, H: 1})
			d.Present()
		}
		for _, mutate := range []bool{true, false, false} {
			scene(plain, a, mutate)
			scene(cache, b, mutate)
			if !bytes.Equal(plain.fb.Pix, sw.fb.Pix) {
				t.Fatalf("mutation beforeCheckpoint=%v mutate=%v", beforeCheckpoint, mutate)
			}
		}
	}
}

func TestBackdropDamageBoundsAndRestore(t *testing.T) {
	bounds := image.Rect(0, 0, 16, 12)
	for _, tc := range []struct {
		name     string
		commands []frameCommand
		want     image.Rectangle
	}{
		{"empty", nil, image.Rectangle{}},
		{"blend", []frameCommand{{op: OpSetBlend}}, image.Rectangle{}},
		{"clipped", []frameCommand{{op: OpFillRect, dst: Rect{X: -2.5, Y: 1.5, W: 8, H: 5}}}, image.Rect(0, 1, 6, 7)},
		{"outside", []frameCommand{{op: OpDraw, dst: Rect{X: 20, W: 3, H: 3}}}, image.Rectangle{}},
		{"union", []frameCommand{{op: OpFillRect, dst: Rect{X: 2, Y: 3, W: 4, H: 4}}, {op: OpDraw, dst: Rect{X: 10, Y: 8, W: 3, H: 2}}}, image.Rect(2, 3, 13, 10)},
		{"text", []frameCommand{{op: OpDrawText}}, bounds},
		{"debug", []frameCommand{{op: OpDebugText}}, bounds},
		{"clear", []frameCommand{{op: OpClear}}, bounds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := backdropDamage(tc.commands, bounds)
			if got != tc.want {
				t.Fatalf("bounds %v want %v", got, tc.want)
			}
			fb := image.NewRGBA(bounds)
			for i := range fb.Pix {
				fb.Pix[i] = 99
			}
			cache := &FrameCache{backdropDamage: got, backdropPixels: make([]byte, len(fb.Pix))}
			cache.restoreBackdrop(fb)
			for y := 0; y < 12; y++ {
				for x := 0; x < 16; x++ {
					want := uint8(99)
					if image.Pt(x, y).In(got) {
						want = 0
					}
					for c := 0; c < 4; c++ {
						if fb.Pix[fb.PixOffset(x, y)+c] != want {
							t.Fatalf("restored outside bounds or missed (%d,%d)", x, y)
						}
					}
				}
			}
		})
	}
}

func TestBackdropRestoreErasesRemovedTextAndClippedOverlays(t *testing.T) {
	plain, _ := NewSoftware(64, 48)
	sw, _ := NewSoftware(64, 48)
	cache := NewFrameCache(sw)
	scene := func(d Device, frame int) {
		d.BeginFrame()
		d.SetBlend(BlendNone)
		d.Clear(RGB(20, 30, 40))
		if c, ok := d.(interface{ CacheBackdrop() }); ok {
			c.CacheBackdrop()
		}
		d.SetBlend(BlendAlpha)
		d.FillRect(Rect{X: float32(frame*9-15) + 0.5, Y: 5.5, W: 15, H: 10}, RGBA(200, 80, 10, 128))
		if frame == 0 {
			d.DrawText(0, 30, "Wide overlay", 14, RGB(255, 255, 255))
		}
		if frame == 1 {
			d.DebugText(20, 40, "HUD", 1)
		}
		d.SetBlend(BlendNone)
		d.Present()
	}
	for i := 0; i < 8; i++ {
		scene(plain, i)
		scene(cache, i)
		if !bytes.Equal(plain.fb.Pix, sw.fb.Pix) {
			t.Fatalf("overlay removal frame %d differs", i)
		}
	}
}
