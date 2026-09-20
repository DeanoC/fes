package fbgrid

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/audioreact"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/theme"
)

func TestPaintAudioChromeOffLeavesLetterbox(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	th := theme.Default()
	PaintAttract(d, AttractFrame{Width: w, Height: h, Empty: true, Theme: th})
	d.Present()
	lx, ly, ok := AttractLetterboxSample(w, h, th)
	if !ok {
		t.Fatal("letterbox")
	}
	assertBGRX(t, dst, cfg, lx, ly, th.AttractBackground.B, th.AttractBackground.G, th.AttractBackground.R, 0)
	ex, ey, ok := AudioEdgeSample(w, h)
	if !ok {
		t.Fatal("edge")
	}
	assertBGRX(t, dst, cfg, ex, ey, th.AttractBackground.B, th.AttractBackground.G, th.AttractBackground.R, 0)
}

func TestPaintAttractMeasuredLevelTintsEdge(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	th := theme.Default()
	var inj audioreact.Injector
	inj.Set(1, audioreact.KindMeasured)
	sample := inj.Sample(time.Time{})
	PaintAttract(d, AttractFrame{Width: w, Height: h, Empty: true, Theme: th, Audio: sample})
	d.Present()
	ex, ey, ok := AudioEdgeSample(w, h)
	if !ok {
		t.Fatal("edge")
	}
	want := blendOverRGBA(th.AttractBackground, th.Highlight, AudioBarAlpha(1))
	assertBGRX(t, dst, cfg, ex, ey, want.B, want.G, want.R, 0)
	lx, ly, ok := AttractLetterboxSample(w, h, th)
	if !ok {
		t.Fatal("letterbox")
	}
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(dst, cfg, lx, ly)
	if err != nil {
		t.Fatal(err)
	}
	if gotB == th.AttractBackground.B && gotG == th.AttractBackground.G && gotR == th.AttractBackground.R {
		t.Fatal("high level left letterbox untinted")
	}
}

func TestPaintAttractSyntheticAddsIdleHint(t *testing.T) {
	t.Parallel()
	th := theme.Default()
	rec := gfx.NewRecorder()
	PaintAttract(rec, AttractFrame{
		Width: 640, Height: 480, Empty: true, Theme: th,
		Audio: audioreact.Sample{Level: 0.3, Kind: audioreact.KindSynthetic},
	})
	var hint string
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && strings.Contains(c.Text, audioreact.IdleHint) {
			hint = c.Text
			break
		}
	}
	if hint == "" {
		t.Fatalf("missing idle pulse hint ops=%v", rec.Ops())
	}
	if strings.Contains(strings.ToLower(hint), "audio") {
		t.Fatalf("synthetic hint claimed audio: %q", hint)
	}
}

func TestPaintAttractMeasuredDoesNotAddIdleHint(t *testing.T) {
	t.Parallel()
	th := theme.Default()
	rec := gfx.NewRecorder()
	PaintAttract(rec, AttractFrame{
		Width: 640, Height: 480, Empty: true, Theme: th,
		Audio: audioreact.Sample{Level: 1, Kind: audioreact.KindMeasured},
	})
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && strings.Contains(c.Text, audioreact.IdleHint) {
			t.Fatalf("measured painted idle hint %q", c.Text)
		}
	}
}

func TestPaintBrowseMeasuredTintsEdgeAndZeroDoesNot(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	g := New(w, h)
	Paint(d, g)
	d.Present()
	ex, ey, ok := AudioEdgeSample(w, h)
	if !ok {
		t.Fatal("edge")
	}
	baseB, baseG, baseR, _, err := gfx.SampleBGRX(dst, cfg, ex, ey)
	if err != nil {
		t.Fatal(err)
	}
	g.Audio = audioreact.Sample{Level: 1, Kind: audioreact.KindMeasured}
	Paint(d, g)
	d.Present()
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(dst, cfg, ex, ey)
	if err != nil {
		t.Fatal(err)
	}
	if gotB == baseB && gotG == baseG && gotR == baseR {
		t.Fatal("measured level left browse edge unchanged")
	}
	g.Audio = audioreact.Sample{}
	Paint(d, g)
	d.Present()
	assertBGRX(t, dst, cfg, ex, ey, baseB, baseG, baseR, 0)
}

func TestPaintWheelAndDetailHonorAudio(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	sample := audioreact.Sample{Level: 1, Kind: audioreact.KindMeasured}
	PaintWheel(d, WheelFrame{Width: w, Height: h, Theme: th, Audio: sample})
	d.Present()
	ex, ey, ok := AudioEdgeSample(w, h)
	if !ok {
		t.Fatal("edge")
	}
	under := VignetteDim(th.Background, th.Vignette, VignetteAlphaAt(ex, ey, w, h, th.HeaderH, th.FooterH, th))
	want := blendOverRGBA(under, th.Highlight, AudioBarAlpha(1))
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(dst, cfg, ex, ey)
	if err != nil {
		t.Fatal(err)
	}
	if gotB == th.Background.B && gotG == th.Background.G && gotR == th.Background.R {
		t.Fatal("wheel edge untinted")
	}
	if gotB != want.B || gotG != want.G || gotR != want.R {
		t.Fatalf("wheel edge bgrx=%d,%d,%d want %d,%d,%d", gotB, gotG, gotR, want.B, want.G, want.R)
	}
	PaintDetail(d, DetailFrame{Width: w, Height: h, Theme: th, Audio: sample})
	d.Present()
	gotB, gotG, gotR, _, err = gfx.SampleBGRX(dst, cfg, ex, ey)
	if err != nil {
		t.Fatal(err)
	}
	if gotB == th.Background.B && gotG == th.Background.G && gotR == th.Background.R {
		t.Fatal("detail edge untinted")
	}
}

func TestAudioBarThicknessAndAlpha(t *testing.T) {
	t.Parallel()
	if AudioBarThickness(0) != 0 || AudioBarAlpha(0) != 0 {
		t.Fatal("zero")
	}
	if AudioBarThickness(1) != audioBarMax || AudioBarAlpha(1) != 176 {
		t.Fatalf("full t=%d a=%d", AudioBarThickness(1), AudioBarAlpha(1))
	}
	if AudioBarThickness(0.01) < 1 {
		t.Fatal("tiny")
	}
}

func blendOverRGBA(dst, src gfx.Color, sa uint8) color.RGBA {
	inv := uint32(255 - sa)
	return color.RGBA{
		R: uint8((uint32(src.R)*uint32(sa) + uint32(dst.R)*inv) / 255),
		G: uint8((uint32(src.G)*uint32(sa) + uint32(dst.G)*inv) / 255),
		B: uint8((uint32(src.B)*uint32(sa) + uint32(dst.B)*inv) / 255),
		A: 255,
	}
}
