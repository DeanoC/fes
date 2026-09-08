package fbgrid

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestPaintDetailDrawsTitleCoverAndHint(t *testing.T) {
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
	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST  SNES 1/1",
		Title: "Mario", Meta: "SNES  ·  1985  ·  Platform",
		Hint: "A play | B back", Cover: cover, CoverKind: CoverPresent,
		Color: th.SystemColor("snes"), Theme: th,
	})
	d.Present()
	cx, cy, ok := DetailCoverSample(w, h, th)
	if !ok {
		t.Fatal("cover sample")
	}
	assertBGRX(t, dst, cfg, cx, cy, 160, 32, 255, 0)

	rec := gfx.NewRecorder()
	PaintDetail(rec, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Mario",
		Meta: "SNES  ·  1985", Hint: "A play | B back", Theme: th,
	})
	var sawTitle bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Mario" && c.SizePx == th.TitlePx() {
			if c.Weight != th.TitleWeight() {
				t.Fatalf("title weight %s want %s", c.Weight, th.TitleWeight())
			}
			sawTitle = true
		}
		if c.Op == "DrawText" && c.Text == "A play | B back" && c.Weight != th.StatusWeight() {
			t.Fatalf("hint weight %s want %s", c.Weight, th.StatusWeight())
		}
		if c.Op == "DebugText" {
			t.Fatalf("detail used DebugText: %+v", rec.Ops())
		}
	}
	if !sawTitle {
		t.Fatalf("missing title DrawText ops=%v", rec.Ops())
	}
	if th.TitleWeight() != gfx.WeightBold {
		t.Fatal("default detail title should be bold")
	}

	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Title: "Missing", CoverKind: CoverMissing,
		Color: th.SystemColor("snes"), Theme: th,
	})
	d.Present()
	px, py, ok := DetailCoverSample(w, h, th)
	if !ok {
		t.Fatal("placeholder sample")
	}
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(dst, cfg, px, py)
	if err != nil {
		t.Fatal(err)
	}
	sys := th.SystemColor("snes")
	if gotB == sys.B && gotG == sys.G && gotR == sys.R {
		t.Fatal("missing cover stayed a flat system fill")
	}
	panel := PlaceholderPanel(sys, th, false)
	if gotB != panel.B || gotG != panel.G || gotR != panel.R {
		t.Fatalf("placeholder bgrx %d,%d,%d want %d,%d,%d", gotB, gotG, gotR, panel.B, panel.G, panel.R)
	}
}

func TestPaintDetailLogoReplacesTitleText(t *testing.T) {
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
	logo := image.NewRGBA(image.Rect(0, 0, 48, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 48; x++ {
			logo.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Meta: "MEGADRIVE  ·  1991", Hint: "A play | B back",
		Logo: logo, Theme: th,
	})
	d.Present()
	sx, sy, ok := DetailLogoSample(w, h, th, logo)
	if !ok {
		t.Fatal("logo sample")
	}
	assertBGRX(t, dst, cfg, sx, sy, 160, 32, 255, 0)

	rec := gfx.NewRecorder()
	PaintDetail(rec, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Meta: "MEGADRIVE  ·  1991", Logo: logo, Theme: th,
	})
	var sawTitle bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Sonic" {
			sawTitle = true
		}
	}
	if sawTitle {
		t.Fatal("detail still drew title text over logo")
	}

	rec = gfx.NewRecorder()
	PaintDetail(rec, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Pong",
		Meta: "PONG", Theme: th,
	})
	sawTitle = false
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Pong" && c.SizePx == th.TitlePx() {
			sawTitle = true
		}
	}
	if !sawTitle {
		t.Fatal("detail without logo dropped title text")
	}
}

func TestPaintDetailWrapsMetaAndDescription(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	coverX, coverY, ok := DetailCoverSample(w, h, th)
	if !ok {
		t.Fatal("cover sample")
	}
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	desc := "A blue hedgehog dashes through Green Hill Zone, collecting rings and leaping loops before Robotnik can catch him."
	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Meta:        "MEGADRIVE  ·  1991  ·  Platform  ·  SEGA  ·  1-2  ·  USA",
		Description: desc, Hint: "A play | B back",
		Cover: cover, CoverKind: CoverPresent, Theme: th,
	})
	d.Present()
	assertBGRX(t, dst, cfg, coverX, coverY, 160, 32, 255, 0)

	rec := gfx.NewRecorder()
	PaintDetail(rec, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Meta:        "MEGADRIVE  ·  1991  ·  Platform  ·  SEGA  ·  1-2  ·  USA",
		Description: desc, Hint: "A play | B back", Theme: th,
	})
	var sawMeta, sawHint bool
	var descLines int
	var joinedDesc strings.Builder
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if c.Text == "A play | B back" {
			sawHint = true
			if c.Y+gfx.TextHeightWeight(c.SizePx, c.Weight) > h {
				t.Fatalf("hint overflow y=%d", c.Y)
			}
		}
		if c.SizePx == th.BodyPx() && strings.Contains(c.Text, "1991") {
			sawMeta = true
			if !strings.Contains(c.Text, "USA") && !strings.Contains(c.Text, "Platform") && !strings.Contains(c.Text, "SEGA") {
				// wrapped onto this or another body line
			}
			if c.Weight != th.BodyWeight() {
				t.Fatalf("meta weight %s", c.Weight)
			}
		}
		if c.SizePx == th.CaptionPx() && c.Weight == th.CaptionWeight() {
			descLines++
			joinedDesc.WriteString(c.Text)
			joinedDesc.WriteByte(' ')
		}
	}
	bodyJoined := drawTextJoin(rec, th.BodyPx())
	if !sawMeta || !strings.Contains(bodyJoined, "1991") || !strings.Contains(bodyJoined, "USA") || !strings.Contains(bodyJoined, "1-2") {
		t.Fatalf("missing meta facts ops=%v joined=%q", rec.Ops(), bodyJoined)
	}
	if !sawHint {
		t.Fatal("missing footer hint")
	}
	gotDesc := joinedDesc.String()
	if descLines < 2 || !strings.Contains(gotDesc, "hedgehog") {
		t.Fatalf("description lines=%d text=%q", descLines, gotDesc)
	}
}

func TestPaintDetailOmitsEmptyDescription(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	rec := gfx.NewRecorder()
	PaintDetail(rec, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Pong",
		Meta: "PONG", Hint: "A play | B back", Theme: th,
	})
	if got := descriptionCopy(rec, th); got != "" {
		t.Fatalf("empty description still painted %q", got)
	}
	var sawMeta bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "PONG" && c.SizePx == th.BodyPx() {
			sawMeta = true
		}
	}
	if !sawMeta {
		t.Fatal("meta vanished when description omitted")
	}

	rec = gfx.NewRecorder()
	PaintDetail(rec, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Pong",
		Hint: "A play | B back", Theme: th,
	})
	if got := descriptionCopy(rec, th); got != "" {
		t.Fatalf("empty copy painted %q", got)
	}
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.SizePx == th.BodyPx() && c.Text != "" {
			t.Fatalf("empty meta painted %q", c.Text)
		}
	}
}

func descriptionCopy(rec *gfx.Recorder, th theme.Theme) string {
	var b strings.Builder
	for _, c := range rec.Calls {
		if c.Op != "DrawText" || c.SizePx != th.CaptionPx() || c.Weight != th.CaptionWeight() {
			continue
		}
		if strings.TrimSpace(c.Text) == "" || utf8.RuneCountInString(c.Text) <= 1 {
			continue
		}
		b.WriteString(c.Text)
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

func drawTextJoin(rec *gfx.Recorder, sizePx int) string {
	var b strings.Builder
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.SizePx == sizePx {
			b.WriteString(c.Text)
			b.WriteByte(' ')
		}
	}
	return b.String()
}
