package gfx

import (
	"image"
	"image/color"
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
		{"FPGA", BackendFPGAStub},
	}
	for _, tc := range cases {
		got, err := ParseBackend(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("ParseBackend(%q)=%q %v want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ParseBackend("opengl"); err == nil {
		t.Fatal("expected unknown backend error")
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
