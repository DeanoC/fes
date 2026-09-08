package gfx

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/color"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	h, err := NewHeader(64, 48)
	if err != nil {
		t.Fatal(err)
	}
	src := Rect{X: 1, Y: 2, W: 3, H: 4}
	pix := bytes.Repeat([]byte{9, 8, 7, 255}, 4)
	cmds := []Command{
		{Op: OpBeginFrame, Frame: 1},
		{Op: OpClear, Color: RGB(12, 14, 20)},
		{Op: OpSetBlend, Blend: BlendAlpha},
		{Op: OpFillRect, Dst: Rect{X: 2, Y: 3, W: 4, H: 5}, Color: RGBA(1, 2, 3, 4)},
		{Op: OpCreateTexture, TexID: 7, Width: 2, Height: 2, Format: FormatRGBA8, Pixels: pix, CRC32: crc32.ChecksumIEEE(pix)},
		{Op: OpDraw, TexID: 7, Dst: Rect{X: 0, Y: 0, W: 8, H: 8}},
		{Op: OpDraw, TexID: 7, Src: &src, Dst: Rect{X: 10, Y: 11, W: 12, H: 13}},
		{Op: OpUpdateTexture, TexID: 7, Width: 2, Height: 2, Format: FormatRGBA8, Pixels: pix, CRC32: crc32.ChecksumIEEE(pix)},
		{Op: OpDebugText, X: -3, Y: 9, Scale: 2, Text: "FC2D"},
		{Op: OpDrawText, X: 4, Y: 5, SizePx: 16, Color: RGB(255, 240, 220), Text: "KIT"},
		{Op: OpDestroyTexture, TexID: 7},
		{Op: OpPresent},
		{Op: OpClose},
	}
	raw, err := EncodeStream(h, cmds)
	if err != nil {
		t.Fatal(err)
	}
	gotH, got, err := DecodeStream(raw)
	if err != nil {
		t.Fatal(err)
	}
	if gotH != h {
		t.Fatalf("header %+v want %+v", gotH, h)
	}
	if len(got) != len(cmds) {
		t.Fatalf("len %d want %d", len(got), len(cmds))
	}
	for i, want := range cmds {
		c := got[i]
		if c.Op != want.Op {
			t.Fatalf("cmd %d op %s want %s", i, c.Op, want.Op)
		}
		switch want.Op {
		case OpBeginFrame:
			if c.Frame != want.Frame {
				t.Fatalf("frame %d", c.Frame)
			}
		case OpClear, OpFillRect:
			if c.Color != want.Color || c.Dst != want.Dst {
				t.Fatalf("cmd %d %+v", i, c)
			}
		case OpSetBlend:
			if c.Blend != want.Blend {
				t.Fatalf("blend %v", c.Blend)
			}
		case OpDraw:
			if c.TexID != want.TexID || c.Dst != want.Dst {
				t.Fatalf("draw %+v", c)
			}
			if want.Src == nil {
				if c.Src != nil {
					t.Fatalf("draw %d src should be nil", i)
				}
			} else if c.Src == nil || *c.Src != *want.Src {
				t.Fatalf("draw src %+v want %+v", c.Src, want.Src)
			}
		case OpCreateTexture, OpUpdateTexture:
			if c.TexID != want.TexID || c.Width != want.Width || c.Height != want.Height {
				t.Fatalf("tex %+v", c)
			}
			if !bytes.Equal(c.Pixels, want.Pixels) || c.CRC32 != want.CRC32 {
				t.Fatalf("pixels/crc cmd %d", i)
			}
		case OpDebugText:
			if c.X != want.X || c.Y != want.Y || c.Scale != want.Scale || c.Text != want.Text {
				t.Fatalf("text %+v", c)
			}
		case OpDrawText:
			if c.X != want.X || c.Y != want.Y || c.SizePx != want.SizePx || c.Text != want.Text || c.Color != want.Color {
				t.Fatalf("drawtext %+v", c)
			}
		case OpDestroyTexture:
			if c.TexID != want.TexID {
				t.Fatalf("destroy %d", c.TexID)
			}
		}
	}
}

func TestDecodeRejectsBadMagicAndVersion(t *testing.T) {
	h, err := NewHeader(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := EncodeStream(h, nil)
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte{}, raw...)
	bad[0] = 'X'
	if _, _, err := DecodeStream(bad); err == nil {
		t.Fatal("expected bad magic")
	}
	ver := append([]byte{}, raw...)
	ver[4] = 99
	if _, _, err := DecodeStream(ver); err == nil {
		t.Fatal("expected bad version")
	}
	if _, _, err := DecodeStream(raw[:10]); err == nil {
		t.Fatal("expected short header")
	}
}

func TestDecodeRejectsCRCMismatch(t *testing.T) {
	h, _ := NewHeader(2, 2)
	pix := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	raw, err := EncodeStream(h, []Command{{
		Op: OpCreateTexture, TexID: 1, Width: 2, Height: 2, Format: FormatRGBA8,
		Pixels: pix, CRC32: crc32.ChecksumIEEE(pix),
	}})
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if _, _, err := DecodeStream(raw); err == nil {
		t.Fatal("expected crc error")
	}
}

func TestEncodeExtendedTextureLength(t *testing.T) {
	h, _ := NewHeader(8, 8)
	w, ht := 256, 128 // 128KiB pixels, forces extended length
	pix := make([]byte, w*ht*4)
	raw, err := EncodeStream(h, []Command{{
		Op: OpCreateTexture, TexID: 1, Width: w, Height: ht, Format: FormatRGBA8, Pixels: pix,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if binaryU16(raw[16+2:16+4]) != cmdLenExt {
		t.Fatalf("expected extended length marker, got %d", binaryU16(raw[16+2:16+4]))
	}
	gotH, cmds, err := DecodeStream(raw)
	if err != nil {
		t.Fatal(err)
	}
	if gotH.LogicalW != 8 || len(cmds) != 1 || cmds[0].Width != w || len(cmds[0].Pixels) != len(pix) {
		t.Fatalf("extended decode %+v %+v", gotH, cmds)
	}
}

func TestReplayMatchesSoftware(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{0, 0, 255, 255})

	direct, err := NewSoftware(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	tex, err := direct.CreateRGBA(src)
	if err != nil {
		t.Fatal(err)
	}
	direct.BeginFrame()
	direct.Clear(RGB(10, 20, 30))
	direct.SetBlend(BlendNone)
	direct.FillRect(Rect{X: 6, Y: 0, W: 2, H: 1}, RGB(0, 255, 0))
	direct.Draw(tex, nil, Rect{X: 0, Y: 0, W: 4, H: 2})
	direct.Present()
	want := direct.Snapshot()

	h, _ := NewHeader(8, 4)
	_, _, pix, err := tightRGBA(src)
	if err != nil {
		t.Fatal(err)
	}
	cmds := []Command{
		{Op: OpCreateTexture, TexID: 1, Width: 2, Height: 1, Format: FormatRGBA8, Pixels: pix, CRC32: crc32.ChecksumIEEE(pix)},
		{Op: OpBeginFrame, Frame: 1},
		{Op: OpClear, Color: RGB(10, 20, 30)},
		{Op: OpSetBlend, Blend: BlendNone},
		{Op: OpFillRect, Dst: Rect{X: 6, Y: 0, W: 2, H: 1}, Color: RGB(0, 255, 0)},
		{Op: OpDraw, TexID: 1, Dst: Rect{X: 0, Y: 0, W: 4, H: 2}},
		{Op: OpPresent},
	}
	raw, err := EncodeStream(h, cmds)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := NewSoftware(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayBytes(replay, raw); err != nil {
		t.Fatal(err)
	}
	got := replay.Snapshot()
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("replay pixels differ from direct software")
	}
}

func binaryU16(p []byte) uint16 {
	return uint16(p[0]) | uint16(p[1])<<8
}
