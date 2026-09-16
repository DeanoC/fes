package gfx

import (
	"image/color"
	"testing"
)

func TestFPGAStubConstructionAndIsStub(t *testing.T) {
	f, err := NewFPGAStub(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !f.IsStub() {
		t.Fatal("expected stub")
	}
	if f.BackendName() != BackendFPGAStub {
		t.Fatalf("backend %q", f.BackendName())
	}
	f.BeginFrame()
	f.Clear(RGB(12, 14, 20))
	f.SetBlend(BlendNone)
	f.FillRect(Rect{X: 1, Y: 1, W: 2, H: 1}, RGB(9, 8, 7))
	f.Present()
	snap := f.Snapshot()
	if snap.Bounds().Dx() != 8 || snap.Bounds().Dy() != 4 {
		t.Fatalf("size %v", snap.Bounds())
	}
	assertRGBA(t, snap, 0, 0, color.RGBA{12, 14, 20, 255})
	assertRGBA(t, snap, 1, 1, color.RGBA{9, 8, 7, 255})
	f.Close()
}

func TestFPGAStubDelegatesBlit(t *testing.T) {
	f, err := NewFPGAStub(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	src := opaqueRGBA(1, 1)
	src.SetRGBA(0, 0, color.RGBA{1, 2, 3, 255})
	tex, err := f.CreateRGBA(src)
	if err != nil {
		t.Fatal(err)
	}
	f.Clear(RGB(0, 0, 0))
	f.Draw(tex, nil, Rect{X: 0, Y: 0, W: 2, H: 1})
	snap := f.Snapshot()
	assertRGBA(t, snap, 0, 0, color.RGBA{1, 2, 3, 255})
	assertRGBA(t, snap, 1, 0, color.RGBA{1, 2, 3, 255})
	f.Destroy(tex)
}
