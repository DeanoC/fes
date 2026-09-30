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
