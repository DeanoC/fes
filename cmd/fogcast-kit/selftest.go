package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
	"github.com/DeanoC/FogCast/kitlauncher"
	"github.com/DeanoC/FogCast/kitlauncher/controller"
	"github.com/DeanoC/FogCast/remoteinput"
)

func runFPGASelftest(fbPath string) error {
	report, err := anim.RunFPGAProof()
	fmt.Print(report)
	if err != nil {
		return err
	}
	if strings.TrimSpace(fbPath) == "" {
		return nil
	}
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		fmt.Printf("linuxfb skip: %v HW=not-yet\n", err)
		return nil
	}
	defer d.Close()
	cfg := d.Config()
	fpga, err := gfx.NewFPGA(cfg.Width, cfg.Height)
	if err != nil {
		return err
	}
	defer fpga.Close()
	anim.PaintStillCycle(fpga, cfg.Width, cfg.Height, anim.StillHold/2)
	if err := gfx.Replay(d, fpga.Commands()); err != nil {
		return err
	}
	d.Present()
	fmt.Printf("linuxfb present %s kit-backend=%s fpga-backend=%s stub=%v raster=%s HW=not-yet\n",
		cfg, d.BackendName(), fpga.BackendName(), fpga.IsStub(), fpga.RasterMode())
	return nil
}

func runPadsSelftest(remap *inputmap.Remapper) error {
	if remap == nil {
		remap = inputmap.IdentityRemapper()
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	mapped := remap.Apply(a)
	fmt.Printf("profile=%s a->%d identity=%v\n", remap.Profile().Name, mapped.Code, mapped.Code == remoteinput.ButtonA)
	hub, err := controller.OpenWith(remap)
	if err != nil {
		return err
	}
	defer hub.Close()
	devs := hub.Devices()
	fmt.Printf("pads=%d\n", len(devs))
	for i, d := range devs {
		fmt.Printf("pad[%d] id=%s name=%q\n", i, d.ID, d.Name)
	}
	events, err := hub.Poll()
	if err != nil {
		return err
	}
	fmt.Printf("poll_events=%d\n", len(events))
	fmt.Printf("selftest-pads PASS\n")
	return nil
}

func runNavSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseNavGrid(d, th)
	fmt.Print(report)
	return err
}

func runThemeSelftest(fbPath string) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseThemeGrid(d)
	fmt.Print(report)
	return err
}

func runShelfSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseShelfGrid(d, th)
	fmt.Print(report)
	return err
}

func runTextSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseTextGrid(d, th)
	fmt.Print(report)
	return err
}

func runCoverSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseCoverGrid(d, th)
	fmt.Print(report)
	return err
}

func exerciseNavGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	games := make([]tenfoot.Game, 25)
	for i := range games {
		games[i] = tenfoot.Game{
			ID:         fmt.Sprintf("game-%02d", i),
			Title:      fmt.Sprintf("Title %02d", i),
			System:     "snes",
			Launchable: true,
		}
	}
	m := kitlauncher.Model{Games: games, Connected: true, TargetReady: true, ControllerConnected: true}
	var b strings.Builder
	step := func(name string, wantFocus, wantPage int) error {
		g := paintModel(d, m, th)
		start, end := catalogPage(m.Focus, len(m.Games))
		hx, hy, ok := g.HighlightSample()
		if !ok {
			return fmt.Errorf("%s: no highlight", name)
		}
		gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), d.Config(), hx, hy)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s focus=%d page=%d:%d highlight=(%d,%d) bgrx=%d,%d,%d,%d local=%d theme=%s\n",
			name, m.Focus, start, end, hx, hy, gotB, gotG, gotR, gotX, g.Focus, th.Name)
		if m.Focus != wantFocus {
			return fmt.Errorf("%s: focus %d want %d", name, m.Focus, wantFocus)
		}
		if start != wantPage {
			return fmt.Errorf("%s: page start %d want %d", name, start, wantPage)
		}
		if gotB != th.Highlight.B || gotG != th.Highlight.G || gotR != th.Highlight.R || gotX != 0 {
			return fmt.Errorf("%s: highlight bgrx %d,%d,%d,%d want %d,%d,%d,0", name, gotB, gotG, gotR, gotX, th.Highlight.B, th.Highlight.G, th.Highlight.R)
		}
		return nil
	}
	if err := step("origin", 0, 0); err != nil {
		return b.String(), err
	}
	press(&m, "dpad-right")
	if err := step("right", 1, 0); err != nil {
		return b.String(), err
	}
	for i := 0; i < 8; i++ {
		press(&m, "dpad-right")
	}
	if err := step("row-clamp", 3, 0); err != nil {
		return b.String(), err
	}
	press(&m, "dpad-down")
	if err := step("down-row", 7, 0); err != nil {
		return b.String(), err
	}
	m.Focus = 11
	press(&m, "dpad-down")
	if err := step("page-cross", 15, 12); err != nil {
		return b.String(), err
	}
	m.Focus = 12
	right, _ := remoteinput.NormalizeAxis("left-x", 32767)
	m.Input(right, time.Now())
	if err := step("stick-right", 13, 12); err != nil {
		return b.String(), err
	}
	m.Input(right, time.Now())
	if m.Focus != 13 {
		return b.String(), fmt.Errorf("held stick repeated focus %d", m.Focus)
	}
	fmt.Fprintf(&b, "selftest-nav PASS\n")
	return b.String(), nil
}

func paintModel(d *gfx.LinuxFB, m kitlauncher.Model, th theme.Theme) fbgrid.Grid {
	cfg := d.Config()
	g := modelGrid(m, cfg.Width, cfg.Height, nil, th)
	fbgrid.Paint(d, g)
	d.Present()
	return g
}

func exerciseThemeGrid(d *gfx.LinuxFB) (string, error) {
	games := []tenfoot.Game{{ID: "g0", Title: "Title 00", System: "snes", Launchable: true}}
	m := kitlauncher.Model{Games: games, Connected: true, TargetReady: true, ControllerConnected: true}
	var b strings.Builder
	sample := func(name string, th theme.Theme) (hlB, hlG, hlR, bgB, bgG, bgR byte, err error) {
		g := paintModel(d, m, th)
		hx, hy, ok := g.HighlightSample()
		if !ok {
			return 0, 0, 0, 0, 0, 0, fmt.Errorf("%s: no highlight", name)
		}
		hlB, hlG, hlR, _, err = gfx.SampleBGRX(d.Destination(), d.Config(), hx, hy)
		if err != nil {
			return 0, 0, 0, 0, 0, 0, err
		}
		bgY := g.HeaderH + 2
		if bgY < 0 {
			bgY = 0
		}
		bgB, bgG, bgR, _, err = gfx.SampleBGRX(d.Destination(), d.Config(), 2, bgY)
		if err != nil {
			return 0, 0, 0, 0, 0, 0, err
		}
		if hlB != th.Highlight.B || hlG != th.Highlight.G || hlR != th.Highlight.R {
			return 0, 0, 0, 0, 0, 0, fmt.Errorf("%s: highlight bgrx %d,%d,%d want %d,%d,%d", name, hlB, hlG, hlR, th.Highlight.B, th.Highlight.G, th.Highlight.R)
		}
		if bgB != th.Background.B || bgG != th.Background.G || bgR != th.Background.R {
			return 0, 0, 0, 0, 0, 0, fmt.Errorf("%s: background bgrx %d,%d,%d want %d,%d,%d", name, bgB, bgG, bgR, th.Background.B, th.Background.G, th.Background.R)
		}
		fmt.Fprintf(&b, "theme=%s highlight=(%d,%d) hl_bgrx=%d,%d,%d bg=(2,%d) bg_bgrx=%d,%d,%d header=%s\n",
			th.Name, hx, hy, hlB, hlG, hlR, bgY, bgB, bgG, bgR, theme.FormatColor(th.HeaderBar))
		return hlB, hlG, hlR, bgB, bgG, bgR, nil
	}
	def := theme.Default()
	arcade := theme.Arcade()
	dHLB, dHLG, dHLR, dBGB, dBGG, dBGR, err := sample("default", def)
	if err != nil {
		return b.String(), err
	}
	aHLB, aHLG, aHLR, aBGB, aBGG, aBGR, err := sample("arcade", arcade)
	if err != nil {
		return b.String(), err
	}
	if dHLB == aHLB && dHLG == aHLG && dHLR == aHLR && dBGB == aBGB && dBGG == aBGG && dBGR == aBGR {
		return b.String(), fmt.Errorf("default and arcade sampled the same pixels")
	}
	fmt.Fprintf(&b, "selftest-theme PASS default_hl=%d,%d,%d arcade_hl=%d,%d,%d default_bg=%d,%d,%d arcade_bg=%d,%d,%d\n",
		dHLB, dHLG, dHLR, aHLB, aHLG, aHLR, dBGB, dBGG, dBGR, aBGB, aBGG, aBGR)
	return b.String(), nil
}

func press(m *kitlauncher.Model, name string) {
	e, _ := remoteinput.NormalizeGamepad(name, true)
	m.Input(e, time.Now())
}

func mixedShelfGames() []tenfoot.Game {
	systems := []string{
		"pong", "pong",
		"megadrive", "megadrive", "megadrive", "megadrive", "megadrive",
		"snes", "snes", "snes", "snes", "snes", "snes", "snes", "snes",
	}
	games := make([]tenfoot.Game, len(systems))
	for i, system := range systems {
		games[i] = tenfoot.Game{
			ID:         fmt.Sprintf("%s-%02d", system, i),
			Title:      fmt.Sprintf("%s %02d", strings.ToUpper(system), i),
			System:     system,
			Launchable: true,
		}
	}
	return games
}

func exerciseShelfGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	var b strings.Builder
	step := func(name, wantShelf string, wantCount, wantFocus int) error {
		g := paintModel(d, m, th)
		start, end := catalogPage(m.Focus, len(m.Games))
		hx, hy, ok := g.HighlightSample()
		if !ok {
			return fmt.Errorf("%s: no highlight", name)
		}
		gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), d.Config(), hx, hy)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s shelf=%s games=%d focus=%d header=%q page=%d:%d highlight=(%d,%d) bgrx=%d,%d,%d,%d\n",
			name, m.Shelf, len(m.Games), m.Focus, g.Header, start, end, hx, hy, gotB, gotG, gotR, gotX)
		if m.Shelf != wantShelf {
			return fmt.Errorf("%s: shelf %q want %q", name, m.Shelf, wantShelf)
		}
		if len(m.Games) != wantCount {
			return fmt.Errorf("%s: games %d want %d", name, len(m.Games), wantCount)
		}
		if m.Focus != wantFocus {
			return fmt.Errorf("%s: focus %d want %d", name, m.Focus, wantFocus)
		}
		if !strings.Contains(g.Header, strings.ToUpper(wantShelf)) && !(wantShelf == kitlauncher.ShelfAll && strings.Contains(g.Header, "ALL")) {
			return fmt.Errorf("%s: header %q missing shelf", name, g.Header)
		}
		if !strings.Contains(g.Header, fmt.Sprintf("%d/15", wantCount)) {
			return fmt.Errorf("%s: header %q missing count", name, g.Header)
		}
		for _, game := range m.Games {
			if wantShelf != kitlauncher.ShelfAll && game.System != wantShelf {
				return fmt.Errorf("%s: visible %s on %s", name, game.System, wantShelf)
			}
		}
		if gotB != th.Highlight.B || gotG != th.Highlight.G || gotR != th.Highlight.R || gotX != 0 {
			return fmt.Errorf("%s: highlight bgrx %d,%d,%d,%d want %d,%d,%d,0", name, gotB, gotG, gotR, gotX, th.Highlight.B, th.Highlight.G, th.Highlight.R)
		}
		return nil
	}
	if err := step("origin", kitlauncher.ShelfAll, 15, 0); err != nil {
		return b.String(), err
	}
	press(&m, "r")
	if err := step("shoulder-r-pong", "pong", 2, 0); err != nil {
		return b.String(), err
	}
	press(&m, "r")
	if err := step("shoulder-r-megadrive", "megadrive", 5, 0); err != nil {
		return b.String(), err
	}
	press(&m, "r")
	if err := step("shoulder-r-snes", "snes", 8, 0); err != nil {
		return b.String(), err
	}
	press(&m, "r")
	if err := step("shoulder-r-wrap-all", kitlauncher.ShelfAll, 15, 7); err != nil {
		return b.String(), err
	}
	press(&m, "l")
	if err := step("shoulder-l-snes", "snes", 8, 0); err != nil {
		return b.String(), err
	}
	m.Shelf = kitlauncher.ShelfAll
	m.SetCatalog(mixedShelfGames())
	m.Focus = 1
	keepID := m.Games[m.Focus].ID
	press(&m, "r")
	if m.Games[m.Focus].ID != keepID {
		return b.String(), fmt.Errorf("keep-pong id %q want %q", m.Games[m.Focus].ID, keepID)
	}
	if err := step("keep-pong", "pong", 2, 1); err != nil {
		return b.String(), err
	}
	press(&m, "r")
	if err := step("leave-to-megadrive", "megadrive", 5, 0); err != nil {
		return b.String(), err
	}
	press(&m, "select")
	if err := step("select-snes", "snes", 8, 0); err != nil {
		return b.String(), err
	}
	press(&m, "select")
	if err := step("select-all", kitlauncher.ShelfAll, 15, 7); err != nil {
		return b.String(), err
	}
	m.Focus = 0
	press(&m, "dpad-right")
	if err := step("dpad-after-shelf", kitlauncher.ShelfAll, 15, 1); err != nil {
		return b.String(), err
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "launch" {
		return b.String(), fmt.Errorf("a launch %q", action)
	}
	fmt.Fprintf(&b, "selftest-shelf PASS header=%q footer=%q shelves=%s\n",
		m.HeaderChrome(), modelFooter(m), strings.Join(m.Shelves, ","))
	return b.String(), nil
}

func exerciseTextGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	arcade := theme.Arcade().Complete()
	cfg := d.Config()
	w, h := cfg.Width, cfg.Height
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	g := paintModel(d, m, arcade)
	snap := d.Snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "text-paint header=%q footer=%q tiles=%d theme=%s size=%dx%d\n",
		g.Header, g.Footer, len(g.Tiles), arcade.Name, w, h)

	debug, err := gfx.NewSoftware(w, h)
	if err != nil {
		return b.String(), err
	}
	debug.BeginFrame()
	debug.Clear(arcade.Background)
	debug.FillRect(gfx.Rect{X: 0, Y: 0, W: float32(w), H: float32(g.HeaderH)}, arcade.HeaderBar)
	debug.DebugText(16, (g.HeaderH-gfx.ScalePx(arcade.HeaderScale))/2, g.Header, arcade.HeaderScale)
	debugSnap := debug.Snapshot()
	if headerBytesEqual(snap, debugSnap, g.HeaderH) {
		return b.String(), fmt.Errorf("header still matches DebugText 8x8 HUD")
	}
	if !headerHasThemedInk(snap, g.HeaderH, arcade.Header) {
		return b.String(), fmt.Errorf("header missing themed UI-face ink")
	}
	fmt.Fprintf(&b, "header-not-debug=1 header-ink=1 header_color=%s\n", theme.FormatColor(arcade.Header))

	rec := gfx.NewRecorder()
	fbgrid.Paint(rec, g)
	var sawDebug, sawDraw bool
	for _, c := range rec.Calls {
		if c.Op == "DebugText" {
			sawDebug = true
		}
		if c.Op == "DrawText" {
			sawDraw = true
		}
	}
	if sawDebug || !sawDraw {
		return b.String(), fmt.Errorf("paint ops debug=%v drawtext=%v ops=%v", sawDebug, sawDraw, rec.Ops())
	}

	nav, err := exerciseNavGrid(d, th)
	b.WriteString(nav)
	if err != nil {
		return b.String(), err
	}
	shelf, err := exerciseShelfGrid(d, th)
	b.WriteString(shelf)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-text PASS font=goregular drawtext=1 debugtext=0\n")
	return b.String(), nil
}

func exerciseCoverGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	src := image.NewRGBA(image.Rect(0, 0, 40, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 40; x++ {
			src.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		return "", err
	}
	cover, err := tenfoot.DecodeCover(buf.Bytes())
	if err != nil {
		return "", err
	}
	fallback := th.SystemColor("megadrive")
	g := fbgrid.NewWithTiles(d.Config().Width, d.Config().Height, []fbgrid.Tile{
		{Name: "ART", Color: fallback, Cover: cover, CoverKind: fbgrid.CoverPresent},
		{Name: "FLAT", Color: fallback, CoverKind: fbgrid.CoverMissing},
		{Name: "WAIT", Color: fallback, CoverKind: fbgrid.CoverLoading},
	})
	fbgrid.ApplyTheme(&g, th)
	g.Header = "FOGCAST  COVER"
	g.Footer = "cover quality"
	fbgrid.Paint(d, g)
	d.Present()

	var b strings.Builder
	ix, iy, ok := g.InteriorSample()
	if !ok {
		return b.String(), fmt.Errorf("cover interior")
	}
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), d.Config(), ix, iy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "cover interior=(%d,%d) bgrx=%d,%d,%d,%d\n", ix, iy, gotB, gotG, gotR, gotX)
	if gotB != 160 || gotG != 32 || gotR != 255 || gotX != 0 {
		return b.String(), fmt.Errorf("cover interior bgrx %d,%d,%d,%d want 160,32,255,0", gotB, gotG, gotR, gotX)
	}

	hx, hy, ok := g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("cover highlight")
	}
	hlB, hlG, hlR, _, err := gfx.SampleBGRX(d.Destination(), d.Config(), hx, hy)
	if err != nil {
		return b.String(), err
	}
	if hlB != th.Highlight.B || hlG != th.Highlight.G || hlR != th.Highlight.R {
		return b.String(), fmt.Errorf("cover highlight bgrx %d,%d,%d want %d,%d,%d", hlB, hlG, hlR, th.Highlight.B, th.Highlight.G, th.Highlight.R)
	}

	missing := fbgrid.PlaceholderPanel(fallback, th, false)
	loading := fbgrid.PlaceholderPanel(fallback, th, true)
	g.Focus = 1
	fbgrid.Paint(d, g)
	d.Present()
	mx, my, ok := g.PanelSample(1)
	if !ok {
		return b.String(), fmt.Errorf("missing panel")
	}
	mB, mG, mR, _, err := gfx.SampleBGRX(d.Destination(), d.Config(), mx, my)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "placeholder missing=(%d,%d) bgrx=%d,%d,%d panel=%d,%d,%d\n", mx, my, mB, mG, mR, missing.B, missing.G, missing.R)
	if mB != missing.B || mG != missing.G || mR != missing.R {
		return b.String(), fmt.Errorf("missing placeholder bgrx %d,%d,%d want %d,%d,%d", mB, mG, mR, missing.B, missing.G, missing.R)
	}
	if mB == fallback.B && mG == fallback.G && mR == fallback.R {
		return b.String(), fmt.Errorf("missing placeholder stayed system fill")
	}

	g.Focus = 2
	fbgrid.Paint(d, g)
	d.Present()
	lx, ly, ok := g.PanelSample(2)
	if !ok {
		return b.String(), fmt.Errorf("loading panel")
	}
	lB, lG, lR, _, err := gfx.SampleBGRX(d.Destination(), d.Config(), lx, ly)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "placeholder loading=(%d,%d) bgrx=%d,%d,%d panel=%d,%d,%d\n", lx, ly, lB, lG, lR, loading.B, loading.G, loading.R)
	if lB != loading.B || lG != loading.G || lR != loading.R {
		return b.String(), fmt.Errorf("loading placeholder bgrx %d,%d,%d want %d,%d,%d", lB, lG, lR, loading.B, loading.G, loading.R)
	}
	if lB == mB && lG == mG && lR == mR {
		return b.String(), fmt.Errorf("loading placeholder matched missing")
	}

	text, err := exerciseTextGrid(d, th)
	b.WriteString(text)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-cover PASS decode=1 placeholder=1 loading=1 letterbox=theme\n")
	return b.String(), nil
}

func headerBytesEqual(a, b *image.RGBA, headerH int) bool {
	if a == nil || b == nil {
		return false
	}
	if headerH < 1 {
		headerH = 1
	}
	wa, wb := a.Bounds().Dx(), b.Bounds().Dx()
	if wa != wb {
		return false
	}
	for y := 0; y < headerH && y < a.Bounds().Dy() && y < b.Bounds().Dy(); y++ {
		ao := a.PixOffset(0, y)
		bo := b.PixOffset(0, y)
		for i := 0; i < wa*4; i++ {
			if a.Pix[ao+i] != b.Pix[bo+i] {
				return false
			}
		}
	}
	return true
}

func headerHasThemedInk(img *image.RGBA, headerH int, c gfx.Color) bool {
	if img == nil {
		return false
	}
	for y := 0; y < headerH && y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			p := img.RGBAAt(x, y)
			if absByte(int(p.R)-int(c.R)) <= 40 && absByte(int(p.G)-int(c.G)) <= 40 && absByte(int(p.B)-int(c.B)) <= 40 && p.A > 128 {
				return true
			}
		}
	}
	return false
}

func absByte(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
