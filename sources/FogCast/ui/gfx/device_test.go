package gfx

import (
	"image"
	"image/color"
	"testing"
)

func TestRecorderCreateUpdateDestroy(t *testing.T) {
	r := NewRecorder()
	img := opaqueRGBA(8, 4)
	tex, err := r.CreateRGBA(img)
	if err != nil {
		t.Fatal(err)
	}
	if !tex.Valid() || tex.Width() != 8 || tex.Height() != 4 {
		t.Fatalf("texture %+v", tex)
	}
	if !r.Alive(tex) {
		t.Fatal("expected alive")
	}
	if err := r.UpdateRGBA(tex, opaqueRGBA(8, 4)); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateRGBA(tex, opaqueRGBA(4, 4)); err == nil {
		t.Fatal("expected size mismatch")
	}
	r.Destroy(tex)
	if r.Alive(tex) {
		t.Fatal("destroyed texture still alive")
	}
	if r.AliveCount() != 0 {
		t.Fatalf("alive %d", r.AliveCount())
	}
}

func TestRecorderRejectsEmptyCreate(t *testing.T) {
	r := NewRecorder()
	if _, err := r.CreateRGBA(nil); err == nil {
		t.Fatal("expected error")
	}
	empty := image.NewRGBA(image.Rect(0, 0, 0, 0))
	if _, err := r.CreateRGBA(empty); err == nil {
		t.Fatal("expected error")
	}
}

func TestRecorderRecordsFrame(t *testing.T) {
	r := NewRecorder()
	img := opaqueRGBA(2, 2)
	tex, err := r.CreateRGBA(img)
	if err != nil {
		t.Fatal(err)
	}
	r.BeginFrame()
	r.Clear(RGB(12, 14, 20))
	r.SetBlend(BlendAlpha)
	r.FillRect(Rect{X: 1, Y: 2, W: 3, H: 4}, RGBA(8, 8, 12, 180))
	r.SetBlend(BlendNone)
	r.Draw(tex, nil, Rect{X: 10, Y: 20, W: 30, H: 40})
	src := Rect{X: 0, Y: 0, W: 1, H: 1}
	r.Draw(tex, &src, Rect{X: 0, Y: 0, W: 2, H: 2})
	r.DebugText(24, 18, "FOGCAST", 3)
	r.Present()
	want := []string{
		"CreateRGBA", "BeginFrame", "Clear", "SetBlend", "FillRect",
		"SetBlend", "Draw", "Draw", "DebugText", "Present",
	}
	got := r.Ops()
	if len(got) != len(want) {
		t.Fatalf("ops %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ops[%d]=%s want %s (%v)", i, got[i], want[i], got)
		}
	}
	if r.Calls[2].Color != RGB(12, 14, 20) {
		t.Fatalf("clear color %+v", r.Calls[2].Color)
	}
	if r.Calls[6].Src != nil {
		t.Fatal("full-texture draw should pass nil src")
	}
	if r.Calls[7].Src == nil || r.Calls[7].Src.W != 1 {
		t.Fatalf("src rect %+v", r.Calls[7].Src)
	}
}

func opaqueRGBA(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	return img
}
