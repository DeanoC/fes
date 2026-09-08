package gfx

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestFPGAConstructionIsStub(t *testing.T) {
	f, err := NewFPGA(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.BackendName() != BackendFPGA {
		t.Fatalf("backend %q", f.BackendName())
	}
	if !f.IsStub() {
		t.Fatal("expected stub until hardware exists")
	}
	if f.RasterMode() != RasterSoftwareReplay {
		t.Fatalf("raster %q", f.RasterMode())
	}
	if f.BackendName() == BackendFPGAStub {
		t.Fatal("fpga must be distinct from fpga-stub")
	}
}

func TestFPGARecordsAndRasters(t *testing.T) {
	f, err := NewFPGA(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src := opaqueRGBA(2, 1)
	src.SetRGBA(0, 0, color.RGBA{9, 8, 7, 255})
	src.SetRGBA(1, 0, color.RGBA{1, 2, 3, 255})
	tex, err := f.CreateRGBA(src)
	if err != nil {
		t.Fatal(err)
	}
	f.BeginFrame()
	f.Clear(RGB(12, 14, 20))
	f.SetBlend(BlendNone)
	f.FillRect(Rect{X: 6, Y: 0, W: 2, H: 1}, RGB(200, 0, 0))
	f.Draw(tex, nil, Rect{X: 0, Y: 0, W: 4, H: 2})
	f.DebugText(0, 10, "A", 1)
	f.Present()
	f.Destroy(tex)

	snap := f.Snapshot()
	assertRGBA(t, snap, 0, 0, color.RGBA{9, 8, 7, 255})
	assertRGBA(t, snap, 2, 0, color.RGBA{1, 2, 3, 255})
	assertRGBA(t, snap, 0, 1, color.RGBA{9, 8, 7, 255})
	assertRGBA(t, snap, 6, 0, color.RGBA{200, 0, 0, 255})
	assertRGBA(t, snap, 7, 3, color.RGBA{12, 14, 20, 255})

	ops := f.Commands()
	want := []Op{
		OpCreateTexture, OpBeginFrame, OpClear, OpSetBlend, OpFillRect,
		OpDraw, OpDebugText, OpPresent, OpDestroyTexture,
	}
	if len(ops) != len(want) {
		t.Fatalf("ops %v", ops)
	}
	for i := range want {
		if ops[i].Op != want[i] {
			t.Fatalf("ops[%d]=%s want %s", i, ops[i].Op, want[i])
		}
	}

	raw, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	sw, err := NewSoftware(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	h, err := ReplayBytes(sw, raw)
	if err != nil {
		t.Fatal(err)
	}
	if h.LogicalW != 8 || h.LogicalH != 4 || h.Flags&FlagSoftwareReplay == 0 {
		t.Fatalf("header %+v", h)
	}
	if h.Flags&FlagHardware != 0 {
		t.Fatal("hardware flag must stay clear")
	}
	got := sw.Snapshot()
	if !bytes.Equal(got.Pix, snap.Pix) {
		t.Fatal("replay pixels differ from FPGA software raster")
	}
}

func TestFPGAUpdateAndSrcRect(t *testing.T) {
	f, err := NewFPGA(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{9, 9, 9, 255})
	img.SetRGBA(1, 0, color.RGBA{255, 0, 0, 128})
	tex, err := f.CreateRGBA(img)
	if err != nil {
		t.Fatal(err)
	}
	img.SetRGBA(1, 0, color.RGBA{0, 0, 255, 255})
	if err := f.UpdateRGBA(tex, img); err != nil {
		t.Fatal(err)
	}
	f.Clear(RGB(0, 255, 0))
	sr := Rect{X: 1, Y: 0, W: 1, H: 1}
	f.Draw(tex, &sr, Rect{X: 0, Y: 0, W: 1, H: 1})
	snap := f.Snapshot()
	assertRGBA(t, snap, 0, 0, color.RGBA{0, 0, 255, 255})
	assertRGBA(t, snap, 1, 0, color.RGBA{0, 255, 0, 255})

	raw, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	_, cmds, err := DecodeStream(raw)
	if err != nil {
		t.Fatal(err)
	}
	var sawSrc bool
	for _, c := range cmds {
		if c.Op == OpDraw && c.Src != nil && *c.Src == sr {
			sawSrc = true
		}
	}
	if !sawSrc {
		t.Fatal("expected src rect in stream")
	}
}

func TestOpenCPUSelectsFPGA(t *testing.T) {
	d, err := OpenCPU("fpga", 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := d.(*FPGA)
	if !ok {
		t.Fatalf("type %T", d)
	}
	if f.BackendName() != BackendFPGA || !f.IsStub() {
		t.Fatalf("fpga %+v", f)
	}
	stub, err := OpenCPU("fpga-stub", 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if stub.(*FPGAStub).BackendName() != BackendFPGAStub {
		t.Fatal("stub backend")
	}
	if _, err := OpenCPU("sdl", 4, 2); err == nil {
		t.Fatal("sdl is not a CPU device")
	}
}
