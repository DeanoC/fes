package fbgrid

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/theme"
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

func TestPaintDetailVideoBadgeAndPreviewCaption(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	shot := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			shot.Set(x, y, color.RGBA{R: 32, G: 200, B: 64, A: 255})
		}
	}
	frame := DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Meta: "MEGADRIVE", Hint: "A play | B back | L/R preview",
		Shot: shot, ShotCaption: "preview 1 / 2", VideoBadge: true, Theme: th,
	}
	rec := gfx.NewRecorder()
	PaintDetail(rec, frame)
	var sawVideo, sawPreview bool
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if c.Text == "VIDEO" {
			sawVideo = true
			if c.SizePx != th.CaptionPx() {
				t.Fatalf("badge size %d", c.SizePx)
			}
		}
		if strings.Contains(c.Text, "preview") {
			sawPreview = true
		}
	}
	if !sawVideo || !sawPreview {
		t.Fatalf("video paint video=%v preview=%v ops=%v", sawVideo, sawPreview, rec.Ops())
	}

	neighbour := DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Pong",
		Hint: "A play | B back | L/R shots", Shot: shot, ShotCaption: "1 / 2", Theme: th,
	}
	stillRec := gfx.NewRecorder()
	PaintDetail(stillRec, neighbour)
	for _, c := range stillRec.Calls {
		if c.Op == "DrawText" && (c.Text == "VIDEO" || strings.Contains(c.Text, "preview")) {
			t.Fatalf("still-only painted %q", c.Text)
		}
	}

	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	PaintDetail(d, frame)
	d.Present()
	bx, by, ok := DetailVideoBadgeSample(w, h, th, frame)
	if !ok {
		t.Fatal("badge sample")
	}
	assertBGRX(t, dst, cfg, bx, by, 0, 220, 255, 0)
	sx, sy, ok := DetailShotSample(w, h, th, frame)
	if !ok {
		t.Fatal("shot sample")
	}
	assertBGRX(t, dst, cfg, sx, sy, 64, 200, 32, 0)
}

func TestPaintDetailMarqueeStripLeavesCoverMetaAndVideo(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	marquee := image.NewRGBA(image.Rect(0, 0, 80, 12))
	shot := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
			shot.Set(x, y, color.RGBA{R: 32, G: 200, B: 64, A: 255})
		}
	}
	for y := 0; y < 12; y++ {
		for x := 0; x < 80; x++ {
			marquee.Set(x, y, color.RGBA{R: 16, G: 200, B: 48, A: 255})
		}
	}
	frame := DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Meta: "MEGADRIVE  ·  1991", Hint: "A play | B back | L/R preview",
		Cover: cover, CoverKind: CoverPresent, Marquee: marquee,
		Shot: shot, ShotCaption: "preview", VideoBadge: true,
		Badges: ComposeBadges("2", "", "", false), Theme: th,
	}
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	PaintDetail(d, frame)
	d.Present()
	mx, my, ok := DetailMarqueeSample(w, h, th, marquee)
	if !ok {
		t.Fatal("marquee sample")
	}
	assertBGRX(t, dst, cfg, mx, my, 48, 200, 16, 0)
	cx, cy, ok := DetailCoverSampleFor(w, h, th, frame)
	if !ok {
		t.Fatal("cover sample")
	}
	assertBGRX(t, dst, cfg, cx, cy, 160, 32, 255, 0)
	if cy <= my {
		t.Fatalf("cover y=%d did not sit below marquee y=%d", cy, my)
	}
	_, originY, ok := DetailCoverSample(w, h, th)
	if !ok {
		t.Fatal("origin cover")
	}
	if cy <= originY {
		t.Fatalf("marquee did not push cover down origin=%d got=%d", originY, cy)
	}
	bx, by, ok := DetailVideoBadgeSample(w, h, th, frame)
	if !ok {
		t.Fatal("video badge")
	}
	assertBGRX(t, dst, cfg, bx, by, 0, 220, 255, 0)
	sx, sy, ok := DetailShotSample(w, h, th, frame)
	if !ok {
		t.Fatal("shot")
	}
	assertBGRX(t, dst, cfg, sx, sy, 64, 200, 32, 0)
	badgeX, badgeY, ok := DetailBadgeSample(w, h, th, frame)
	if !ok {
		t.Fatal("badge")
	}
	assertBGRX(t, dst, cfg, badgeX, badgeY, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)

	rec := gfx.NewRecorder()
	PaintDetail(rec, frame)
	var sawMeta, sawVideo, sawPreview bool
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if strings.Contains(c.Text, "1991") {
			sawMeta = true
		}
		if c.Text == "VIDEO" {
			sawVideo = true
		}
		if strings.Contains(c.Text, "preview") {
			sawPreview = true
		}
	}
	if !sawMeta || !sawVideo || !sawPreview {
		t.Fatalf("crushed chrome meta=%v video=%v preview=%v ops=%v", sawMeta, sawVideo, sawPreview, rec.Ops())
	}

	hidden := DetailFrame{Width: w, Height: h, Title: "Pong", Cover: cover, CoverKind: CoverPresent, Theme: th}
	PaintDetail(d, hidden)
	d.Present()
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(dst, cfg, mx, my)
	if err != nil {
		t.Fatal(err)
	}
	if gotB == 48 && gotG == 200 && gotR == 16 {
		t.Fatal("absent marquee kept banner pixels")
	}
}

func TestPaintDetailSeriesStripAndHideEmpty(t *testing.T) {
	t.Parallel()
	th := theme.Default()
	frame := DetailFrame{
		Width: 640, Height: 480, Title: "Sonic", Hint: "A play | B back | Down series",
		Theme: th, SeriesLabel: "Sonic the Hedgehog",
		Series: []Tile{
			{Name: "SONIC 2", Color: gfx.RGB(40, 90, 200)},
			{Name: "SONIC 3", Color: gfx.RGB(40, 180, 80)},
		},
	}
	rec := gfx.NewRecorder()
	PaintDetail(rec, frame)
	var sawLabel, sawSonic2 bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Sonic the Hedgehog" && c.SizePx == th.Complete().CaptionPx() {
			sawLabel = true
		}
		if c.Op == "DrawText" && strings.Contains(c.Text, "SONIC 2") {
			sawSonic2 = true
		}
		if c.Op == "DebugText" {
			t.Fatalf("series DebugText %+v", c)
		}
	}
	if !sawLabel {
		t.Fatalf("missing series label ops=%v", rec.Ops())
	}
	_ = sawSonic2
	hidden := DetailFrame{Width: 640, Height: 480, Title: "Pong", Theme: th}
	hideRec := gfx.NewRecorder()
	PaintDetail(hideRec, hidden)
	for _, c := range hideRec.Calls {
		if c.Op == "DrawText" && (c.Text == "Series" || c.Text == "Sonic the Hedgehog") {
			t.Fatalf("hidden series painted %q", c.Text)
		}
	}
	frame.SeriesActive = true
	frame.SeriesFocus = 1
	sx, sy, ok := DetailSeriesHighlightSample(frame)
	if !ok {
		t.Fatal("series highlight sample")
	}
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	PaintDetail(d, frame)
	d.Present()
	assertBGRX(t, dst, cfg, sx, sy, th.Complete().Highlight.B, th.Complete().Highlight.G, th.Complete().Highlight.R, 0)
}

func TestPaintDetailMarqueeTopSeriesBottom(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	marquee := image.NewRGBA(image.Rect(0, 0, 80, 12))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	for y := 0; y < 12; y++ {
		for x := 0; x < 80; x++ {
			marquee.Set(x, y, color.RGBA{R: 16, G: 200, B: 48, A: 255})
		}
	}
	frame := DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Meta: "MEGADRIVE", Hint: "A play | B back | Down series",
		Cover: cover, CoverKind: CoverPresent, Marquee: marquee,
		Theme: th, SeriesLabel: "Sonic the Hedgehog",
		Series: []Tile{
			{Name: "SONIC 2", Color: gfx.RGB(40, 90, 200)},
			{Name: "SONIC 3", Color: gfx.RGB(40, 180, 80)},
		},
		SeriesActive: true, SeriesFocus: 0,
	}
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	PaintDetail(d, frame)
	d.Present()
	mx, my, ok := DetailMarqueeSample(w, h, th, marquee)
	if !ok {
		t.Fatal("marquee sample")
	}
	assertBGRX(t, dst, cfg, mx, my, 48, 200, 16, 0)
	cx, cy, ok := DetailCoverSampleFor(w, h, th, frame)
	if !ok {
		t.Fatal("cover sample")
	}
	assertBGRX(t, dst, cfg, cx, cy, 160, 32, 255, 0)
	sx, sy, ok := DetailSeriesHighlightSample(frame)
	if !ok {
		t.Fatal("series sample")
	}
	assertBGRX(t, dst, cfg, sx, sy, th.Complete().Highlight.B, th.Complete().Highlight.G, th.Complete().Highlight.R, 0)
	if cy <= my {
		t.Fatalf("cover y=%d did not sit below marquee y=%d", cy, my)
	}
	if sy <= cy {
		t.Fatalf("series y=%d did not sit below cover y=%d", sy, cy)
	}
	if sy <= my {
		t.Fatalf("series y=%d did not sit below marquee y=%d", sy, my)
	}
	rec := gfx.NewRecorder()
	PaintDetail(rec, frame)
	var sawSeries bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Sonic the Hedgehog" && c.SizePx == th.Complete().CaptionPx() {
			sawSeries = true
		}
	}
	if !sawSeries {
		t.Fatalf("missing series label ops=%v", rec.Ops())
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
