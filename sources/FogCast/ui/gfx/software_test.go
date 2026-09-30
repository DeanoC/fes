package gfx

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"math/rand"
	"strings"
	"testing"
)

func TestSoftwareFillAndClear(t *testing.T) {
	s, err := NewSoftware(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if s.BackendName() != BackendSoftware {
		t.Fatalf("backend %q", s.BackendName())
	}
	s.BeginFrame()
	s.Clear(RGB(10, 20, 30))
	s.SetBlend(BlendNone)
	s.FillRect(Rect{X: 2, Y: 1, W: 3, H: 2}, RGB(200, 0, 0))
	s.Present()
	snap := s.Snapshot()
	assertRGBA(t, snap, 0, 0, color.RGBA{10, 20, 30, 255})
	assertRGBA(t, snap, 2, 1, color.RGBA{200, 0, 0, 255})
	assertRGBA(t, snap, 4, 2, color.RGBA{200, 0, 0, 255})
	assertRGBA(t, snap, 5, 1, color.RGBA{10, 20, 30, 255})
	assertRGBA(t, snap, 2, 3, color.RGBA{10, 20, 30, 255})
}

func TestSoftwareFillAlpha(t *testing.T) {
	s, err := NewSoftware(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(RGB(0, 0, 0))
	s.SetBlend(BlendAlpha)
	s.FillRect(Rect{X: 0, Y: 0, W: 1, H: 1}, RGBA(255, 0, 0, 128))
	s.SetBlend(BlendNone)
	s.FillRect(Rect{X: 1, Y: 0, W: 1, H: 1}, RGBA(0, 255, 0, 128))
	snap := s.Snapshot()
	got := snap.RGBAAt(0, 0)
	if got.R != 128 || got.G != 0 || got.B != 0 || got.A != 255 {
		t.Fatalf("blended %+v", got)
	}
	got = snap.RGBAAt(1, 0)
	if got != (color.RGBA{0, 255, 0, 128}) {
		t.Fatalf("replace %+v", got)
	}
}

func TestSoftwareBlitNearest(t *testing.T) {
	s, err := NewSoftware(4, 2)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(RGB(0, 0, 0))
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{0, 0, 255, 255})
	tex, err := s.CreateRGBA(src)
	if err != nil {
		t.Fatal(err)
	}
	// 2× nearest: each source pixel covers two destination pixels.
	s.Draw(tex, nil, Rect{X: 0, Y: 0, W: 4, H: 2})
	snap := s.Snapshot()
	assertRGBA(t, snap, 0, 0, color.RGBA{255, 0, 0, 255})
	assertRGBA(t, snap, 1, 0, color.RGBA{255, 0, 0, 255})
	assertRGBA(t, snap, 2, 0, color.RGBA{0, 0, 255, 255})
	assertRGBA(t, snap, 3, 1, color.RGBA{0, 0, 255, 255})
}

func TestSoftwareBlitSrcRectAndAlpha(t *testing.T) {
	s, err := NewSoftware(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(RGB(0, 255, 0))
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{9, 9, 9, 255})
	src.SetRGBA(1, 0, color.RGBA{255, 0, 0, 128})
	tex, err := s.CreateRGBA(src)
	if err != nil {
		t.Fatal(err)
	}
	sr := Rect{X: 1, Y: 0, W: 1, H: 1}
	s.Draw(tex, &sr, Rect{X: 0, Y: 0, W: 1, H: 1})
	snap := s.Snapshot()
	got := snap.RGBAAt(0, 0)
	if got.R != 128 || got.G != 127 || got.B != 0 || got.A != 255 {
		t.Fatalf("src-rect blend %+v", got)
	}
	assertRGBA(t, snap, 1, 0, color.RGBA{0, 255, 0, 255})
}

func TestSoftwareCreateUpdateDestroy(t *testing.T) {
	s, err := NewSoftware(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRGBA(nil); err == nil {
		t.Fatal("expected empty create error")
	}
	tex, err := s.CreateRGBA(opaqueRGBA(2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if !tex.Valid() || tex.Width() != 2 || tex.Height() != 2 {
		t.Fatalf("texture %+v", tex)
	}
	if err := s.UpdateRGBA(tex, opaqueRGBA(2, 2)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRGBA(tex, opaqueRGBA(3, 3)); err == nil {
		t.Fatal("expected size mismatch")
	}
	s.Destroy(tex)
	s.Draw(tex, nil, Rect{X: 0, Y: 0, W: 2, H: 2})
	s.Close()
}

func TestSoftwareDebugTextWritesPixels(t *testing.T) {
	s, err := NewSoftware(32, 16)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(RGB(0, 0, 0))
	s.DebugText(0, 0, "A", 1)
	snap := s.Snapshot()
	found := false
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if snap.RGBAAt(x, y) == (color.RGBA{236, 240, 248, 255}) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected debug glyph pixels")
	}
}

func TestParseBackend(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", BackendSDL},
		{"sdl", BackendSDL},
		{"SDL3", BackendSDL},
		{"software", BackendSoftware},
		{"sw", BackendSoftware},
		{"fpga-stub", BackendFPGAStub},
		{"stub", BackendFPGAStub},
		{"FPGA", BackendFPGA},
		{"fpga", BackendFPGA},
		{"fpga-recorder", BackendFPGA},
		{"linuxfb", BackendLinuxFB},
		{"fb0", BackendLinuxFB},
		{"menu-display", BackendMenuDisplay},
		{"menudisplay", BackendMenuDisplay},
		{"MENU", BackendMenuDisplay},
	}
	for _, tc := range cases {
		got, err := ParseBackend(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("ParseBackend(%q)=%q %v want %q", tc.in, got, err, tc.want)
		}
	}
	_, err := ParseBackend("opengl")
	if err == nil || !strings.Contains(err.Error(), "menu-display") {
		t.Fatalf("expected unknown backend error, got %v", err)
	}
	if _, err := OpenCPU("menu-display", 8, 8); err == nil || !strings.Contains(err.Error(), "not a CPU device") {
		t.Fatalf("menu-display CPU device err = %v", err)
	}
}

func TestSoftwareClipsOutOfBounds(t *testing.T) {
	s, err := NewSoftware(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(RGB(1, 2, 3))
	s.FillRect(Rect{X: -10, Y: -10, W: 2, H: 2}, RGB(255, 0, 0))
	s.FillRect(Rect{X: 3, Y: 3, W: 8, H: 8}, RGB(0, 255, 0))
	snap := s.Snapshot()
	assertRGBA(t, snap, 0, 0, color.RGBA{1, 2, 3, 255})
	assertRGBA(t, snap, 3, 3, color.RGBA{0, 255, 0, 255})
}

func BenchmarkSoftwareFill(b *testing.B) {
	s, err := NewSoftware(1280, 720)
	if err != nil {
		b.Fatal(err)
	}
	s.SetBlend(BlendNone)
	r := Rect{X: 0, Y: 0, W: 1280, H: 720}
	c := RGB(12, 14, 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.FillRect(r, c)
	}
}

func BenchmarkSoftwareBlit(b *testing.B) {
	s, err := NewSoftware(1280, 720)
	if err != nil {
		b.Fatal(err)
	}
	tex, err := s.CreateRGBA(opaqueRGBA(256, 256))
	if err != nil {
		b.Fatal(err)
	}
	dst := Rect{X: 100, Y: 100, W: 256, H: 256}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Draw(tex, nil, dst)
	}
}

func assertRGBA(t *testing.T, img *image.RGBA, x, y int, want color.RGBA) {
	t.Helper()
	got := img.RGBAAt(x, y)
	if got != want {
		t.Fatalf("pixel (%d,%d)=%+v want %+v", x, y, got, want)
	}
}

// Compare optimized paths to the original scalar raster rules over fractional
// rectangles, source clipping, sub-images, mixed alpha, and negative positions.
func TestSoftwareDrawMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(318))
	for _, opaque := range []bool{false, true} {
		parent := image.NewRGBA(image.Rect(-7, -5, 26, 24))
		for i := 0; i < len(parent.Pix); i += 4 {
			parent.Pix[i], parent.Pix[i+1], parent.Pix[i+2] = uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256))
			parent.Pix[i+3] = 255
			if !opaque {
				parent.Pix[i+3] = uint8(rng.Intn(256))
			}
		}
		img := parent.SubImage(image.Rect(-3, -2, 20, 17)).(*image.RGBA)
		s, _ := NewSoftware(31, 27)
		tex, err := s.CreateRGBA(img)
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; n < 600; n++ {
			sr := Rect{X: float32(rng.Intn(38)-12) / 2, Y: float32(rng.Intn(34)-10) / 2, W: float32(rng.Intn(50)+1) / 2, H: float32(rng.Intn(44)+1) / 2}
			dst := Rect{X: float32(rng.Intn(60)-20) / 2, Y: float32(rng.Intn(60)-20) / 2, W: float32(rng.Intn(80)+1) / 2, H: float32(rng.Intn(70)+1) / 2}
			if n%2 == 0 {
				sr.X = float32(int(sr.X))
				sr.Y = float32(int(sr.Y))
				dst.X = float32(int(dst.X))
				dst.Y = float32(int(dst.Y))
				dst.W = sr.W
				dst.H = sr.H
			}
			var src *Rect = &sr
			if n%11 == 0 {
				src = nil
			}
			for i := range s.pix {
				s.pix[i] = uint8(rng.Intn(256))
			}
			initial := append([]byte(nil), s.pix...)
			want := s.Snapshot()
			scalarTextureDraw(want, img, src, dst)
			s.Draw(tex, src, dst)
			if !bytes.Equal(s.pix, want.Pix) {
				t.Fatalf("opaque=%v case=%d src=%+v dst=%+v differ", opaque, n, src, dst)
			}
			copy(s.pix, initial)
			s.Draw(tex, src, dst)
			if !bytes.Equal(s.pix, want.Pix) {
				t.Fatalf("admitted resize opaque=%v case=%d src=%+v dst=%+v differ", opaque, n, src, dst)
			}
		}
	}
}

func scalarTextureDraw(dstImg, srcImg *image.RGBA, src *Rect, dst Rect) {
	w, h := srcImg.Bounds().Dx(), srcImg.Bounds().Dy()
	sr := Rect{W: float32(w), H: float32(h)}
	if src != nil {
		sr = *src
	}
	x0, y0, x1, y1, ok := clipRect(dst, dstImg.Bounds().Dx(), dstImg.Bounds().Dy())
	if !ok {
		return
	}
	unscaled := dst.W == sr.W && dst.H == sr.H && isInt(dst.X) && isInt(dst.Y) && isInt(sr.X) && isInt(sr.Y)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			var sx, sy int
			if unscaled {
				sx = int(sr.X) + x - int(dst.X)
				sy = int(sr.Y) + y - int(dst.Y)
				if sx < 0 || sx >= w || sy < 0 || sy >= h {
					continue
				}
			} else {
				sx = int(math.Floor(float64(sr.X + (float32(x)+0.5-dst.X)*(sr.W/dst.W))))
				sy = int(math.Floor(float64(sr.Y + (float32(y)+0.5-dst.Y)*(sr.H/dst.H))))
				if sx < 0 {
					sx = 0
				}
				if sx >= w {
					sx = w - 1
				}
				if sy < 0 {
					sy = 0
				}
				if sy >= h {
					sy = h - 1
				}
			}
			off := srcImg.PixOffset(srcImg.Rect.Min.X+sx, srcImg.Rect.Min.Y+sy)
			pix := srcImg.Pix[off : off+4]
			blendOver(dstImg.Pix, dstImg.PixOffset(x, y), pix[0], pix[1], pix[2], pix[3])
		}
	}
}

func TestSoftwareUpdateRefreshesOpacity(t *testing.T) {
	s, _ := NewSoftware(2, 2)
	img := opaqueRGBA(2, 2)
	tex, err := s.CreateRGBA(img)
	if err != nil {
		t.Fatal(err)
	}
	if !s.lookup(tex).opaque {
		t.Fatal("opaque upload misclassified")
	}
	img.Pix[3] = 128
	if err := s.UpdateRGBA(tex, img); err != nil {
		t.Fatal(err)
	}
	if s.lookup(tex).opaque {
		t.Fatal("update retained stale opaque classification")
	}
	s.Clear(RGB(50, 100, 150))
	want := s.Snapshot()
	dst := Rect{W: 2, H: 2}
	scalarTextureDraw(want, img, nil, dst)
	s.Draw(tex, nil, dst)
	if !bytes.Equal(s.pix, want.Pix) {
		t.Fatal("transparent update did not blend")
	}
	img.Pix[3] = 255
	if err := s.UpdateRGBA(tex, img); err != nil {
		t.Fatal(err)
	}
	if !s.lookup(tex).opaque {
		t.Fatal("opaque update retained stale transparent classification")
	}
	// Texture uploads retain their own bytes, including the opacity classification.
	img.Pix[3] = 0
	if s.lookup(tex).pix[3] != 255 {
		t.Fatal("texture aliases caller bytes")
	}
}

func TestSoftwareAlphaFillAllChannelValues(t *testing.T) {
	s, _ := NewSoftware(256, 1)
	s.SetBlend(BlendAlpha)
	rect := Rect{W: 256, H: 1}
	// Exhaust every source-channel, source-alpha and destination-channel value.
	// Each destination channel (including alpha) uses a permutation of 0..255.
	for a := 0; a < 256; a++ {
		for source := 0; source < 256; source++ {
			c := RGBA(uint8(source), uint8(255-source), uint8(source^85), uint8(a))
			for d := 0; d < 256; d++ {
				i := d * 4
				s.pix[i], s.pix[i+1], s.pix[i+2], s.pix[i+3] = uint8(d), uint8(255-d), uint8(d^170), uint8(d^85)
			}
			s.FillRect(rect, c)
			for d := 0; d < 256; d++ {
				inputs := [4]int{d, 255 - d, d ^ 170, d ^ 85}
				channels := [3]int{source, 255 - source, source ^ 85}
				for channel := 0; channel < 4; channel++ {
					want := a + inputs[channel]*(255-a)/255
					if channel < 3 {
						want = (channels[channel]*a + inputs[channel]*(255-a)) / 255
					}
					if s.pix[d*4+channel] != uint8(want) {
						t.Fatalf("source=%v dest=%v channel=%d got=%d want=%d", c, inputs, channel, s.pix[d*4+channel], want)
					}
				}
			}
		}
	}
}

func TestSoftwareScaledCacheInvalidationAndLimits(t *testing.T) {
	s, _ := NewSoftware(1280, 720)
	img := opaqueRGBA(128, 128)
	tex, _ := s.CreateRGBA(img)
	dst := Rect{X: 1, Y: 2, W: 384, H: 384}
	s.Draw(tex, nil, dst)
	if len(s.scaled) != 0 {
		t.Fatal("first resize allocated a cache entry")
	}
	s.Draw(tex, nil, dst)
	if len(s.scaled) != 1 {
		t.Fatal("repeated resize not cached")
	}
	first := s.scaled[0].bitmap
	s.Draw(tex, nil, dst)
	if s.scaled[0].bitmap != first {
		t.Fatal("resize not reused")
	}
	img.Pix[0], img.Pix[3] = 200, 128
	if err := s.UpdateRGBA(tex, img); err != nil {
		t.Fatal(err)
	}
	if len(s.scaled) != 0 || s.scaledBytes != 0 {
		t.Fatal("updated texture retained stale resizes")
	}
	s.Draw(tex, nil, dst)
	if len(s.scaled) != 0 {
		t.Fatal("updated source admitted on first sighting")
	}
	s.Draw(tex, nil, dst)
	if s.scaled[0].bitmap.opaque {
		t.Fatal("updated alpha lost")
	}
	s.Clear(RGBA(40, 60, 80, 100))
	want := s.Snapshot()
	scalarTextureDraw(want, img, nil, dst)
	s.Draw(tex, nil, dst)
	if !bytes.Equal(s.pix, want.Pix) {
		t.Fatal("updated resize differs from scalar")
	}
	for i := 0; i < 80; i++ {
		s.Draw(tex, nil, Rect{X: float32(i), W: 384, H: 384})
		s.Draw(tex, nil, Rect{X: float32(i), W: 384, H: 384})
	}
	if s.scaledBytes > scaledBitmapBudget || len(s.scaled) > scaledBitmapLimit {
		t.Fatal("resize cache exceeded budget")
	}
	if s.scaled[len(s.scaled)-1].dst.X == 0 {
		t.Fatal("least recently used resize not evicted")
	}
	s.Destroy(tex)
	if len(s.scaled) != 0 || s.scaledBytes != 0 {
		t.Fatal("destroy retained resizes")
	}
	tex, _ = s.CreateRGBA(opaqueRGBA(128, 128))
	fractional := Rect{X: -10.25, Y: 4.75, W: 384.25, H: 384.75}
	s.Draw(tex, nil, fractional)
	if len(s.scaled) != 0 {
		t.Fatal("fractional resize admitted on first draw")
	}
	s.Clear(RGBA(20, 40, 60, 80))
	want = s.Snapshot()
	scalarTextureDraw(want, opaqueRGBA(128, 128), nil, fractional)
	s.Draw(tex, nil, fractional)
	if len(s.scaled) != 1 || !bytes.Equal(s.pix, want.Pix) {
		t.Fatal("fractional cached resize differs")
	}
	fractionalEntry := s.scaled[0].bitmap
	s.Draw(tex, nil, fractional)
	if s.scaled[0].bitmap != fractionalEntry {
		t.Fatal("fractional cache not reused")
	}
	clipped := Rect{W: 2, H: 100000}
	s.Draw(tex, nil, clipped)
	s.Draw(tex, nil, clipped)
	if s.scaled[0].bitmap.w != 2 || s.scaled[0].bitmap.h != 720 {
		t.Fatal("cache stored invisible pixels")
	}
	s.invalidateScaled(tex.id)
	large, _ := NewSoftware(2048, 2048)
	largeTex, _ := large.CreateRGBA(opaqueRGBA(128, 128))
	large.Draw(largeTex, nil, Rect{W: 4096, H: 4096})
	large.Draw(largeTex, nil, Rect{W: 4096, H: 4096})
	if len(large.scaled) != 0 {
		t.Fatal("over-budget visible resize cached")
	}
	for i := 0; i < 80; i++ {
		s.Draw(tex, nil, Rect{X: float32(i), W: 32, H: 32})
		s.Draw(tex, nil, Rect{X: float32(i), W: 32, H: 32})
	}
	if len(s.scaled) != scaledBitmapLimit || s.scaledBytes != scaledBitmapLimit*32*32*4 {
		t.Fatalf("small resize entry cap: count=%d bytes=%d", len(s.scaled), s.scaledBytes)
	}
	s.Draw(tex, nil, Rect{X: 20, W: 32, H: 32})
	s.Draw(tex, nil, Rect{X: 80, W: 32, H: 32})
	s.Draw(tex, nil, Rect{X: 80, W: 32, H: 32})
	if s.scaled[1].dst.X != 20 || s.scaled[len(s.scaled)-1].dst.X != 17 {
		t.Fatal("cache hit did not promote entry before eviction")
	}
}

func TestSoftwareScaledCacheDoesNotAdmitStreamingOrMovingSprites(t *testing.T) {
	s, _ := NewSoftware(1280, 720)
	img := opaqueRGBA(256, 256)
	tex, _ := s.CreateRGBA(img)
	dst := Rect{X: 100, Y: 100, W: 384, H: 384}
	for i := 0; i < 10; i++ {
		img.Pix[0] = uint8(i * 10)
		if err := s.UpdateRGBA(tex, img); err != nil {
			t.Fatal(err)
		}
		s.Clear(RGBA(30, 40, 50, 60))
		want := s.Snapshot()
		scalarTextureDraw(want, img, nil, dst)
		s.Draw(tex, nil, dst)
		if !bytes.Equal(s.pix, want.Pix) {
			t.Fatal("streamed pixels differ")
		}
		if len(s.scaled) != 0 || s.scaledBytes != 0 {
			t.Fatal("streaming resize allocated cache entry")
		}
	}
	for i := 0; i < 20; i++ {
		dst.X = float32(i * 3)
		s.Draw(tex, nil, dst)
		if len(s.scaled) != 0 {
			t.Fatal("moving sprite allocated cache entry")
		}
	}
	s.Draw(tex, nil, dst)
	if len(s.scaled) != 1 {
		t.Fatal("sprite did not admit after stopping")
	}
}
