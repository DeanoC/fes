package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/host/tenfoot/linuxinput"
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

func runBoldSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseBoldGrid(d, th)
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

func runAttractSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseAttractGrid(d, th)
	fmt.Print(report)
	return err
}

func runDetailSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseDetailGrid(d, th)
	fmt.Print(report)
	return err
}

func runMotionSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseMotionGrid(d, th)
	fmt.Print(report)
	return err
}

func runWheelSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseWheelGrid(d, th)
	fmt.Print(report)
	return err
}

func runStripSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseStripGrid(d, th)
	fmt.Print(report)
	return err
}

func runAtmosphereSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseAtmosphereGrid(d, th)
	fmt.Print(report)
	return err
}

func runBadgesSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseBadgesGrid(d, th)
	fmt.Print(report)
	return err
}

func exerciseBadgesGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	cfg := d.Config()
	var b strings.Builder
	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	g := fbgrid.NewWithTiles(cfg.Width, cfg.Height, []fbgrid.Tile{
		{Name: "SONIC", Color: th.SystemColor("megadrive"), Cover: cover, CoverKind: fbgrid.CoverPresent, Badges: fbgrid.ComposeBadges("2", "4.5", "100%", true)},
		{Name: "PONG", Color: th.SystemColor("pong"), Cover: cover, CoverKind: fbgrid.CoverPresent},
	})
	fbgrid.ApplyTheme(&g, th)
	g.Header = "FOGCAST  BADGES"
	g.Footer = "A play | B detail | L/R | Y flow"
	fbgrid.Paint(d, g)
	d.Present()
	bx, by, ok := fbgrid.TileBadgeSample(g, 0, 0)
	if !ok {
		return b.String(), fmt.Errorf("grid badge sample")
	}
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), cfg, bx, by)
	if err != nil {
		return b.String(), err
	}
	if gotB != th.Highlight.B || gotG != th.Highlight.G || gotR != th.Highlight.R || gotX != 0 {
		return b.String(), fmt.Errorf("grid badge bgrx %d,%d,%d,%d", gotB, gotG, gotR, gotX)
	}
	ix, iy, ok := g.InteriorSample()
	if !ok {
		return b.String(), fmt.Errorf("grid interior")
	}
	inB, inG, inR, inX, err := gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	if inB != 160 || inG != 32 || inR != 255 || inX != 0 {
		return b.String(), fmt.Errorf("cover crushed bgrx %d,%d,%d,%d", inB, inG, inR, inX)
	}
	if _, _, ok := fbgrid.TileBadgeSample(g, 1, 0); ok {
		return b.String(), fmt.Errorf("empty tile grew a badge")
	}
	fmt.Fprintf(&b, "grid badge=(%d,%d) bgrx=%d,%d,%d,%d interior=(%d,%d) cover_bgrx=%d,%d,%d,%d\n",
		bx, by, gotB, gotG, gotR, gotX, ix, iy, inB, inG, inR, inX)

	rec := gfx.NewRecorder()
	fbgrid.Paint(rec, g)
	var saw2P, sawRating, sawDone, sawPort bool
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		switch c.Text {
		case "2P":
			saw2P = true
		case "4.5":
			sawRating = true
		case "100%":
			sawDone = true
		case "PORT":
			sawPort = true
		}
	}
	if !saw2P || !sawRating || !sawDone || !sawPort {
		return b.String(), fmt.Errorf("grid chips 2P=%v rating=%v done=%v port=%v ops=%v", saw2P, sawRating, sawDone, sawPort, rec.Ops())
	}

	for _, kind := range []fbgrid.BrowseKind{fbgrid.BrowseCoverflow, fbgrid.BrowseWall} {
		g.Kind = kind
		fbgrid.ApplyTheme(&g, th)
		fbgrid.Paint(d, g)
		d.Present()
		if _, _, ok := fbgrid.TileBadgeSample(g, 0, 0); !ok {
			return b.String(), fmt.Errorf("%s badge sample", kind)
		}
		fmt.Fprintf(&b, "layout=%s badge=1\n", kind)
	}

	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	m.Focus = 1
	m.ApplyPresentation(m.Games[m.Focus].ID, tenfoot.Presentation{Presentation: &tenfoot.PresentationInfo{Players: "1-2"}})
	frame := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	if len(frame.Badges) == 0 || frame.Badges[0].Label != "1-2" {
		return b.String(), fmt.Errorf("detail badges %+v", frame.Badges)
	}
	fbgrid.PaintDetail(d, frame)
	d.Present()
	dx, dy, ok := fbgrid.DetailBadgeSample(cfg.Width, cfg.Height, th, frame)
	if !ok {
		return b.String(), fmt.Errorf("detail badge sample")
	}
	dB, dG, dR, dX, err := gfx.SampleBGRX(d.Destination(), cfg, dx, dy)
	if err != nil {
		return b.String(), err
	}
	if dB != th.Highlight.B || dG != th.Highlight.G || dR != th.Highlight.R || dX != 0 {
		return b.String(), fmt.Errorf("detail badge bgrx %d,%d,%d,%d", dB, dG, dR, dX)
	}
	fmt.Fprintf(&b, "detail badge=(%d,%d) bgrx=%d,%d,%d,%d label=%q\n", dx, dy, dB, dG, dR, dX, frame.Badges[0].Label)

	wheel := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, WheelOpen: true}
	games := mixedShelfGames()
	for i := range games {
		if games[i].System == "megadrive" && games[i].ID != "" {
			games[i].PlayCount = 2
			games[i].LastPlayedAt = int64(100 + i)
			if games[i].Title == "" {
				games[i].Title = "Sonic"
			}
		}
	}
	wheel.SetCatalog(games)
	wheel.CycleShelf(1)
	wheel.CycleShelf(1)
	if wheel.Shelf != "megadrive" {
		return b.String(), fmt.Errorf("wheel shelf %q", wheel.Shelf)
	}
	wframe := modelWheelFrame(wheel, nil, nil, nil, th, cfg.Width, cfg.Height)
	if !strings.Contains(wframe.Stats, "plays") || wframe.Featured == "" {
		return b.String(), fmt.Errorf("wheel stats=%q featured=%q", wframe.Stats, wframe.Featured)
	}
	fbgrid.PaintWheel(d, wframe)
	d.Present()
	fmt.Fprintf(&b, "wheel stats=%q featured=%q header=%q\n", wframe.Stats, wframe.Featured, wframe.Header)

	press(&m, "y")
	if m.Browse != fbgrid.BrowseCoverflow {
		return b.String(), fmt.Errorf("y stole browse %s", m.Browse)
	}
	press(&m, "x")
	if m.Pack != theme.PackNeon {
		return b.String(), fmt.Errorf("x stole pack %s", m.Pack)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "launch" {
		return b.String(), fmt.Errorf("A after badges %q", action)
	}
	fmt.Fprintf(&b, "nav-still y=%s x=%s launch=1\n", m.Browse, m.Pack)

	for _, look := range []theme.Theme{theme.Default(), theme.Arcade(), theme.Night()} {
		look = look.Complete()
		packGrid := fbgrid.NewWithTiles(cfg.Width, cfg.Height, []fbgrid.Tile{
			{Name: "SONIC", Color: look.SystemColor("megadrive"), Badges: fbgrid.ComposeBadges("2", "", "", false)},
		})
		fbgrid.ApplyTheme(&packGrid, look)
		fbgrid.Paint(d, packGrid)
		d.Present()
		px, py, ok := fbgrid.TileBadgeSample(packGrid, 0, 0)
		if !ok {
			return b.String(), fmt.Errorf("%s pack badge", look.Name)
		}
		pB, pG, pR, _, err := gfx.SampleBGRX(d.Destination(), cfg, px, py)
		if err != nil {
			return b.String(), err
		}
		if pB != look.Highlight.B || pG != look.Highlight.G || pR != look.Highlight.R {
			return b.String(), fmt.Errorf("%s pack badge bgrx %d,%d,%d want %d,%d,%d", look.Name, pB, pG, pR, look.Highlight.B, look.Highlight.G, look.Highlight.R)
		}
		fmt.Fprintf(&b, "pack=%s badge_bgrx=%d,%d,%d\n", look.Name, pB, pG, pR)
	}

	packs, err := exercisePacksGrid(d, th)
	b.WriteString(packs)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-badges PASS chips=1 wheel-stats=1 packs=1 nested-packs=1\n")
	return b.String(), nil
}

func runPacksSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exercisePacksGrid(d, th)
	fmt.Print(report)
	return err
}

func runMarqueeSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseMarqueeGrid(d, th)
	fmt.Print(report)
	return err
}

func exerciseMarqueeGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	cfg := d.Config()
	var b strings.Builder
	aa := strings.Repeat("aa", 32)
	bb := strings.Repeat("bb", 32)
	cc := strings.Repeat("cc", 32)
	ee := strings.Repeat("ee", 32)
	still := solidStill(255, 32, 160, 40, 8)
	banner := solidStill(16, 200, 48, 80, 12)
	shot := solidStill(32, 200, 64, 8, 8)
	stills := map[string]*image.RGBA{aa: still, bb: banner, cc: shot}

	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	m.Focus = 1
	m.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{
		{GameID: "mario", Title: "Mario", Platform: "snes", Backdrop: aa, Marquee: bb, Launchable: true},
	}})
	m.SetAttractIdle(20 * time.Millisecond)
	m.SetAttractCycle(10 * time.Second)
	t0 := time.Now()
	m.Tick(t0)
	m.Tick(t0.Add(40 * time.Millisecond))
	if !m.AttractActive {
		return b.String(), fmt.Errorf("attract did not arm")
	}
	view := m.AttractView(t0.Add(40 * time.Millisecond))
	if view.Handle != aa || view.Marquee != bb {
		return b.String(), fmt.Errorf("attract view %+v", view)
	}
	paintAttractModel(d, view, stills, th)
	mx, my, ok := fbgrid.AttractMarqueeSample(cfg.Width, cfg.Height, banner, th)
	if !ok {
		return b.String(), fmt.Errorf("attract marquee sample")
	}
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), cfg, mx, my)
	if err != nil {
		return b.String(), err
	}
	if gotB != 48 || gotG != 200 || gotR != 16 || gotX != 0 {
		return b.String(), fmt.Errorf("attract marquee bgrx %d,%d,%d,%d", gotB, gotG, gotR, gotX)
	}
	frame := attractFrameFromStills(view, stills, th, cfg.Width, cfg.Height)
	dest := fbgrid.AttractStillDestFor(frame)
	sx := int(dest.X + dest.W/2)
	sy := int(dest.Y + dest.H/2)
	sB, sG, sR, sX, err := gfx.SampleBGRX(d.Destination(), cfg, sx, sy)
	if err != nil {
		return b.String(), err
	}
	if sB != 160 || sG != 32 || sR != 255 || sX != 0 {
		return b.String(), fmt.Errorf("attract still crushed bgrx %d,%d,%d,%d", sB, sG, sR, sX)
	}
	if sy <= my {
		return b.String(), fmt.Errorf("still y=%d not below marquee y=%d", sy, my)
	}
	fmt.Fprintf(&b, "attract marquee=(%d,%d) bgrx=%d,%d,%d,%d still=(%d,%d) bgrx=%d,%d,%d,%d\n",
		mx, my, gotB, gotG, gotR, gotX, sx, sy, sB, sG, sR, sX)

	hidden := kitlauncher.Model{Connected: true, TargetReady: true, AttractActive: true}
	hidden.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{
		{GameID: "mario", Title: "Mario", Platform: "snes", Backdrop: aa, Launchable: true},
	}})
	hidden.AttractActive = true
	hiddenView := hidden.AttractView(t0)
	if hiddenView.Marquee != "" {
		return b.String(), fmt.Errorf("absent marquee handle %q", hiddenView.Marquee)
	}
	paintAttractModel(d, hiddenView, stills, th)
	hB, hG, hR, _, err := gfx.SampleBGRX(d.Destination(), cfg, mx, my)
	if err != nil {
		return b.String(), err
	}
	if hB == 48 && hG == 200 && hR == 16 {
		return b.String(), fmt.Errorf("absent attract marquee kept banner pixels")
	}
	fmt.Fprintf(&b, "attract hide=1\n")

	only := kitlauncher.Model{Connected: true, TargetReady: true, AttractActive: true}
	only.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{
		{GameID: "pong", Title: "Pong", Marquee: bb, Launchable: true},
	}})
	only.AttractActive = true
	onlyView := only.AttractView(t0)
	if onlyView.Handle != bb || onlyView.Marquee != "" {
		return b.String(), fmt.Errorf("marquee-only %+v", onlyView)
	}
	fmt.Fprintf(&b, "attract marquee-only still=1 strip=0\n")

	motion := kitlauncher.Model{Connected: true, TargetReady: true}
	motion.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{
		{GameID: "mario", Title: "Mario", Platform: "snes", Video: ee, Backdrop: aa, Cover: cc, Marquee: bb, Launchable: true},
	}})
	motion.SetAttractIdle(20 * time.Millisecond)
	motion.SetAttractCycle(10 * time.Second)
	tMotion := time.Now()
	motion.Tick(tMotion)
	motion.Tick(tMotion.Add(40 * time.Millisecond))
	motionView := motion.AttractView(tMotion.Add(40 * time.Millisecond))
	if !motionView.Motion || motionView.Marquee != bb {
		return b.String(), fmt.Errorf("motion marquee %+v", motionView)
	}
	paintAttractModel(d, motionView, stills, th)
	motionFrame := attractFrameFromStills(motionView, stills, th, cfg.Width, cfg.Height)
	bx, by, ok := fbgrid.AttractVideoBadgeSampleFor(motionFrame)
	if !ok {
		return b.String(), fmt.Errorf("motion badge sample")
	}
	bB, bG, bR, bX, err := gfx.SampleBGRX(d.Destination(), cfg, bx, by)
	if err != nil {
		return b.String(), err
	}
	if bB != 0 || bG != 220 || bR != 255 || bX != 0 {
		return b.String(), fmt.Errorf("motion badge bgrx %d,%d,%d,%d", bB, bG, bR, bX)
	}
	fmt.Fprintf(&b, "attract motion-marquee=1 badge=(%d,%d)\n", bx, by)

	press(&m, "b")
	if m.AttractActive {
		return b.String(), fmt.Errorf("dismiss failed")
	}
	m.ApplyPresentation(m.Games[m.Focus].ID, tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{
			MarqueeID:     bb,
			Studio:        "SEGA",
			Year:          "1991",
			Players:       "1-2",
			VideoID:       ee,
			ScreenshotIDs: []string{cc},
		},
	})
	press(&m, "b")
	if !m.DetailOpen {
		return b.String(), fmt.Errorf("detail closed")
	}
	if m.FocusMarqueeHandle() != bb {
		return b.String(), fmt.Errorf("detail marquee handle %q", m.FocusMarqueeHandle())
	}
	dframe := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	dframe.Cover = still
	dframe.CoverKind = fbgrid.CoverPresent
	dframe.Marquee = banner
	dframe.Shot = shot
	dframe.VideoBadge = true
	dframe.ShotCaption = "preview"
	fbgrid.PaintDetail(d, dframe)
	d.Present()
	dx, dy, ok := fbgrid.DetailMarqueeSample(cfg.Width, cfg.Height, th, banner)
	if !ok {
		return b.String(), fmt.Errorf("detail marquee sample")
	}
	dB, dG, dR, dX, err := gfx.SampleBGRX(d.Destination(), cfg, dx, dy)
	if err != nil {
		return b.String(), err
	}
	if dB != 48 || dG != 200 || dR != 16 || dX != 0 {
		return b.String(), fmt.Errorf("detail marquee bgrx %d,%d,%d,%d", dB, dG, dR, dX)
	}
	cx, cy, ok := fbgrid.DetailCoverSampleFor(cfg.Width, cfg.Height, th, dframe)
	if !ok {
		return b.String(), fmt.Errorf("detail cover sample")
	}
	cB, cG, cR, cX, err := gfx.SampleBGRX(d.Destination(), cfg, cx, cy)
	if err != nil {
		return b.String(), err
	}
	if cB != 160 || cG != 32 || cR != 255 || cX != 0 {
		return b.String(), fmt.Errorf("detail cover crushed bgrx %d,%d,%d,%d", cB, cG, cR, cX)
	}
	vbX, vbY, ok := fbgrid.DetailVideoBadgeSample(cfg.Width, cfg.Height, th, dframe)
	if !ok {
		return b.String(), fmt.Errorf("detail video sample")
	}
	vB, vG, vR, vX, err := gfx.SampleBGRX(d.Destination(), cfg, vbX, vbY)
	if err != nil {
		return b.String(), err
	}
	if vB != 0 || vG != 220 || vR != 255 || vX != 0 {
		return b.String(), fmt.Errorf("detail video crushed bgrx %d,%d,%d,%d", vB, vG, vR, vX)
	}
	badgeX, badgeY, ok := fbgrid.DetailBadgeSample(cfg.Width, cfg.Height, th, dframe)
	if !ok {
		return b.String(), fmt.Errorf("detail badge sample")
	}
	bdB, bdG, bdR, _, err := gfx.SampleBGRX(d.Destination(), cfg, badgeX, badgeY)
	if err != nil {
		return b.String(), err
	}
	if bdB != th.Highlight.B || bdG != th.Highlight.G || bdR != th.Highlight.R {
		return b.String(), fmt.Errorf("detail badge crushed bgrx %d,%d,%d", bdB, bdG, bdR)
	}
	rec := gfx.NewRecorder()
	fbgrid.PaintDetail(rec, dframe)
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
		return b.String(), fmt.Errorf("detail chrome meta=%v video=%v preview=%v ops=%v", sawMeta, sawVideo, sawPreview, rec.Ops())
	}
	fmt.Fprintf(&b, "detail marquee=(%d,%d) cover=(%d,%d) video=1 badge=1 meta=1\n", dx, dy, cx, cy)

	plain := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	plain.Cover = still
	plain.CoverKind = fbgrid.CoverPresent
	plain.Marquee = nil
	fbgrid.PaintDetail(d, plain)
	d.Present()
	pB, pG, pR, _, err := gfx.SampleBGRX(d.Destination(), cfg, dx, dy)
	if err != nil {
		return b.String(), err
	}
	if pB == 48 && pG == 200 && pR == 16 {
		return b.String(), fmt.Errorf("absent detail marquee kept banner pixels")
	}
	fmt.Fprintf(&b, "detail hide=1\n")

	press(&m, "b")
	if m.DetailOpen {
		return b.String(), fmt.Errorf("detail stayed open")
	}
	press(&m, "y")
	if m.Browse != fbgrid.BrowseCoverflow {
		return b.String(), fmt.Errorf("y stole browse %s", m.Browse)
	}
	press(&m, "x")
	if m.Pack != theme.PackNeon {
		return b.String(), fmt.Errorf("x stole pack %s", m.Pack)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "launch" {
		return b.String(), fmt.Errorf("A after marquee %q", action)
	}
	fmt.Fprintf(&b, "nav-still y=%s x=%s launch=1\n", m.Browse, m.Pack)

	nested, err := exerciseTransitionGrid(d, th)
	b.WriteString(nested)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-marquee PASS attract=1 hide=1 stills-fallback=1 motion=1 detail=1 nested-transition=1\n")
	return b.String(), nil
}

func runTransitionSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseTransitionGrid(d, th)
	fmt.Print(report)
	return err
}

func runSearchSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseSearchGrid(d, th)
	fmt.Print(report)
	return err
}

func oskType(m *kitlauncher.Model, text string) {
	for _, r := range text {
		id := "char-" + string(r)
		if r == ' ' {
			id = "space"
		}
		if !m.FocusSearchKey(id) {
			continue
		}
		press(m, "a")
	}
}

func exerciseSearchGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	cfg := d.Config()
	var b strings.Builder
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedCatalog())
	m.Focus = 1
	if m.Games[m.Focus].ID != "sonic" {
		return b.String(), fmt.Errorf("origin focus %q", m.Games[m.Focus].ID)
	}
	restore := m.Games[m.Focus].ID
	g := paintModel(d, m, th)
	if strings.Contains(g.Header, "SEARCH") {
		return b.String(), fmt.Errorf("browse header tagged search %q", g.Header)
	}
	if g.EmptyLabel != "" {
		return b.String(), fmt.Errorf("browse empty label %q", g.EmptyLabel)
	}

	press(&m, "start")
	if !m.SearchOpen {
		return b.String(), fmt.Errorf("start did not open search")
	}
	if m.Browse != fbgrid.BrowseGrid {
		return b.String(), fmt.Errorf("start stole layout %s", m.Browse)
	}
	g = paintModel(d, m, th)
	if !strings.Contains(g.Header, "SEARCH") {
		return b.String(), fmt.Errorf("search header %q", g.Header)
	}
	if g.Footer != tenfoot.OSKKitHint(0) {
		return b.String(), fmt.Errorf("osk footer %q", g.Footer)
	}
	rec := gfx.NewRecorder()
	fbgrid.Paint(rec, g)
	fbgrid.PaintOSK(rec, modelOSKFrame(m, cfg.Width, cfg.Height, kitLook(m, th), g))
	var sawSearch, sawQ, sawDone bool
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if strings.Contains(c.Text, "Search:") {
			sawSearch = true
		}
		if c.Text == "Q" {
			sawQ = true
		}
		if strings.EqualFold(c.Text, "done") {
			sawDone = true
		}
	}
	if !sawSearch || !sawQ || !sawDone {
		return b.String(), fmt.Errorf("osk chrome search=%v q=%v done=%v ops=%v", sawSearch, sawQ, sawDone, rec.Ops())
	}
	fmt.Fprintf(&b, "open header=%q footer=%q osk=1\n", g.Header, g.Footer)

	press(&m, "y")
	if m.Browse != fbgrid.BrowseGrid || !m.SearchOpen {
		return b.String(), fmt.Errorf("y stole layout browse=%s open=%v", m.Browse, m.SearchOpen)
	}
	press(&m, "x")
	if m.Pack != "" || !m.SearchOpen {
		return b.String(), fmt.Errorf("x stole pack %q", m.Pack)
	}
	press(&m, "select")
	if m.Shelf != kitlauncher.ShelfAll || !m.SearchOpen {
		return b.String(), fmt.Errorf("select stole shelf %q", m.Shelf)
	}

	oskType(&m, "sonic")
	if fold := strings.ToLower(m.SearchQuery); fold != "sonic" {
		return b.String(), fmt.Errorf("query %q", m.SearchQuery)
	}
	if len(m.Games) != 1 || m.Games[0].ID != "sonic" {
		return b.String(), fmt.Errorf("filter games=%v", idsOf(m.Games))
	}
	g = paintModel(d, m, th)
	if !strings.Contains(g.Header, "1/6") || !strings.Contains(g.Header, "SEARCH") {
		return b.String(), fmt.Errorf("filtered header %q", g.Header)
	}
	fmt.Fprintf(&b, "filter query=%q n=%d header=%q\n", m.SearchQuery, len(m.Games), g.Header)

	if !m.FocusSearchKey("done") {
		return b.String(), fmt.Errorf("focus done")
	}
	press(&m, "a")
	if m.SearchOpen {
		return b.String(), fmt.Errorf("done left osk open")
	}
	if m.SearchQuery != "sonic" || len(m.Games) != 1 {
		return b.String(), fmt.Errorf("done dropped filter q=%q n=%d", m.SearchQuery, len(m.Games))
	}
	press(&m, "dpad-right")
	if m.Focus != 0 {
		return b.String(), fmt.Errorf("filtered dpad wrap %d", m.Focus)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "launch" {
		return b.String(), fmt.Errorf("filtered A %q", action)
	}
	press(&m, "b")
	if !m.DetailOpen {
		return b.String(), fmt.Errorf("filtered B did not open detail")
	}
	press(&m, "b")
	if m.DetailOpen {
		return b.String(), fmt.Errorf("detail B did not close")
	}
	fmt.Fprintf(&b, "nav-filtered n=%d launch=1 detail=1\n", len(m.Games))

	press(&m, "start")
	if !m.SearchOpen {
		return b.String(), fmt.Errorf("start did not reopen osk")
	}
	press(&m, "b")
	if strings.TrimSpace(m.SearchQuery) != "" || !m.SearchOpen {
		return b.String(), fmt.Errorf("clear q=%q open=%v", m.SearchQuery, m.SearchOpen)
	}
	if len(m.Games) != 6 {
		return b.String(), fmt.Errorf("empty query n=%d", len(m.Games))
	}
	press(&m, "b")
	if m.SearchOpen || m.SearchQuery != "" {
		return b.String(), fmt.Errorf("exit open=%v q=%q", m.SearchOpen, m.SearchQuery)
	}
	if m.Games[m.Focus].ID != restore {
		return b.String(), fmt.Errorf("restore focus %q want %q", m.Games[m.Focus].ID, restore)
	}
	fmt.Fprintf(&b, "restore id=%q n=%d\n", m.Games[m.Focus].ID, len(m.Games))

	press(&m, "start")
	oskType(&m, "zzzz")
	if len(m.Games) != 0 {
		return b.String(), fmt.Errorf("miss n=%d", len(m.Games))
	}
	g = paintModel(d, m, th)
	if g.EmptyLabel != "No matches" || len(g.Tiles) != 0 {
		return b.String(), fmt.Errorf("empty label=%q tiles=%d", g.EmptyLabel, len(g.Tiles))
	}
	emptyRec := gfx.NewRecorder()
	fbgrid.Paint(emptyRec, g)
	var sawMiss bool
	for _, c := range emptyRec.Calls {
		if c.Op == "DrawText" && c.Text == "No matches" {
			sawMiss = true
		}
	}
	if !sawMiss {
		return b.String(), fmt.Errorf("empty state not painted ops=%v", emptyRec.Ops())
	}
	fmt.Fprintf(&b, "empty-miss label=%q tiles=%d\n", g.EmptyLabel, len(g.Tiles))
	press(&m, "b")
	press(&m, "b")

	stripM := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	stripM.SetCatalog(mixedCatalog())
	stripM.SetStrip([]tenfoot.Game{{ID: "recent", Title: "Recent", Launchable: true}}, "Recent")
	press(&stripM, "start")
	oskType(&stripM, "zzzz")
	if !stripM.FocusSearchKey("done") {
		return b.String(), fmt.Errorf("strip-hide done")
	}
	press(&stripM, "a")
	g = paintModel(d, stripM, th)
	if g.EmptyLabel != "No matches" || len(g.Tiles) != 0 || len(g.Strip) != 0 {
		return b.String(), fmt.Errorf("strip-hide label=%q tiles=%d strip=%d", g.EmptyLabel, len(g.Tiles), len(g.Strip))
	}
	fmt.Fprintf(&b, "strip-hide label=%q tiles=%d strip=%d\n", g.EmptyLabel, len(g.Tiles), len(g.Strip))

	untitled := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	untitled.SetCatalog([]tenfoot.Game{
		{ID: "logo-md", Title: "", System: "megadrive", Launchable: true},
		{ID: "named", Title: "Streets", System: "megadrive", Launchable: true},
	})
	press(&untitled, "start")
	oskType(&untitled, "megadrive")
	if len(untitled.Games) != 1 || untitled.Games[0].ID != "logo-md" {
		return b.String(), fmt.Errorf("logo fallback games=%v", idsOf(untitled.Games))
	}
	fmt.Fprintf(&b, "logo-fallback n=%d id=%q\n", len(untitled.Games), untitled.Games[0].ID)

	wheel := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, WheelOpen: true}
	wheel.SetCatalog(mixedCatalog())
	press(&wheel, "r")
	press(&wheel, "r")
	if wheel.Shelf != "megadrive" || !wheel.WheelOpen {
		return b.String(), fmt.Errorf("wheel shelf %q open=%v", wheel.Shelf, wheel.WheelOpen)
	}
	press(&wheel, "start")
	if wheel.WheelOpen || !wheel.SearchOpen || wheel.Shelf != "megadrive" {
		return b.String(), fmt.Errorf("wheel start open=%v search=%v shelf=%q", wheel.WheelOpen, wheel.SearchOpen, wheel.Shelf)
	}
	oskType(&wheel, "sonic")
	if !wheel.FocusSearchKey("done") {
		return b.String(), fmt.Errorf("wheel done")
	}
	press(&wheel, "a")
	press(&wheel, "b")
	if !wheel.WheelOpen || strings.TrimSpace(wheel.SearchQuery) != "" || wheel.SearchTag() != "" {
		return b.String(), fmt.Errorf("wheel back search q=%q tag=%q wheel=%v", wheel.SearchQuery, wheel.SearchTag(), wheel.WheelOpen)
	}
	if wheel.WheelStats() != "3 games" {
		return b.String(), fmt.Errorf("wheel back stats %q", wheel.WheelStats())
	}
	press(&wheel, "r")
	press(&wheel, "a")
	if wheel.WheelOpen || wheel.Shelf != "snes" || len(wheel.Games) != 2 || wheel.SearchTag() != "" {
		return b.String(), fmt.Errorf("snes after search wheel=%v shelf=%q n=%d tag=%q", wheel.WheelOpen, wheel.Shelf, len(wheel.Games), wheel.SearchTag())
	}
	fmt.Fprintf(&b, "wheel-start shelf=megadrive search=1 wheel-back=1 snes n=%d\n", len(wheel.Games))

	press(&m, "y")
	if m.Browse != fbgrid.BrowseCoverflow {
		return b.String(), fmt.Errorf("y after search browse=%s", m.Browse)
	}
	press(&m, "x")
	if m.Pack != theme.PackNeon {
		return b.String(), fmt.Errorf("x after search pack=%s", m.Pack)
	}
	if action := m.Input(a, time.Now()); action != "launch" {
		return b.String(), fmt.Errorf("A after search %q", action)
	}
	fmt.Fprintf(&b, "nav-still y=%s x=%s launch=1\n", m.Browse, m.Pack)

	nested, err := exerciseTransitionGrid(d, th)
	b.WriteString(nested)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-search PASS open=1 filter=1 empty=1 restore=1 logo=1 nested-transition=1\n")
	return b.String(), nil
}

func idsOf(games []tenfoot.Game) []string {
	out := make([]string, len(games))
	for i, game := range games {
		out[i] = game.ID
	}
	return out
}

func mixedCatalog() []tenfoot.Game {
	return []tenfoot.Game{
		{ID: "pong", Title: "Pong", System: "pong", Launchable: true},
		{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true},
		{ID: "streets", Title: "Streets", System: "megadrive", Launchable: true},
		{ID: "blocked-md", Title: "Blocked", System: "megadrive", Launchable: false},
		{ID: "mario", Title: "Mario", System: "snes", Launchable: true},
		{ID: "zelda", Title: "Zelda", System: "snes", Launchable: true},
	}
}

func exerciseTransitionGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	cfg := d.Config()
	var b strings.Builder
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, Pack: theme.PackClassic}
	m.SetCatalog(mixedShelfGames())
	g := paintModel(d, m, th)
	if g.Kind != fbgrid.BrowseGrid {
		return b.String(), fmt.Errorf("origin kind=%s", g.Kind)
	}
	ix, iy, ok := g.PanelSample(0)
	if !ok {
		return b.String(), fmt.Errorf("origin panel")
	}
	look := kitLook(m, th)
	tile := fbgrid.PlaceholderPanel(g.Tiles[0].Color, look, false)
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	if gotB != tile.B || gotG != tile.G || gotR != tile.R {
		return b.String(), fmt.Errorf("settled tile bgrx %d,%d,%d want %d,%d,%d", gotB, gotG, gotR, tile.B, tile.G, tile.R)
	}
	fmt.Fprintf(&b, "settled kind=%s tile=(%d,%d) bgrx=%d,%d,%d curtain=%s wipe=%s glitch=%s\n",
		g.Kind, ix, iy, gotB, gotG, gotR, anim.CurtainDuration, anim.WipeDuration, anim.GlitchDuration)
	if anim.CurtainDuration > anim.MaxDuration || anim.WipeDuration > anim.MaxDuration || anim.GlitchDuration > anim.MaxDuration {
		return b.String(), fmt.Errorf("duration cap")
	}

	dst := gfx.Rect{X: 0, Y: 0, W: float32(cfg.Width), H: float32(cfg.Height)}
	classic := theme.Default()
	overlay := func(style anim.Style, t float64, look theme.Theme) error {
		fbgrid.Paint(d, g)
		anim.PaintTransition(d, dst, style, t, transitionColors(look))
		d.Present()
		return nil
	}

	if err := overlay(anim.StyleCurtain, 0, classic); err != nil {
		return b.String(), err
	}
	cB, cG, cR, _, err := gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	fill := classic.HeaderBar
	if cB != fill.B || cG != fill.G || cR != fill.R {
		return b.String(), fmt.Errorf("curtain-t0 tile bgrx %d,%d,%d want fill %d,%d,%d", cB, cG, cR, fill.B, fill.G, fill.R)
	}
	if cB == tile.B && cG == tile.G && cR == tile.R {
		return b.String(), fmt.Errorf("curtain-t0 left tile revealed")
	}
	fmt.Fprintf(&b, "curtain-t0 tile=(%d,%d) bgrx=%d,%d,%d fill=%s\n", ix, iy, cB, cG, cR, theme.FormatColor(fill))

	if err := overlay(anim.StyleCurtain, 1, classic); err != nil {
		return b.String(), err
	}
	sB, sG, sR, _, err := gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	if sB != tile.B || sG != tile.G || sR != tile.R {
		return b.String(), fmt.Errorf("curtain-settled tile bgrx %d,%d,%d", sB, sG, sR)
	}

	if err := overlay(anim.StyleWipe, 0.5, theme.Night()); err != nil {
		return b.String(), err
	}
	leftB, leftG, leftR, _, err := gfx.SampleBGRX(d.Destination(), cfg, cfg.Width/8, cfg.Height/2)
	if err != nil {
		return b.String(), err
	}
	rightB, rightG, rightR, _, err := gfx.SampleBGRX(d.Destination(), cfg, 7*cfg.Width/8, cfg.Height/2)
	if err != nil {
		return b.String(), err
	}
	if leftB == rightB && leftG == rightG && leftR == rightR {
		return b.String(), fmt.Errorf("wipe-mid left and right matched %d,%d,%d", leftB, leftG, leftR)
	}
	wipeFill := theme.Night().HeaderBar
	if rightB != wipeFill.B || rightG != wipeFill.G || rightR != wipeFill.R {
		return b.String(), fmt.Errorf("wipe-mid right bgrx %d,%d,%d want fill %d,%d,%d", rightB, rightG, rightR, wipeFill.B, wipeFill.G, wipeFill.R)
	}
	fmt.Fprintf(&b, "wipe-mid left=(%d,%d) bgrx=%d,%d,%d right=(%d,%d) bgrx=%d,%d,%d\n",
		cfg.Width/8, cfg.Height/2, leftB, leftG, leftR, 7*cfg.Width/8, cfg.Height/2, rightB, rightG, rightR)

	if err := overlay(anim.StyleNone, 0, classic); err != nil {
		return b.String(), err
	}
	nB, nG, nR, _, err := gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	if nB != tile.B || nG != tile.G || nR != tile.R {
		return b.String(), fmt.Errorf("none overlay mutated tile %d,%d,%d", nB, nG, nR)
	}
	fmt.Fprintf(&b, "none-noop tile bgrx=%d,%d,%d\n", nB, nG, nR)

	neon := theme.Arcade()
	if err := overlay(anim.ParseStyle(neon.Transition), 0.45, neon); err != nil {
		return b.String(), err
	}
	gB, gG, gR, _, err := gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	if gB == tile.B && gG == tile.G && gR == tile.R {
		return b.String(), fmt.Errorf("glitch-mid unchanged")
	}
	glitchPix := [3]byte{gB, gG, gR}
	if err := overlay(anim.StyleCurtain, 0.45, classic); err != nil {
		return b.String(), err
	}
	cuB, cuG, cuR, _, err := gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	if cuB == glitchPix[0] && cuG == glitchPix[1] && cuR == glitchPix[2] {
		return b.String(), fmt.Errorf("glitch-mid matched curtain-mid")
	}
	fmt.Fprintf(&b, "glitch-mid tile bgrx=%d,%d,%d neon_transition=%s curtain-mid bgrx=%d,%d,%d\n",
		gB, gG, gR, neon.Transition, cuB, cuG, cuR)

	now := time.Unix(20, 0)
	var fx sceneFX
	fx.observe(modelScene(m), anim.StyleCurtain, now)
	if fx.active(now) {
		return b.String(), fmt.Errorf("first observe should not start fx")
	}
	press(&m, "y")
	if m.Browse != fbgrid.BrowseCoverflow {
		return b.String(), fmt.Errorf("y browse %s", m.Browse)
	}
	fx.observe(modelScene(m), kitTransitionStyle(kitLook(m, th), false), now)
	if !fx.active(now) || fx.style != anim.StyleCurtain {
		return b.String(), fmt.Errorf("layout fx active=%v style=%s", fx.active(now), fx.style)
	}
	if fx.progress(now) != 0 {
		return b.String(), fmt.Errorf("layout fx t0 %v", fx.progress(now))
	}
	if fx.active(now.Add(anim.CurtainDuration)) {
		return b.String(), fmt.Errorf("layout fx still active after duration")
	}

	press(&m, "x")
	if m.Pack != theme.PackNeon {
		return b.String(), fmt.Errorf("x pack %s", m.Pack)
	}
	fx.observe(modelScene(m), kitTransitionStyle(kitLook(m, th), false), now.Add(time.Second))
	if fx.style != anim.StyleGlitch {
		return b.String(), fmt.Errorf("pack fx style %s", fx.style)
	}

	press(&m, "b")
	if !m.DetailOpen {
		return b.String(), fmt.Errorf("b did not open detail")
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, now); action != "launch" {
		return b.String(), fmt.Errorf("A during detail fx %q", action)
	}
	fmt.Fprintf(&b, "hooks layout=%s pack=%s detail=1 launch-still-a=1 disabled=%s\n",
		m.Browse, m.Pack, kitTransitionStyle(classic, true))

	if kitTransitionStyle(theme.Theme{Transition: "none"}.Complete(), false) != anim.StyleNone {
		return b.String(), fmt.Errorf("theme none should be no-op")
	}

	packs, err := exercisePacksGrid(d, th)
	b.WriteString(packs)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-transition PASS curtain=1 wipe=1 glitch=1 none=1 nested-packs=1\n")
	return b.String(), nil
}

func exercisePacksGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	cfg := d.Config()
	var b strings.Builder
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, Pack: theme.PackClassic}
	m.SetCatalog(mixedShelfGames())
	sample := func(label string) (hlB, hlG, hlR, bgB, bgG, bgR byte, header string, err error) {
		look := kitLook(m, th)
		g := paintModel(d, m, look)
		hx, hy, ok := g.HighlightSample()
		if !ok {
			return 0, 0, 0, 0, 0, 0, g.Header, fmt.Errorf("%s: no highlight", label)
		}
		hlB, hlG, hlR, _, err = gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
		if err != nil {
			return 0, 0, 0, 0, 0, 0, g.Header, err
		}
		bgY := g.HeaderH + 2
		if bgY < 0 {
			bgY = 0
		}
		bgB, bgG, bgR, _, err = gfx.SampleBGRX(d.Destination(), cfg, 2, bgY)
		if err != nil {
			return 0, 0, 0, 0, 0, 0, g.Header, err
		}
		if hlB != look.Highlight.B || hlG != look.Highlight.G || hlR != look.Highlight.R {
			return 0, 0, 0, 0, 0, 0, g.Header, fmt.Errorf("%s: highlight bgrx %d,%d,%d want %d,%d,%d", label, hlB, hlG, hlR, look.Highlight.B, look.Highlight.G, look.Highlight.R)
		}
		if bgB != look.Background.B || bgG != look.Background.G || bgR != look.Background.R {
			return 0, 0, 0, 0, 0, 0, g.Header, fmt.Errorf("%s: background bgrx %d,%d,%d want %d,%d,%d", label, bgB, bgG, bgR, look.Background.B, look.Background.G, look.Background.R)
		}
		fmt.Fprintf(&b, "pack=%s id=%s highlight=(%d,%d) hl_bgrx=%d,%d,%d bg=(2,%d) bg_bgrx=%d,%d,%d header=%q title_px=%d header_bar=%s\n",
			label, m.Pack, hx, hy, hlB, hlG, hlR, bgY, bgB, bgG, bgR, g.Header, look.TitlePx(), theme.FormatColor(look.HeaderBar))
		return hlB, hlG, hlR, bgB, bgG, bgR, g.Header, nil
	}

	cHLB, cHLG, cHLR, cBGB, cBGG, cBGR, classicHeader, err := sample("classic")
	if err != nil {
		return b.String(), err
	}
	if strings.Contains(classicHeader, "NEON") || strings.Contains(classicHeader, "DIM") {
		return b.String(), fmt.Errorf("classic header tagged %q", classicHeader)
	}

	press(&m, "x")
	if m.Pack != theme.PackNeon {
		return b.String(), fmt.Errorf("x1 pack %s", m.Pack)
	}
	nHLB, nHLG, nHLR, nBGB, nBGG, nBGR, neonHeader, err := sample("neon")
	if err != nil {
		return b.String(), err
	}
	if !strings.Contains(neonHeader, "NEON") {
		return b.String(), fmt.Errorf("neon header %q", neonHeader)
	}
	if nHLB == cHLB && nHLG == cHLG && nHLR == cHLR && nBGB == cBGB && nBGG == cBGG && nBGR == cBGR {
		return b.String(), fmt.Errorf("classic and neon sampled the same pixels")
	}

	press(&m, "x")
	if m.Pack != theme.PackSofaDim {
		return b.String(), fmt.Errorf("x2 pack %s", m.Pack)
	}
	dHLB, dHLG, dHLR, dBGB, dBGG, dBGR, dimHeader, err := sample("sofa-dim")
	if err != nil {
		return b.String(), err
	}
	if !strings.Contains(dimHeader, "DIM") {
		return b.String(), fmt.Errorf("dim header %q", dimHeader)
	}
	if (dHLB == nHLB && dHLG == nHLG && dHLR == nHLR && dBGB == nBGB && dBGG == nBGG && dBGR == nBGR) ||
		(dHLB == cHLB && dHLG == cHLG && dHLR == cHLR && dBGB == cBGB && dBGG == cBGG && dBGR == cBGR) {
		return b.String(), fmt.Errorf("sofa-dim sampled the same as another pack")
	}

	press(&m, "x")
	if m.Pack != theme.PackClassic {
		return b.String(), fmt.Errorf("x3 pack %s", m.Pack)
	}
	_, _, _, _, _, _, backHeader, err := sample("classic-wrap")
	if err != nil {
		return b.String(), err
	}
	if strings.Contains(backHeader, "NEON") || strings.Contains(backHeader, "DIM") {
		return b.String(), fmt.Errorf("wrap header tagged %q", backHeader)
	}

	press(&m, "x")
	if m.Pack != theme.PackNeon {
		return b.String(), fmt.Errorf("x4 pack %s", m.Pack)
	}
	press(&m, "dpad-right")
	if m.Focus != 1 || m.Pack != theme.PackNeon || m.Browse != fbgrid.BrowseGrid {
		return b.String(), fmt.Errorf("dpad after x focus=%d pack=%s browse=%s", m.Focus, m.Pack, m.Browse)
	}
	press(&m, "y")
	if m.Browse != fbgrid.BrowseCoverflow || m.Pack != theme.PackNeon || m.Focus != 1 {
		return b.String(), fmt.Errorf("y after x browse=%s pack=%s focus=%d", m.Browse, m.Pack, m.Focus)
	}
	look := kitLook(m, th)
	g := paintModel(d, m, look)
	if g.Kind != fbgrid.BrowseCoverflow || !strings.Contains(g.Header, "FLOW") || !strings.Contains(g.Header, "NEON") {
		return b.String(), fmt.Errorf("coverflow+neon header=%q kind=%s", g.Header, g.Kind)
	}
	fmt.Fprintf(&b, "coverflow-neon kind=%s focus=%d header=%q footer=%q\n", g.Kind, m.Focus, g.Header, g.Footer)

	wheel := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, WheelOpen: true, Pack: theme.PackClassic}
	wheel.SetCatalog(mixedShelfGames())
	press(&wheel, "x")
	if wheel.Pack != theme.PackNeon || !wheel.WheelOpen {
		return b.String(), fmt.Errorf("wheel x pack=%s open=%v", wheel.Pack, wheel.WheelOpen)
	}
	wlook := kitLook(wheel, th)
	frame := modelWheelFrame(wheel, nil, nil, nil, wlook, cfg.Width, cfg.Height)
	fbgrid.PaintWheel(d, frame)
	d.Present()
	if !strings.Contains(frame.Header, "NEON") {
		return b.String(), fmt.Errorf("wheel header %q", frame.Header)
	}
	if frame.Footer != "A open | L/R platform | X dim" {
		return b.String(), fmt.Errorf("wheel footer %q", frame.Footer)
	}
	fmt.Fprintf(&b, "wheel-neon header=%q footer=%q\n", frame.Header, frame.Footer)

	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "launch" {
		return b.String(), fmt.Errorf("A after packs %q", action)
	}
	fmt.Fprintf(&b, "launch-still-a=1 focus=%d pack=%s browse=%s\n", m.Focus, m.Pack, m.Browse)

	layouts, err := exerciseLayoutsGrid(d, th)
	b.WriteString(layouts)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-packs PASS classic_hl=%d,%d,%d neon_hl=%d,%d,%d dim_hl=%d,%d,%d classic_bg=%d,%d,%d neon_bg=%d,%d,%d dim_bg=%d,%d,%d cycle=1 nested-layouts=1\n",
		cHLB, cHLG, cHLR, nHLB, nHLG, nHLR, dHLB, dHLG, dHLR, cBGB, cBGG, cBGR, nBGB, nBGG, nBGR, dBGB, dBGG, dBGR)
	return b.String(), nil
}

func runLayoutsSelftest(fbPath string, th theme.Theme) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseLayoutsGrid(d, th)
	fmt.Print(report)
	return err
}

func exerciseLayoutsGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	cfg := d.Config()
	var b strings.Builder
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	g := paintModel(d, m, th)
	if g.Kind != fbgrid.BrowseGrid || len(g.Tiles) != 12 {
		return b.String(), fmt.Errorf("origin kind=%s tiles=%d", g.Kind, len(g.Tiles))
	}
	if g.Footer != "A play | B detail | L/R | Y flow" {
		return b.String(), fmt.Errorf("grid footer %q", g.Footer)
	}
	gridW := g.CellW
	hx, hy, ok := g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("grid highlight")
	}
	hlB, hlG, hlR, _, err := gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "origin kind=%s tiles=%d cellw=%d highlight=(%d,%d) bgrx=%d,%d,%d footer=%q\n",
		g.Kind, len(g.Tiles), gridW, hx, hy, hlB, hlG, hlR, g.Footer)

	press(&m, "y")
	if m.Browse != fbgrid.BrowseCoverflow {
		return b.String(), fmt.Errorf("y1 browse %s", m.Browse)
	}
	g = paintModel(d, m, th)
	if g.Kind != fbgrid.BrowseCoverflow || !strings.Contains(g.Header, "FLOW") {
		return b.String(), fmt.Errorf("coverflow kind=%s header=%q", g.Kind, g.Header)
	}
	if g.Footer != "A play | B detail | L/R | Y wall" {
		return b.String(), fmt.Errorf("coverflow footer %q", g.Footer)
	}
	fr, ok := g.TileRect(g.Focus)
	if !ok {
		return b.String(), fmt.Errorf("coverflow focus rect")
	}
	nr, ok := g.TileRect(1)
	if !ok || len(g.Tiles) < 2 {
		return b.String(), fmt.Errorf("coverflow neighbor")
	}
	if fr.W <= nr.W {
		return b.String(), fmt.Errorf("coverflow focus %+v neighbor %+v", fr, nr)
	}
	hx, hy, ok = g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("coverflow highlight")
	}
	hlB, hlG, hlR, hlX, err := gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	if hlB != th.Highlight.B || hlG != th.Highlight.G || hlR != th.Highlight.R || hlX != 0 {
		return b.String(), fmt.Errorf("coverflow highlight bgrx %d,%d,%d,%d", hlB, hlG, hlR, hlX)
	}
	rec := gfx.NewRecorder()
	fbgrid.Paint(rec, g)
	var sawTitle bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.SizePx == th.TitlePx() && c.Weight == th.TitleWeight() && c.Text != g.Header {
			sawTitle = true
		}
		if c.Op == "DebugText" {
			return b.String(), fmt.Errorf("coverflow DebugText")
		}
	}
	if !sawTitle {
		return b.String(), fmt.Errorf("coverflow missing title role ops=%v", rec.Ops())
	}
	fmt.Fprintf(&b, "coverflow kind=%s tiles=%d focusW=%d neighborW=%d header=%q footer=%q\n",
		g.Kind, len(g.Tiles), int(fr.W), int(nr.W), g.Header, g.Footer)

	press(&m, "dpad-right")
	if m.Focus != 1 || m.Browse != fbgrid.BrowseCoverflow {
		return b.String(), fmt.Errorf("coverflow dpad focus=%d browse=%s", m.Focus, m.Browse)
	}
	g = paintModel(d, m, th)
	if g.Focus != 1 {
		return b.String(), fmt.Errorf("coverflow local focus %d", g.Focus)
	}
	fmt.Fprintf(&b, "coverflow-right focus=%d local=%d\n", m.Focus, g.Focus)

	press(&m, "y")
	if m.Browse != fbgrid.BrowseWall {
		return b.String(), fmt.Errorf("y2 browse %s", m.Browse)
	}
	g = paintModel(d, m, th)
	if g.Kind != fbgrid.BrowseWall || g.Columns != 6 || !strings.Contains(g.Header, "WALL") {
		return b.String(), fmt.Errorf("wall kind=%s cols=%d header=%q", g.Kind, g.Columns, g.Header)
	}
	if g.CellW >= gridW {
		return b.String(), fmt.Errorf("wall cell %d not denser than grid %d", g.CellW, gridW)
	}
	hx, hy, ok = g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("wall highlight")
	}
	hlB, hlG, hlR, hlX, err = gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	if hlB != th.Highlight.B || hlG != th.Highlight.G || hlR != th.Highlight.R || hlX != 0 {
		return b.String(), fmt.Errorf("wall highlight bgrx %d,%d,%d,%d", hlB, hlG, hlR, hlX)
	}
	fmt.Fprintf(&b, "wall kind=%s tiles=%d cols=%d cellw=%d header=%q footer=%q\n",
		g.Kind, len(g.Tiles), g.Columns, g.CellW, g.Header, g.Footer)

	press(&m, "y")
	if m.Browse != fbgrid.BrowseSplit {
		return b.String(), fmt.Errorf("y3 browse %s", m.Browse)
	}
	g = paintModel(d, m, th)
	if g.Kind != fbgrid.BrowseSplit || g.Columns != 1 || !strings.Contains(g.Header, "SPLIT") {
		return b.String(), fmt.Errorf("split kind=%s cols=%d header=%q", g.Kind, g.Columns, g.Header)
	}
	if g.Footer != "A play | B detail | L/R | Y grid" {
		return b.String(), fmt.Errorf("split footer %q", g.Footer)
	}
	row, ok := g.TileRect(g.Focus)
	if !ok {
		return b.String(), fmt.Errorf("split list row")
	}
	hero, ok := g.SplitHeroCover()
	if !ok || hero.W <= row.W || hero.H <= row.H {
		return b.String(), fmt.Errorf("split hero %+v row %+v", hero, row)
	}
	hx, hy, ok = g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("split highlight")
	}
	hlB, hlG, hlR, hlX, err = gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	if hlB != th.Highlight.B || hlG != th.Highlight.G || hlR != th.Highlight.R || hlX != 0 {
		return b.String(), fmt.Errorf("split highlight bgrx %d,%d,%d,%d", hlB, hlG, hlR, hlX)
	}
	if g.Focus < 0 || g.Focus >= len(g.Tiles) || g.Tiles[g.Focus].Meta == "" {
		return b.String(), fmt.Errorf("split missing meta")
	}
	rec = gfx.NewRecorder()
	fbgrid.Paint(rec, g)
	var sawHeroTitle, sawMeta bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.SizePx == th.TitlePx() && c.Weight == th.TitleWeight() && c.Text != g.Header {
			sawHeroTitle = true
		}
		if c.Op == "DrawText" && c.SizePx == th.BodyPx() && c.Text == g.Tiles[g.Focus].Meta {
			sawMeta = true
		}
		if c.Op == "DebugText" {
			return b.String(), fmt.Errorf("split DebugText")
		}
	}
	if !sawHeroTitle || !sawMeta {
		return b.String(), fmt.Errorf("split chrome title=%v meta=%v ops=%v", sawHeroTitle, sawMeta, rec.Ops())
	}
	fmt.Fprintf(&b, "split kind=%s tiles=%d heroW=%d rowW=%d meta=%q header=%q footer=%q\n",
		g.Kind, len(g.Tiles), int(hero.W), int(row.W), g.Tiles[g.Focus].Meta, g.Header, g.Footer)

	press(&m, "dpad-down")
	if m.Focus != 2 || m.Browse != fbgrid.BrowseSplit {
		return b.String(), fmt.Errorf("split dpad focus=%d browse=%s", m.Focus, m.Browse)
	}
	g = paintModel(d, m, th)
	if g.Focus != 2 {
		return b.String(), fmt.Errorf("split local focus %d", g.Focus)
	}
	fmt.Fprintf(&b, "split-down focus=%d local=%d\n", m.Focus, g.Focus)

	press(&m, "y")
	if m.Browse != fbgrid.BrowseGrid {
		return b.String(), fmt.Errorf("y4 browse %s", m.Browse)
	}
	g = paintModel(d, m, th)
	if g.Kind != fbgrid.BrowseGrid || strings.Contains(g.Header, "FLOW") || strings.Contains(g.Header, "WALL") || strings.Contains(g.Header, "SPLIT") {
		return b.String(), fmt.Errorf("back grid kind=%s header=%q", g.Kind, g.Header)
	}
	fmt.Fprintf(&b, "back-grid kind=%s focus=%d header=%q\n", g.Kind, m.Focus, g.Header)

	empty := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, Browse: fbgrid.BrowseCoverflow}
	hidden := paintModel(d, empty, th)
	if len(hidden.Tiles) != 0 {
		return b.String(), fmt.Errorf("empty coverflow tiles %d", len(hidden.Tiles))
	}
	if _, _, ok := hidden.HighlightSample(); ok {
		return b.String(), fmt.Errorf("empty coverflow highlight")
	}
	fmt.Fprintf(&b, "empty-coverflow tiles=%d header=%q\n", len(hidden.Tiles), hidden.Header)

	emptySplit := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, Browse: fbgrid.BrowseSplit}
	hiddenSplit := paintModel(d, emptySplit, th)
	if len(hiddenSplit.Tiles) != 0 {
		return b.String(), fmt.Errorf("empty split tiles %d", len(hiddenSplit.Tiles))
	}
	if _, _, ok := hiddenSplit.HighlightSample(); ok {
		return b.String(), fmt.Errorf("empty split highlight")
	}
	fmt.Fprintf(&b, "empty-split tiles=%d header=%q\n", len(hiddenSplit.Tiles), hiddenSplit.Header)

	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "launch" {
		return b.String(), fmt.Errorf("grid A after layouts %q", action)
	}
	fmt.Fprintf(&b, "launch-still-a=1 focus=%d\n", m.Focus)

	atm, err := exerciseAtmosphereGrid(d, th)
	b.WriteString(atm)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-layouts PASS coverflow=1 wall=1 split=1 cycle=1 empty=1 nested-atmosphere=1\n")
	return b.String(), nil
}

func exerciseAtmosphereGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	cfg := d.Config()
	var b strings.Builder
	art := gfx.RGB(40, 180, 80)
	fanart := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			fanart.SetRGBA(x, y, color.RGBA{R: art.R, G: art.G, B: art.B, A: 255})
		}
	}
	g := fbgrid.New(cfg.Width, cfg.Height)
	fbgrid.ApplyTheme(&g, th)
	g.Header = "FOGCAST  ATMOSPHERE"
	g.Atmosphere = fanart
	fbgrid.Paint(d, g)
	d.Present()
	sx, sy, ok := fbgrid.AtmosphereSample(g)
	if !ok {
		return b.String(), fmt.Errorf("fanart stage sample")
	}
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), cfg, sx, sy)
	if err != nil {
		return b.String(), err
	}
	dim := fbgrid.AtmosphereDim(art, th.Background)
	fmt.Fprintf(&b, "fanart stage=(%d,%d) bgrx=%d,%d,%d,%d dim=%d,%d,%d\n",
		sx, sy, gotB, gotG, gotR, gotX, dim.B, dim.G, dim.R)
	if gotB != dim.B || gotG != dim.G || gotR != dim.R || gotX != 0 {
		return b.String(), fmt.Errorf("fanart stage bgrx %d,%d,%d,%d want %d,%d,%d,0", gotB, gotG, gotR, gotX, dim.B, dim.G, dim.R)
	}
	hx, hy, ok := g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("fanart highlight")
	}
	hlB, hlG, hlR, _, err := gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	if hlB != th.Highlight.B || hlG != th.Highlight.G || hlR != th.Highlight.R {
		return b.String(), fmt.Errorf("fanart highlight bgrx %d,%d,%d", hlB, hlG, hlR)
	}

	plain := fbgrid.New(cfg.Width, cfg.Height)
	fbgrid.ApplyTheme(&plain, th)
	fbgrid.Paint(d, plain)
	d.Present()
	px, py, ok := fbgrid.AtmosphereSample(plain)
	if !ok {
		return b.String(), fmt.Errorf("plain stage sample")
	}
	pB, pG, pR, _, err := gfx.SampleBGRX(d.Destination(), cfg, px, py)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "absent stage=(%d,%d) bgrx=%d,%d,%d bg=%d,%d,%d\n",
		px, py, pB, pG, pR, th.Background.B, th.Background.G, th.Background.R)
	if pB != th.Background.B || pG != th.Background.G || pR != th.Background.R {
		return b.String(), fmt.Errorf("absent stage bgrx %d,%d,%d want background", pB, pG, pR)
	}

	cover := gfx.RGB(200, 40, 40)
	coverImg := image.NewRGBA(image.Rect(0, 0, 8, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 8; x++ {
			coverImg.SetRGBA(x, y, color.RGBA{R: cover.R, G: cover.G, B: cover.B, A: 255})
		}
	}
	wall := fbgrid.NewWithTiles(cfg.Width, cfg.Height, []fbgrid.Tile{
		{Name: "A", Color: th.SystemColor("megadrive"), Cover: coverImg, CoverKind: fbgrid.CoverPresent},
		{Name: "B", Color: th.SystemColor("snes"), Cover: coverImg, CoverKind: fbgrid.CoverPresent},
	})
	fbgrid.ApplyTheme(&wall, th)
	fbgrid.Paint(d, wall)
	d.Present()
	wx, wy, ok := fbgrid.AtmosphereSample(wall)
	if !ok {
		return b.String(), fmt.Errorf("cover-wall stage sample")
	}
	wB, wG, wR, _, err := gfx.SampleBGRX(d.Destination(), cfg, wx, wy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "cover-wall stage=(%d,%d) bgrx=%d,%d,%d\n", wx, wy, wB, wG, wR)
	if wB == th.Background.B && wG == th.Background.G && wR == th.Background.R {
		return b.String(), fmt.Errorf("cover-wall stage stayed background")
	}

	hero := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			hero.SetRGBA(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	fbgrid.PaintWheel(d, fbgrid.WheelFrame{
		Width: cfg.Width, Height: cfg.Height, Title: "MEGADRIVE", Stats: "3 games",
		Hero: hero, HeroKind: fbgrid.CoverPresent,
		Color: th.SystemColor("megadrive"),
		Items: []fbgrid.WheelItem{{ID: "megadrive", Label: "MEGADRIVE", Color: th.SystemColor("megadrive")}},
		Theme: th,
	})
	d.Present()
	whx, why, ok := fbgrid.WheelHeroSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("wheel hero")
	}
	hB, hG, hR, _, err := gfx.SampleBGRX(d.Destination(), cfg, whx, why)
	if err != nil {
		return b.String(), err
	}
	asx, asy, ok := fbgrid.WheelAtmosphereSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("wheel stage")
	}
	aB, aG, aR, _, err := gfx.SampleBGRX(d.Destination(), cfg, asx, asy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "wheel hero=(%d,%d) bgrx=%d,%d,%d stage=(%d,%d) bgrx=%d,%d,%d\n",
		whx, why, hB, hG, hR, asx, asy, aB, aG, aR)
	if hB != 160 || hG != 32 || hR != 255 {
		return b.String(), fmt.Errorf("wheel hero bgrx %d,%d,%d", hB, hG, hR)
	}
	wheelDim := fbgrid.AtmosphereDim(gfx.RGB(255, 32, 160), th.Background)
	if aB != wheelDim.B || aG != wheelDim.G || aR != wheelDim.R {
		return b.String(), fmt.Errorf("wheel stage bgrx %d,%d,%d want %d,%d,%d", aB, aG, aR, wheelDim.B, wheelDim.G, wheelDim.R)
	}

	fbgrid.PaintDetail(d, fbgrid.DetailFrame{
		Width: cfg.Width, Height: cfg.Height, Header: "FOGCAST", Title: "Sonic",
		Hint:  "A play | B back",
		Cover: hero, CoverKind: fbgrid.CoverPresent,
		Atmosphere: fanart,
		Color:      th.SystemColor("megadrive"), Theme: th,
	})
	d.Present()
	cx, cy, ok := fbgrid.DetailCoverSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("detail cover")
	}
	cB, cG, cR, _, err := gfx.SampleBGRX(d.Destination(), cfg, cx, cy)
	if err != nil {
		return b.String(), err
	}
	dsx, dsy, ok := fbgrid.DetailAtmosphereSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("detail stage")
	}
	dB, dG, dR, _, err := gfx.SampleBGRX(d.Destination(), cfg, dsx, dsy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "detail cover=(%d,%d) bgrx=%d,%d,%d stage=(%d,%d) bgrx=%d,%d,%d\n",
		cx, cy, cB, cG, cR, dsx, dsy, dB, dG, dR)
	if cB != 160 || cG != 32 || cR != 255 {
		return b.String(), fmt.Errorf("detail cover bgrx %d,%d,%d", cB, cG, cR)
	}
	if dB != dim.B || dG != dim.G || dR != dim.R {
		return b.String(), fmt.Errorf("detail stage bgrx %d,%d,%d want %d,%d,%d", dB, dG, dR, dim.B, dim.G, dim.R)
	}

	strip, err := exerciseStripGrid(d, th)
	b.WriteString(strip)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-atmosphere PASS fanart=1 absent=1 cover-wall=1 wheel=1 detail=1 nested-strip=1\n")
	return b.String(), nil
}

func exerciseStripGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	recents := []tenfoot.Game{
		{ID: "megadrive-02", Title: "MEGADRIVE 02", System: "megadrive", Launchable: true},
		{ID: "snes-07", Title: "SNES 07", System: "snes", Launchable: true},
	}
	m.SetStrip(recents, "Recent")
	cfg := d.Config()
	var b strings.Builder
	g := paintModel(d, m, th)
	if len(g.Strip) != 2 || g.StripLabel != "Recent" || g.StripActive {
		return b.String(), fmt.Errorf("idle strip tiles=%d label=%q active=%v", len(g.Strip), g.StripLabel, g.StripActive)
	}
	if g.CellH < 60 {
		return b.String(), fmt.Errorf("strip crushed grid cell %d", g.CellH)
	}
	rec := gfx.NewRecorder()
	fbgrid.Paint(rec, g)
	var sawLabel bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Recent" && c.SizePx == th.CaptionPx() {
			sawLabel = true
		}
		if c.Op == "DebugText" {
			return b.String(), fmt.Errorf("strip DebugText")
		}
	}
	if !sawLabel {
		return b.String(), fmt.Errorf("missing Recent caption ops=%v", rec.Ops())
	}
	hx, hy, ok := g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("grid highlight")
	}
	hlB, hlG, hlR, _, err := gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "idle-grid strip=%d label=%q highlight=(%d,%d) bgrx=%d,%d,%d cellh=%d\n",
		len(g.Strip), g.StripLabel, hx, hy, hlB, hlG, hlR, g.CellH)

	m.Focus = len(m.Games) - 1
	press(&m, "dpad-down")
	if !m.StripActive || m.DetailOpen || m.StripFocus != 0 {
		return b.String(), fmt.Errorf("enter strip active=%v detail=%v focus=%d", m.StripActive, m.DetailOpen, m.StripFocus)
	}
	g = paintModel(d, m, th)
	sx, sy, ok := g.StripHighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("strip highlight")
	}
	sB, sG, sR, sX, err := gfx.SampleBGRX(d.Destination(), cfg, sx, sy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "enter-strip focus=%d highlight=(%d,%d) bgrx=%d,%d,%d,%d footer=%q\n",
		m.StripFocus, sx, sy, sB, sG, sR, sX, g.Footer)
	if sB != th.Highlight.B || sG != th.Highlight.G || sR != th.Highlight.R || sX != 0 {
		return b.String(), fmt.Errorf("strip highlight bgrx %d,%d,%d,%d", sB, sG, sR, sX)
	}
	if g.Footer != "A detail | B grid | L/R" {
		return b.String(), fmt.Errorf("strip footer %q", g.Footer)
	}

	press(&m, "dpad-right")
	if m.StripFocus != 1 {
		return b.String(), fmt.Errorf("strip right %d", m.StripFocus)
	}
	press(&m, "dpad-right")
	if m.StripFocus != 1 {
		return b.String(), fmt.Errorf("strip right clamp %d", m.StripFocus)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "" || !m.DetailOpen {
		return b.String(), fmt.Errorf("strip A action=%q detail=%v", action, m.DetailOpen)
	}
	game, ok := m.FocusedGame()
	if !ok || game.ID != "snes-07" {
		return b.String(), fmt.Errorf("strip detail game %+v ok=%v", game, ok)
	}
	fmt.Fprintf(&b, "strip-detail id=%s title=%q\n", game.ID, game.Title)
	press(&m, "b")
	if m.DetailOpen || !m.StripActive || m.StripFocus != 1 {
		return b.String(), fmt.Errorf("detail B strip active=%v detail=%v focus=%d", m.StripActive, m.DetailOpen, m.StripFocus)
	}
	press(&m, "b")
	if m.StripActive || m.DetailOpen || m.Focus != len(m.Games)-1 {
		return b.String(), fmt.Errorf("strip B grid active=%v detail=%v focus=%d", m.StripActive, m.DetailOpen, m.Focus)
	}
	fmt.Fprintf(&b, "back-grid focus=%d strip=%v\n", m.Focus, m.StripActive)

	emptyGames := make([]tenfoot.Game, 8)
	for i := range emptyGames {
		emptyGames[i] = tenfoot.Game{ID: fmt.Sprintf("g%d", i), Title: "T", System: "snes", Launchable: true}
	}
	empty := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, Games: emptyGames}
	empty.Focus = 7
	press(&empty, "dpad-down")
	if !empty.DetailOpen || empty.StripActive {
		return b.String(), fmt.Errorf("empty strip last-row down detail=%v strip=%v", empty.DetailOpen, empty.StripActive)
	}
	m.SetStrip(nil, "Recent")
	hidden := paintModelGrid(m, th, cfg.Width, cfg.Height)
	if len(hidden.Strip) != 0 {
		return b.String(), fmt.Errorf("hidden strip still has %d tiles", len(hidden.Strip))
	}
	hideRec := gfx.NewRecorder()
	fbgrid.Paint(hideRec, hidden)
	for _, c := range hideRec.Calls {
		if c.Op == "DrawText" && c.Text == "Recent" {
			return b.String(), fmt.Errorf("hidden strip painted Recent")
		}
	}
	fmt.Fprintf(&b, "hide-empty tiles=%d\n", len(hidden.Strip))

	wheel, err := exerciseWheelGrid(d, th)
	b.WriteString(wheel)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-strip PASS enter=1 nav=1 detail=1 back=1 hide=1 nested-wheel=1\n")
	return b.String(), nil
}

func exerciseWheelGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true, WheelOpen: true}
	m.SetCatalog(mixedShelfGames())
	cfg := d.Config()
	var b strings.Builder
	step := func(name, wantShelf string, wantCount int, wantWheel bool) error {
		frame := paintWheel(d, m, th)
		hx, hy, ok := fbgrid.WheelFocusSample(cfg.Width, cfg.Height, th, len(frame.Items), frame.Focus)
		if !ok {
			return fmt.Errorf("%s: no wheel focus", name)
		}
		gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s shelf=%s games=%d wheel=%v header=%q stats=%q focus=(%d,%d) bgrx=%d,%d,%d,%d items=%d\n",
			name, m.Shelf, len(m.Games), m.WheelOpen, frame.Header, frame.Stats, hx, hy, gotB, gotG, gotR, gotX, len(frame.Items))
		if m.WheelOpen != wantWheel {
			return fmt.Errorf("%s: wheel %v want %v", name, m.WheelOpen, wantWheel)
		}
		if m.Shelf != wantShelf {
			return fmt.Errorf("%s: shelf %q want %q", name, m.Shelf, wantShelf)
		}
		if len(m.Games) != wantCount {
			return fmt.Errorf("%s: games %d want %d", name, len(m.Games), wantCount)
		}
		if gotB != th.Highlight.B || gotG != th.Highlight.G || gotR != th.Highlight.R || gotX != 0 {
			return fmt.Errorf("%s: highlight bgrx %d,%d,%d,%d", name, gotB, gotG, gotR, gotX)
		}
		return nil
	}
	if err := step("origin", kitlauncher.ShelfAll, 15, true); err != nil {
		return b.String(), err
	}
	press(&m, "r")
	if err := step("shoulder-r-pong", "pong", 2, true); err != nil {
		return b.String(), err
	}
	press(&m, "r")
	if err := step("shoulder-r-megadrive", "megadrive", 5, true); err != nil {
		return b.String(), err
	}
	hero := solidStill(255, 32, 160, 16, 12)
	frame := modelWheelFrame(m, nil, nil, nil, th, cfg.Width, cfg.Height)
	frame.Hero = hero
	frame.HeroKind = fbgrid.CoverPresent
	fbgrid.PaintWheel(d, frame)
	d.Present()
	hx, hy, ok := fbgrid.WheelHeroSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("hero sample")
	}
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "hero=(%d,%d) bgrx=%d,%d,%d,%d title=%q stats=%q featured=%q\n",
		hx, hy, gotB, gotG, gotR, gotX, frame.Title, frame.Stats, frame.Featured)
	if gotB != 160 || gotG != 32 || gotR != 255 || gotX != 0 {
		return b.String(), fmt.Errorf("hero bgrx %d,%d,%d,%d want 160,32,255,0", gotB, gotG, gotR, gotX)
	}
	if frame.Title != "MEGADRIVE" || frame.Stats != "5 games" {
		return b.String(), fmt.Errorf("hero chrome title=%q stats=%q", frame.Title, frame.Stats)
	}
	rec := gfx.NewRecorder()
	fbgrid.PaintWheel(rec, frame)
	var sawTitle, sawStats bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "MEGADRIVE" && c.SizePx == th.TitlePx() {
			if c.Weight != th.TitleWeight() {
				return b.String(), fmt.Errorf("wheel title weight %s", c.Weight)
			}
			sawTitle = true
		}
		if c.Op == "DrawText" && strings.Contains(c.Text, "5 games") {
			sawStats = true
		}
		if c.Op == "DebugText" {
			return b.String(), fmt.Errorf("wheel DebugText")
		}
	}
	if !sawTitle || !sawStats {
		return b.String(), fmt.Errorf("wheel text title=%v stats=%v ops=%v", sawTitle, sawStats, rec.Ops())
	}

	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "" {
		return b.String(), fmt.Errorf("wheel A %q", action)
	}
	if m.WheelOpen || m.Shelf != "megadrive" || len(m.Games) != 5 {
		return b.String(), fmt.Errorf("enter wheel=%v shelf=%s n=%d", m.WheelOpen, m.Shelf, len(m.Games))
	}
	g := paintModel(d, m, th)
	if len(g.Tiles) != 5 || g.Header != "FOGCAST  MEGADRIVE 5/15" {
		return b.String(), fmt.Errorf("grid header=%q tiles=%d", g.Header, len(g.Tiles))
	}
	if g.Footer != "A play | B platforms | L/R | Y flow" {
		return b.String(), fmt.Errorf("grid footer %q", g.Footer)
	}
	fmt.Fprintf(&b, "enter-grid header=%q tiles=%d footer=%q\n", g.Header, len(g.Tiles), g.Footer)
	press(&m, "b")
	if !m.WheelOpen || m.DetailOpen || m.Shelf != "megadrive" {
		return b.String(), fmt.Errorf("back wheel=%v detail=%v shelf=%s", m.WheelOpen, m.DetailOpen, m.Shelf)
	}
	if err := step("back-wheel-megadrive", "megadrive", 5, true); err != nil {
		return b.String(), err
	}

	motion, err := exerciseMotionGrid(d, th)
	b.WriteString(motion)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-wheel PASS enter=1 back=1 hero=1 nested-motion=1\n")
	return b.String(), nil
}

func paintWheel(d *gfx.LinuxFB, m kitlauncher.Model, th theme.Theme) fbgrid.WheelFrame {
	cfg := d.Config()
	frame := modelWheelFrame(m, nil, nil, nil, th, cfg.Width, cfg.Height)
	fbgrid.PaintWheel(d, frame)
	d.Present()
	return frame
}

func exerciseMotionGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	cfg := d.Config()
	g := fbgrid.New(cfg.Width, cfg.Height)
	fbgrid.ApplyTheme(&g, th)
	var b strings.Builder

	fbgrid.Paint(d, g)
	d.Present()
	hx, hy, ok := g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("origin highlight")
	}
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "motion origin focus=%d highlight=(%d,%d) bgrx=%d,%d,%d,%d\n", g.Focus, hx, hy, gotB, gotG, gotR, gotX)
	if gotB != th.Highlight.B || gotG != th.Highlight.G || gotR != th.Highlight.R || gotX != 0 {
		return b.String(), fmt.Errorf("origin highlight")
	}

	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: false})
	if g.Focus != 1 {
		return b.String(), fmt.Errorf("right focus %d", g.Focus)
	}
	fbgrid.Paint(d, g)
	d.Present()
	rest := append([]byte(nil), d.Destination()...)
	hx, hy, ok = g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("moved highlight")
	}
	gotB, gotG, gotR, gotX, err = gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "motion pop-t0 focus=%d highlight=(%d,%d) bgrx=%d,%d,%d,%d amount=%.3f\n", g.Focus, hx, hy, gotB, gotG, gotR, gotX, g.FocusPopAmount())
	if gotB != th.Highlight.B || gotG != th.Highlight.G || gotR != th.Highlight.R || gotX != 0 {
		return b.String(), fmt.Errorf("pop-t0 highlight")
	}
	if g.FocusPopAmount() != 0 {
		return b.String(), fmt.Errorf("pop-t0 amount %v", g.FocusPopAmount())
	}

	ox, oy, ok := g.CellOrigin(g.Focus)
	if !ok {
		return b.String(), fmt.Errorf("pop origin")
	}
	gapX, gapY := ox-2, oy+g.CellH/2
	restGapB, restGapG, restGapR, _, err := gfx.SampleBGRX(d.Destination(), cfg, gapX, gapY)
	if err != nil {
		return b.String(), err
	}
	x0, _, _ := g.CellOrigin(0)

	midPop := int(fbgrid.FocusPopDuration / fbgrid.TickPeriod / 2)
	for i := 0; i < midPop; i++ {
		g.Tick()
	}
	if g.FocusPopAmount() <= 0.5 {
		return b.String(), fmt.Errorf("mid-pop amount %v", g.FocusPopAmount())
	}
	fbgrid.Paint(d, g)
	d.Present()
	if bytes.Equal(rest, d.Destination()) {
		return b.String(), fmt.Errorf("mid-pop paint matched rest")
	}
	popB, popG, popR, _, err := gfx.SampleBGRX(d.Destination(), cfg, gapX, gapY)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "motion pop-mid focus=%d amount=%.3f scale=%.3f gap=(%d,%d) bgrx=%d,%d,%d rest-gap=%d,%d,%d origin0=%d\n",
		g.Focus, g.FocusPopAmount(), g.FocusScale(), gapX, gapY, popB, popG, popR, restGapB, restGapG, restGapR, x0)
	if popB == restGapB && popG == restGapG && popR == restGapR {
		return b.String(), fmt.Errorf("mid-pop gap pixel unchanged")
	}
	hx, hy, ok = g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("mid-pop highlight")
	}
	gotB, gotG, gotR, gotX, err = gfx.SampleBGRX(d.Destination(), cfg, hx, hy)
	if err != nil {
		return b.String(), err
	}
	if gotB != th.Highlight.B || gotG != th.Highlight.G || gotR != th.Highlight.R || gotX != 0 {
		return b.String(), fmt.Errorf("mid-pop highlight bgrx %d,%d,%d,%d", gotB, gotG, gotR, gotX)
	}
	x0b, _, _ := g.CellOrigin(0)
	if x0b != x0 {
		return b.String(), fmt.Errorf("unfocused origin moved %d -> %d", x0, x0b)
	}

	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: false})
	if g.ConfirmLeft != fbgrid.ConfirmFrames || g.Selected == "" {
		return b.String(), fmt.Errorf("confirm left=%d selected=%q", g.ConfirmLeft, g.Selected)
	}
	fbgrid.Paint(d, g)
	d.Present()
	sx, sy, ok := g.CellOrigin(g.ConfirmIndex)
	if !ok {
		return b.String(), fmt.Errorf("confirm origin")
	}
	ix, iy := sx+g.CellW/2, sy+g.CellH/2
	gotB, gotG, gotR, gotX, err = gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "motion confirm-t0 index=%d interior=(%d,%d) bgrx=%d,%d,%d,%d amount=%.3f\n",
		g.ConfirmIndex, ix, iy, gotB, gotG, gotR, gotX, g.ConfirmPulseAmount())
	if gotB != th.Flash.B || gotG != th.Flash.G || gotR != th.Flash.R || gotX != 0 {
		return b.String(), fmt.Errorf("confirm-t0 interior %d,%d,%d,%d want flash", gotB, gotG, gotR, gotX)
	}
	if g.ConfirmPulseAmount() != 1 {
		return b.String(), fmt.Errorf("confirm-t0 amount %v", g.ConfirmPulseAmount())
	}

	midPulse := fbgrid.ConfirmFrames / 2
	for i := 0; i < midPulse; i++ {
		g.Tick()
	}
	fbgrid.Paint(d, g)
	d.Present()
	midB, midG, midR, _, err := gfx.SampleBGRX(d.Destination(), cfg, ix, iy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "motion confirm-mid left=%d amount=%.3f interior bgrx=%d,%d,%d\n", g.ConfirmLeft, g.ConfirmPulseAmount(), midB, midG, midR)
	if midB == th.Flash.B && midG == th.Flash.G && midR == th.Flash.R {
		return b.String(), fmt.Errorf("confirm-mid still full flash")
	}
	tile := g.Tiles[g.ConfirmIndex].Color
	if midB == tile.B && midG == tile.G && midR == tile.R {
		return b.String(), fmt.Errorf("confirm-mid already tile fill")
	}
	if g.ConfirmPulseAmount() <= 0.2 || g.ConfirmPulseAmount() >= 0.95 {
		return b.String(), fmt.Errorf("confirm-mid amount %v", g.ConfirmPulseAmount())
	}

	detail, err := exerciseDetailGrid(d, th)
	b.WriteString(detail)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-motion PASS pop=1 confirm=1 nested-detail=1\n")
	return b.String(), nil
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
		start, end := catalogPage(m.Focus, len(m.Games), m.Browse)
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
	look := kitLook(m, th)
	g := modelGrid(m, cfg.Width, cfg.Height, nil, nil, look)
	fbgrid.Paint(d, g)
	if m.SearchOpen {
		fbgrid.PaintOSK(d, modelOSKFrame(m, cfg.Width, cfg.Height, look, g))
	}
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
	defRec := gfx.NewRecorder()
	fbgrid.Paint(defRec, paintModelGrid(m, def, d.Config().Width, d.Config().Height))
	arcadeRec := gfx.NewRecorder()
	fbgrid.Paint(arcadeRec, paintModelGrid(m, arcade, d.Config().Width, d.Config().Height))
	defTitle, defBody, defCaption, defStatus := paintRoleSizes(defRec)
	arcTitle, arcBody, arcCaption, arcStatus := paintRoleSizes(arcadeRec)
	fmt.Fprintf(&b, "roles theme=default title_px=%d body_px=%d caption_px=%d status_px=%d paint_title=%d paint_body=%d paint_caption=%d paint_status=%d\n",
		def.TitlePx(), def.BodyPx(), def.CaptionPx(), def.StatusPx(), defTitle, defBody, defCaption, defStatus)
	fmt.Fprintf(&b, "roles theme=arcade title_px=%d body_px=%d caption_px=%d status_px=%d paint_title=%d paint_body=%d paint_caption=%d paint_status=%d\n",
		arcade.TitlePx(), arcade.BodyPx(), arcade.CaptionPx(), arcade.StatusPx(), arcTitle, arcBody, arcCaption, arcStatus)
	if defTitle == arcTitle && defStatus == arcStatus {
		return b.String(), fmt.Errorf("default and arcade painted the same type sizes title=%d status=%d", defTitle, defStatus)
	}
	if defTitle != def.TitlePx() || arcTitle != arcade.TitlePx() || defBody != def.BodyPx() || arcBody != arcade.BodyPx() {
		return b.String(), fmt.Errorf("paint sizes missed theme roles default=%d/%d arcade=%d/%d", defTitle, defBody, arcTitle, arcBody)
	}

	scaleTh := theme.Theme{HeaderScale: 2, LabelScale: 1, StatusScale: 2}.Complete()
	pxTh := theme.Theme{HeaderScale: 2, LabelScale: 1, StatusScale: 2, TitleSize: 24, BodySize: 12, CaptionSize: 10, StatusSize: 16}.Complete()
	scaleRec := gfx.NewRecorder()
	fbgrid.Paint(scaleRec, paintModelGrid(m, scaleTh, d.Config().Width, d.Config().Height))
	pxRec := gfx.NewRecorder()
	fbgrid.Paint(pxRec, paintModelGrid(m, pxTh, d.Config().Width, d.Config().Height))
	scaleTitle, _, _, scaleStatus := paintRoleSizes(scaleRec)
	pxTitle, _, _, pxStatus := paintRoleSizes(pxRec)
	fmt.Fprintf(&b, "compat scale-only title_px=%d status_px=%d paint_title=%d paint_status=%d\n",
		scaleTh.TitlePx(), scaleTh.StatusPx(), scaleTitle, scaleStatus)
	fmt.Fprintf(&b, "compat px-override title_px=%d status_px=%d paint_title=%d paint_status=%d\n",
		pxTh.TitlePx(), pxTh.StatusPx(), pxTitle, pxStatus)
	if scaleTitle != gfx.ScalePx(2) || scaleStatus != gfx.ScalePx(2) {
		return b.String(), fmt.Errorf("scale-only fallback title=%d status=%d want %d/%d", scaleTitle, scaleStatus, gfx.ScalePx(2), gfx.ScalePx(2))
	}
	if pxTitle != 24 || pxStatus != 16 || pxTitle == scaleTitle {
		return b.String(), fmt.Errorf("px override title=%d status=%d vs scale title=%d", pxTitle, pxStatus, scaleTitle)
	}

	fmt.Fprintf(&b, "selftest-theme PASS default_hl=%d,%d,%d arcade_hl=%d,%d,%d default_bg=%d,%d,%d arcade_bg=%d,%d,%d default_title_px=%d arcade_title_px=%d scale_title_px=%d px_title_px=%d\n",
		dHLB, dHLG, dHLR, aHLB, aHLG, aHLR, dBGB, dBGG, dBGR, aBGB, aBGG, aBGR, defTitle, arcTitle, scaleTitle, pxTitle)
	return b.String(), nil
}

func paintModelGrid(m kitlauncher.Model, th theme.Theme, w, h int) fbgrid.Grid {
	return modelGrid(m, w, h, nil, nil, th)
}

func paintRoleSizes(rec *gfx.Recorder) (title, body, caption, status int) {
	var texts []gfx.Call
	for _, c := range rec.Calls {
		if c.Op == "DrawText" {
			texts = append(texts, c)
		}
	}
	if len(texts) == 0 {
		return 0, 0, 0, 0
	}
	title = texts[0].SizePx
	status = texts[len(texts)-1].SizePx
	for _, c := range texts[1 : len(texts)-1] {
		if len([]rune(c.Text)) == 1 {
			if caption == 0 {
				caption = c.SizePx
			}
			continue
		}
		if body == 0 {
			body = c.SizePx
		}
	}
	return title, body, caption, status
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
		start, end := catalogPage(m.Focus, len(m.Games), m.Browse)
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
	var headerWeight, footerWeight gfx.Weight
	var sawHeader, sawFooter bool
	for _, c := range rec.Calls {
		if c.Op == "DebugText" {
			sawDebug = true
		}
		if c.Op == "DrawText" {
			sawDraw = true
			if c.Text == g.Header {
				headerWeight = c.Weight
				sawHeader = true
			}
			if c.Text == g.Footer {
				footerWeight = c.Weight
				sawFooter = true
			}
		}
	}
	if sawDebug || !sawDraw {
		return b.String(), fmt.Errorf("paint ops debug=%v drawtext=%v ops=%v", sawDebug, sawDraw, rec.Ops())
	}
	if !sawHeader || headerWeight != gfx.WeightBold {
		return b.String(), fmt.Errorf("header weight %s saw=%v want bold", headerWeight, sawHeader)
	}
	if !sawFooter || footerWeight != gfx.WeightRegular {
		return b.String(), fmt.Errorf("footer weight %s saw=%v want regular", footerWeight, sawFooter)
	}

	regImg := gfx.RasterizeText("FOGCAST", arcade.TitlePx(), arcade.Header, 0)
	boldImg := gfx.RasterizeTextWeight("FOGCAST", arcade.TitlePx(), gfx.WeightBold, arcade.Header, 0)
	if !textImagesDiffer(regImg, boldImg) {
		return b.String(), fmt.Errorf("bold raster matched regular at title_px=%d", arcade.TitlePx())
	}

	regularTheme := arcade
	regularTheme.TitleBold = false
	regularTheme.HeaderBold = false
	regSoft, err := gfx.NewSoftware(w, h)
	if err != nil {
		return b.String(), err
	}
	regGrid := g
	regGrid.Theme = regularTheme
	fbgrid.Paint(regSoft, regGrid)
	if headerBytesEqual(snap, regSoft.Snapshot(), g.HeaderH) {
		return b.String(), fmt.Errorf("header ink matched regular-weight paint")
	}
	fmt.Fprintf(&b, "header-bold=1 header-weight=%s footer-weight=%s\n", headerWeight, footerWeight)

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
	fmt.Fprintf(&b, "selftest-text PASS font=goregular+gobold drawtext=1 debugtext=0 title_bold=1 title_px=%d body_px=%d caption_px=%d status_px=%d\n",
		arcade.TitlePx(), arcade.BodyPx(), arcade.CaptionPx(), arcade.StatusPx())
	return b.String(), nil
}

func exerciseBoldGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	var b strings.Builder
	reg := gfx.RasterizeText("TITLE", th.TitlePx(), th.Header, 0)
	bold := gfx.RasterizeTextWeight("TITLE", th.TitlePx(), gfx.WeightBold, th.Header, 0)
	if !textImagesDiffer(reg, bold) {
		return b.String(), fmt.Errorf("gobold raster matched goregular")
	}
	fmt.Fprintf(&b, "bold-raster=1 title_weight=%s header_weight=%s body_weight=%s\n",
		th.TitleWeight(), th.HeaderWeight(), th.BodyWeight())
	if th.TitleWeight() != gfx.WeightBold || th.HeaderWeight() != gfx.WeightBold {
		return b.String(), fmt.Errorf("built-in title/header should be bold")
	}
	if th.BodyWeight() != gfx.WeightRegular || th.StatusWeight() != gfx.WeightRegular {
		return b.String(), fmt.Errorf("body/status should stay regular")
	}

	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	m.Focus = 1
	press(&m, "b")
	cfg := d.Config()
	frame := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	rec := gfx.NewRecorder()
	fbgrid.PaintDetail(rec, frame)
	var sawTitle bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == frame.Title && c.SizePx == th.TitlePx() {
			if c.Weight != gfx.WeightBold {
				return b.String(), fmt.Errorf("detail title weight %s", c.Weight)
			}
			sawTitle = true
		}
		if c.Op == "DrawText" && c.Text == frame.Hint && c.Weight != gfx.WeightRegular {
			return b.String(), fmt.Errorf("detail hint weight %s", c.Weight)
		}
	}
	if !sawTitle {
		return b.String(), fmt.Errorf("missing detail title DrawText")
	}
	fmt.Fprintf(&b, "detail-title-bold=1 title=%q\n", frame.Title)

	detail, err := exerciseDetailGrid(d, th)
	b.WriteString(detail)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-bold PASS font=goregular+gobold title_bold=1 nested-detail=1\n")
	return b.String(), nil
}

func textImagesDiffer(a, b *image.RGBA) bool {
	if a == nil || b == nil {
		return a != b
	}
	if a.Bounds() != b.Bounds() || len(a.Pix) != len(b.Pix) {
		return true
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return true
		}
	}
	return false
}

func exerciseDetailGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	m.Focus = 1
	keepShelf, keepFocus, keepID := m.Shelf, m.Focus, m.Games[m.Focus].ID
	now := time.Now()
	var b strings.Builder

	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, now); action != "launch" {
		return b.String(), fmt.Errorf("grid A %q", action)
	}
	if m.DetailOpen {
		return b.String(), fmt.Errorf("grid A opened detail")
	}

	press(&m, "b")
	if !m.DetailOpen || m.Shelf != keepShelf || m.Focus != keepFocus || m.Games[m.Focus].ID != keepID {
		return b.String(), fmt.Errorf("B open shelf=%s focus=%d id=%s open=%v", m.Shelf, m.Focus, focusedSelftestID(m), m.DetailOpen)
	}

	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	cfg := d.Config()
	frame := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	frame.Cover = cover
	frame.CoverKind = fbgrid.CoverPresent
	fbgrid.PaintDetail(d, frame)
	d.Present()
	cx, cy, ok := fbgrid.DetailCoverSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("detail cover sample")
	}
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), cfg, cx, cy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "detail cover=(%d,%d) bgrx=%d,%d,%d,%d title=%q meta=%q\n", cx, cy, gotB, gotG, gotR, gotX, frame.Title, frame.Meta)
	if gotB != 160 || gotG != 32 || gotR != 255 || gotX != 0 {
		return b.String(), fmt.Errorf("detail cover bgrx %d,%d,%d,%d want 160,32,255,0", gotB, gotG, gotR, gotX)
	}

	rec := gfx.NewRecorder()
	fbgrid.PaintDetail(rec, frame)
	var sawTitle, sawDebug bool
	for _, c := range rec.Calls {
		if c.Op == "DebugText" {
			sawDebug = true
		}
		if c.Op == "DrawText" && c.Text == frame.Title && c.SizePx == th.TitlePx() {
			if c.Weight != th.TitleWeight() {
				return b.String(), fmt.Errorf("detail title weight %s want %s", c.Weight, th.TitleWeight())
			}
			sawTitle = true
		}
	}
	if sawDebug || !sawTitle {
		return b.String(), fmt.Errorf("detail paint debug=%v title=%v ops=%v", sawDebug, sawTitle, rec.Ops())
	}
	snap := d.Snapshot()
	tx, ty, ok := fbgrid.DetailTitleOrigin(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("title origin")
	}
	titleH := gfx.TextHeight(th.TitlePx()) + 4
	if !regionHasThemedInk(snap, tx, ty, 240, titleH, th.Header) {
		return b.String(), fmt.Errorf("detail missing title ink at %d,%d", tx, ty)
	}
	fmt.Fprintf(&b, "detail title-ink=1 title_px=%d title_weight=%s hint=%q\n", th.TitlePx(), th.TitleWeight(), frame.Hint)

	logo := solidStill(255, 32, 160, 48, 12)
	frame.Logo = logo
	fbgrid.PaintDetail(d, frame)
	d.Present()
	lx, ly, ok := fbgrid.DetailLogoSample(cfg.Width, cfg.Height, th, logo)
	if !ok {
		return b.String(), fmt.Errorf("detail logo sample")
	}
	gotB, gotG, gotR, gotX, err = gfx.SampleBGRX(d.Destination(), cfg, lx, ly)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "detail logo=(%d,%d) bgrx=%d,%d,%d,%d\n", lx, ly, gotB, gotG, gotR, gotX)
	if gotB != 160 || gotG != 32 || gotR != 255 || gotX != 0 {
		return b.String(), fmt.Errorf("detail logo bgrx %d,%d,%d,%d want 160,32,255,0", gotB, gotG, gotR, gotX)
	}
	logoRec := gfx.NewRecorder()
	fbgrid.PaintDetail(logoRec, frame)
	for _, c := range logoRec.Calls {
		if c.Op == "DrawText" && c.Text == frame.Title {
			return b.String(), fmt.Errorf("detail logo still drew title text")
		}
	}

	m.Games[m.Focus].Region = "usa"
	m.Games[m.Focus].Year = "1990"
	m.Games[m.Focus].Genre = "Action"
	m.ApplyPresentation(m.Games[m.Focus].ID, tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{
			Year:    "1991",
			Genre:   "Platform",
			Studio:  "SEGA",
			Players: "1-2",
			Summary: "A blue hedgehog dashes through Green Hill Zone collecting rings.",
		},
	})
	rich := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	if !strings.Contains(rich.Meta, "1991") || !strings.Contains(rich.Meta, "USA") || !strings.Contains(rich.Meta, "1-2") {
		return b.String(), fmt.Errorf("rich meta %q", rich.Meta)
	}
	if !strings.Contains(rich.Description, "hedgehog") {
		return b.String(), fmt.Errorf("rich description %q", rich.Description)
	}
	richRec := gfx.NewRecorder()
	fbgrid.PaintDetail(richRec, rich)
	var sawYear, sawUSA, sawPlayers, sawHedgehog bool
	var descLines int
	for _, c := range richRec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if c.SizePx == th.BodyPx() {
			if strings.Contains(c.Text, "1991") {
				sawYear = true
			}
			if strings.Contains(c.Text, "USA") {
				sawUSA = true
			}
			if strings.Contains(c.Text, "1-2") {
				sawPlayers = true
			}
		}
		if c.SizePx == th.CaptionPx() {
			descLines++
			if strings.Contains(strings.ToLower(c.Text), "hedgehog") {
				sawHedgehog = true
			}
		}
	}
	if !sawYear || !sawUSA || !sawPlayers || !sawHedgehog || descLines < 1 {
		return b.String(), fmt.Errorf("rich paint year=%v usa=%v players=%v hedgehog=%v descLines=%d ops=%v", sawYear, sawUSA, sawPlayers, sawHedgehog, descLines, richRec.Ops())
	}
	fmt.Fprintf(&b, "detail meta=%q desc-lines=%d hedgehog=1\n", rich.Meta, descLines)

	m.ApplyPresentation(m.Games[m.Focus].ID, tenfoot.Presentation{})
	m.Games[m.Focus].Region = ""
	m.Games[m.Focus].Year = ""
	m.Games[m.Focus].Genre = ""
	empty := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	if empty.Description != "" {
		return b.String(), fmt.Errorf("empty description %q", empty.Description)
	}
	emptyRec := gfx.NewRecorder()
	fbgrid.PaintDetail(emptyRec, empty)
	for _, c := range emptyRec.Calls {
		if c.Op == "DrawText" && c.SizePx == th.CaptionPx() && utf8.RuneCountInString(c.Text) > 1 {
			return b.String(), fmt.Errorf("empty description painted %q", c.Text)
		}
	}
	fmt.Fprintf(&b, "detail omit-empty=1\n")

	if action := m.Input(a, now); action != "launch" {
		return b.String(), fmt.Errorf("detail A %q", action)
	}

	press(&m, "b")
	if m.DetailOpen || m.Shelf != keepShelf || m.Focus != keepFocus || m.Games[m.Focus].ID != keepID {
		return b.String(), fmt.Errorf("B close shelf=%s focus=%d id=%s open=%v", m.Shelf, m.Focus, focusedSelftestID(m), m.DetailOpen)
	}

	m.Focus = len(m.Games) - 1
	last := m.Focus
	press(&m, "dpad-down")
	if !m.DetailOpen || m.Focus != last {
		return b.String(), fmt.Errorf("last-row down open=%v focus=%d want %d", m.DetailOpen, m.Focus, last)
	}
	press(&m, "dpad-up")
	if m.DetailOpen || m.Focus != last {
		return b.String(), fmt.Errorf("Up close open=%v focus=%d", m.DetailOpen, m.Focus)
	}

	m.SetAttractIdle(10 * time.Millisecond)
	press(&m, "b")
	t0 := time.Now()
	m.Tick(t0)
	m.Tick(t0.Add(40 * time.Millisecond))
	if !m.DetailOpen || m.AttractActive {
		return b.String(), fmt.Errorf("attract while detail open=%v attract=%v", m.DetailOpen, m.AttractActive)
	}
	press(&m, "b")

	shotAA := strings.Repeat("aa", 32)
	shotBB := strings.Repeat("bb", 32)
	press(&m, "b")
	m.ApplyPresentation(m.Games[m.Focus].ID, tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{
			Studio:        "Nintendo",
			Year:          "1985",
			ScreenshotIDs: []string{shotAA, shotBB},
		},
	})
	if m.FocusDetail().Studio != "Nintendo" {
		return b.String(), fmt.Errorf("presentation studio %q", m.FocusDetail().Studio)
	}
	press(&m, "r")
	if m.ShotIndex() != 1 {
		return b.String(), fmt.Errorf("shot index %d", m.ShotIndex())
	}

	stillShot := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			stillShot.Set(x, y, color.RGBA{R: 32, G: 200, B: 64, A: 255})
		}
	}
	stillFrame := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	stillFrame.Shot = stillShot
	if stillFrame.VideoBadge || strings.Contains(stillFrame.ShotCaption, "preview") {
		return b.String(), fmt.Errorf("still-only badge=%v caption=%q", stillFrame.VideoBadge, stillFrame.ShotCaption)
	}
	stillRec := gfx.NewRecorder()
	fbgrid.PaintDetail(stillRec, stillFrame)
	for _, c := range stillRec.Calls {
		if c.Op == "DrawText" && (c.Text == "VIDEO" || strings.Contains(c.Text, "preview")) {
			return b.String(), fmt.Errorf("still-only painted %q", c.Text)
		}
	}
	fmt.Fprintf(&b, "detail still-only caption=%q video=0\n", stillFrame.ShotCaption)

	press(&m, "b")
	press(&m, "b")
	if !m.DetailOpen {
		return b.String(), fmt.Errorf("reopen detail for video preview")
	}
	videoID := strings.Repeat("ee", 32)
	coverID := strings.Repeat("cc", 32)
	m.ApplyPresentation(m.Games[m.Focus].ID, tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{
			VideoID:           videoID,
			ScreenshotIDs:     []string{shotAA, shotBB},
			BackdropArtworkID: coverID,
		},
	})
	if !m.HasVideoPreview() || m.FocusVideoHandle() != videoID {
		return b.String(), fmt.Errorf("video handle %q", m.FocusVideoHandle())
	}
	tPreview := time.Now()
	m.Tick(tPreview)
	m.Tick(tPreview.Add(2*time.Second + time.Millisecond))
	if m.ShotIndex() != 1 {
		return b.String(), fmt.Errorf("preview cycle index %d", m.ShotIndex())
	}
	videoFrame := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	videoFrame.Shot = stillShot
	if !videoFrame.VideoBadge || !strings.Contains(videoFrame.ShotCaption, "preview") {
		return b.String(), fmt.Errorf("video frame badge=%v caption=%q", videoFrame.VideoBadge, videoFrame.ShotCaption)
	}
	videoRec := gfx.NewRecorder()
	fbgrid.PaintDetail(videoRec, videoFrame)
	var sawVideo, sawPreview bool
	for _, c := range videoRec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if c.Text == "VIDEO" {
			sawVideo = true
		}
		if strings.Contains(c.Text, "preview") {
			sawPreview = true
		}
	}
	if !sawVideo || !sawPreview {
		return b.String(), fmt.Errorf("video paint video=%v preview=%v ops=%v", sawVideo, sawPreview, videoRec.Ops())
	}
	fbgrid.PaintDetail(d, videoFrame)
	d.Present()
	bx, by, ok := fbgrid.DetailVideoBadgeSample(cfg.Width, cfg.Height, th, videoFrame)
	if !ok {
		return b.String(), fmt.Errorf("video badge sample")
	}
	gotB, gotG, gotR, gotX, err = gfx.SampleBGRX(d.Destination(), cfg, bx, by)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "detail video-preview caption=%q cycle=1 badge=(%d,%d) bgrx=%d,%d,%d,%d\n", videoFrame.ShotCaption, bx, by, gotB, gotG, gotR, gotX)
	if gotB != 0 || gotG != 220 || gotR != 255 || gotX != 0 {
		return b.String(), fmt.Errorf("video badge bgrx %d,%d,%d,%d want 0,220,255,0", gotB, gotG, gotR, gotX)
	}

	m.ApplyPresentation(m.Games[m.Focus].ID, tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{VideoID: videoID, CoverArtworkID: coverID},
	})
	if m.ShotHandle() != coverID {
		return b.String(), fmt.Errorf("poster handle %q", m.ShotHandle())
	}
	poster := modelDetailFrame(m, nil, nil, th, cfg.Width, cfg.Height)
	if !poster.VideoBadge || poster.ShotCaption != "preview" {
		return b.String(), fmt.Errorf("poster badge=%v caption=%q", poster.VideoBadge, poster.ShotCaption)
	}
	fmt.Fprintf(&b, "detail video-poster=1 caption=%q\n", poster.ShotCaption)

	press(&m, "b")

	attract, err := exerciseAttractGrid(d, th)
	b.WriteString(attract)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-detail PASS open=1 close=1 title-ink=1 logo=1 launch=1 attract-hold=1 nested-attract=1 meta=1 description=1 omit-empty=1 video-preview=1 still-only=1 poster=1\n")
	return b.String(), nil
}

func focusedSelftestID(m kitlauncher.Model) string {
	if m.Focus < 0 || m.Focus >= len(m.Games) {
		return ""
	}
	return m.Games[m.Focus].ID
}

func regionHasThemedInk(img *image.RGBA, x, y, w, h int, c gfx.Color) bool {
	if img == nil || w < 1 || h < 1 {
		return false
	}
	b := img.Bounds()
	for yy := y; yy < y+h && yy < b.Max.Y; yy++ {
		if yy < b.Min.Y {
			continue
		}
		for xx := x; xx < x+w && xx < b.Max.X; xx++ {
			if xx < b.Min.X {
				continue
			}
			p := img.RGBAAt(xx, yy)
			if absByte(int(p.R)-int(c.R)) <= 40 && absByte(int(p.G)-int(c.G)) <= 40 && absByte(int(p.B)-int(c.B)) <= 40 && p.A > 128 {
				return true
			}
		}
	}
	return false
}

func solidStill(r, g, b uint8, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func exerciseAttractGrid(d *gfx.LinuxFB, th theme.Theme) (string, error) {
	th = th.Complete()
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog(mixedShelfGames())
	m.Focus = 1
	keepShelf, keepFocus, keepID := m.Shelf, m.Focus, m.Games[m.Focus].ID
	aa := strings.Repeat("aa", 32)
	bb := strings.Repeat("bb", 32)
	mario := solidStill(255, 32, 160, 40, 8)
	sonic := solidStill(40, 80, 200, 40, 8)
	var buf bytes.Buffer
	if err := png.Encode(&buf, mario); err != nil {
		return "", err
	}
	decoded, err := tenfoot.DecodeStill(buf.Bytes())
	if err != nil {
		return "", err
	}
	stills := map[string]*image.RGBA{aa: decoded, bb: sonic}
	m.SetAttractPlaylist(tenfoot.AttractPlaylist{
		Items: []tenfoot.AttractItem{
			{GameID: "mario", Title: "Mario", Platform: "snes", Backdrop: aa, Launchable: true},
			{GameID: "sonic", Title: "Sonic", Platform: "megadrive", Backdrop: bb, Launchable: true},
		},
	})
	m.SetAttractIdle(20 * time.Millisecond)
	m.SetAttractCycle(80 * time.Millisecond)
	t0 := time.Now()
	m.Tick(t0)
	var b strings.Builder
	g := paintModel(d, m, th)
	hx, hy, ok := g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("grid highlight before attract")
	}
	hlB, hlG, hlR, _, err := gfx.SampleBGRX(d.Destination(), d.Config(), hx, hy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "pre-attract shelf=%s focus=%d highlight=(%d,%d) bgrx=%d,%d,%d\n", m.Shelf, m.Focus, hx, hy, hlB, hlG, hlR)

	m.Tick(t0.Add(40 * time.Millisecond))
	if !m.AttractActive {
		return b.String(), fmt.Errorf("idle did not arm attract")
	}
	view := m.AttractView(t0.Add(40 * time.Millisecond))
	if view.Title != "Mario" || view.Empty {
		return b.String(), fmt.Errorf("attract view %+v", view)
	}
	paintAttractModel(d, view, stills, th)
	cfg := d.Config()
	dest := fbgrid.AttractStillDest(cfg.Width, cfg.Height, decoded, th)
	sx := int(dest.X + dest.W/2)
	sy := int(dest.Y + dest.H/2)
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), cfg, sx, sy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "attract still=(%d,%d) bgrx=%d,%d,%d,%d title=%q\n", sx, sy, gotB, gotG, gotR, gotX, view.Title)
	if gotB != 160 || gotG != 32 || gotR != 255 || gotX != 0 {
		return b.String(), fmt.Errorf("still bgrx %d,%d,%d,%d want 160,32,255,0", gotB, gotG, gotR, gotX)
	}
	lx, ly, ok := fbgrid.AttractLetterboxSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("letterbox sample")
	}
	lbB, lbG, lbR, _, err := gfx.SampleBGRX(d.Destination(), cfg, lx, ly)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "attract bg=(%d,%d) bgrx=%d,%d,%d want=%d,%d,%d\n", lx, ly, lbB, lbG, lbR, th.AttractBackground.B, th.AttractBackground.G, th.AttractBackground.R)
	if lbB != th.AttractBackground.B || lbG != th.AttractBackground.G || lbR != th.AttractBackground.R {
		return b.String(), fmt.Errorf("attract background bgrx %d,%d,%d", lbB, lbG, lbR)
	}

	empty := kitlauncher.Model{Connected: true, TargetReady: true, AttractActive: true}
	empty.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: nil})
	empty.AttractActive = true
	emptyView := empty.AttractView(t0)
	paintAttractModel(d, emptyView, nil, th)
	cx, cy, ok := fbgrid.AttractStageSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("empty stage sample")
	}
	eB, eG, eR, _, err := gfx.SampleBGRX(d.Destination(), cfg, cx, cy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "empty-panel stage=(%d,%d) bgrx=%d,%d,%d empty=%v\n", cx, cy, eB, eG, eR, emptyView.Empty)
	if !emptyView.Empty {
		return b.String(), fmt.Errorf("empty playlist was not empty panel")
	}
	if eB == 160 && eG == 32 && eR == 255 {
		return b.String(), fmt.Errorf("empty panel kept still pixels")
	}

	cc := strings.Repeat("cc", 32)
	ee := strings.Repeat("ee", 32)
	dd := strings.Repeat("dd", 32)
	ff := strings.Repeat("ff", 32)
	coverStill := solidStill(32, 200, 64, 40, 8)
	sonicStill := stills[bb]
	pongStill := solidStill(200, 40, 80, 40, 8)
	stills[cc] = coverStill
	stills[dd] = pongStill
	stills[ff] = sonicStill

	motion := kitlauncher.Model{Connected: true, TargetReady: true}
	motion.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{
		{GameID: "mario", Title: "Mario", Platform: "snes", Video: ee, Backdrop: aa, Cover: cc, Launchable: true},
	}})
	motion.SetAttractIdle(20 * time.Millisecond)
	motion.SetAttractCycle(10 * time.Second)
	tMotion := time.Now()
	motion.Tick(tMotion)
	motion.Tick(tMotion.Add(40 * time.Millisecond))
	motionView := motion.AttractView(tMotion.Add(40 * time.Millisecond))
	if !motionView.Motion || motionView.Handle != aa || !strings.Contains(motionView.Caption, "preview") {
		return b.String(), fmt.Errorf("motion view %+v", motionView)
	}
	paintAttractModel(d, motionView, stills, th)
	motionRec := gfx.NewRecorder()
	fbgrid.PaintAttract(motionRec, attractFrameFromStills(motionView, stills, th, cfg.Width, cfg.Height))
	var sawVideo, sawPreview bool
	for _, c := range motionRec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if c.Text == "VIDEO" {
			sawVideo = true
		}
		if strings.Contains(c.Text, "preview") {
			sawPreview = true
		}
	}
	if !sawVideo || !sawPreview {
		return b.String(), fmt.Errorf("motion paint video=%v preview=%v ops=%v", sawVideo, sawPreview, motionRec.Ops())
	}
	bx, by, ok := fbgrid.AttractVideoBadgeSample(cfg.Width, cfg.Height, decoded, th)
	if !ok {
		return b.String(), fmt.Errorf("motion badge sample")
	}
	gotB, gotG, gotR, gotX, err = gfx.SampleBGRX(d.Destination(), cfg, bx, by)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "attract motion-preview caption=%q badge=(%d,%d) bgrx=%d,%d,%d,%d\n", motionView.Caption, bx, by, gotB, gotG, gotR, gotX)
	if gotB != 0 || gotG != 220 || gotR != 255 || gotX != 0 {
		return b.String(), fmt.Errorf("motion badge bgrx %d,%d,%d,%d want 0,220,255,0", gotB, gotG, gotR, gotX)
	}
	motion.Tick(tMotion.Add(40*time.Millisecond + 2*time.Second + time.Millisecond))
	cycled := motion.AttractView(tMotion.Add(40*time.Millisecond + 2*time.Second + time.Millisecond))
	if cycled.Handle != cc || cycled.ShotIndex != 1 {
		return b.String(), fmt.Errorf("motion cycle %+v", cycled)
	}
	fmt.Fprintf(&b, "attract motion-cycle=1 handle=%q\n", cycled.Handle)

	stillOnly := kitlauncher.Model{Connected: true, TargetReady: true, AttractActive: true}
	stillOnly.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{
		{GameID: "mario", Title: "Mario", Platform: "snes", Backdrop: aa, Launchable: true},
	}})
	stillOnly.AttractActive = true
	stillView := stillOnly.AttractView(t0)
	if stillView.Motion || stillView.Caption != "" {
		return b.String(), fmt.Errorf("stills-only chrome %+v", stillView)
	}
	stillRec := gfx.NewRecorder()
	fbgrid.PaintAttract(stillRec, attractFrameFromStills(stillView, stills, th, cfg.Width, cfg.Height))
	for _, c := range stillRec.Calls {
		if c.Op == "DrawText" && (c.Text == "VIDEO" || strings.Contains(c.Text, "preview")) {
			return b.String(), fmt.Errorf("stills-only painted %q", c.Text)
		}
	}
	fmt.Fprintf(&b, "attract stills-fallback video=0\n")

	wall := kitlauncher.Model{Connected: true, TargetReady: true}
	wall.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{
		{GameID: "mario", Title: "Mario", Platform: "snes", Video: ee, Backdrop: aa, Cover: cc, Launchable: true},
		{GameID: "sonic", Title: "Sonic", Platform: "megadrive", Backdrop: bb, Launchable: true},
		{GameID: "pong", Title: "Pong", Platform: "pong", Backdrop: dd, Launchable: true},
		{GameID: "zelda", Title: "Zelda", Platform: "snes", Backdrop: ff, Launchable: true},
	}})
	wall.SetAttractIdle(20 * time.Millisecond)
	wall.SetAttractCycle(10 * time.Second)
	tWall := time.Now()
	wall.Tick(tWall)
	wall.Tick(tWall.Add(40 * time.Millisecond))
	wallView := wall.AttractView(tWall.Add(40 * time.Millisecond))
	if !wallView.Motion || len(wallView.Wall) != 4 {
		return b.String(), fmt.Errorf("wall view %+v", wallView)
	}
	paintAttractModel(d, wallView, stills, th)
	wx, wy, ok := fbgrid.AttractWallBadgeSample(cfg.Width, cfg.Height, th)
	if !ok {
		return b.String(), fmt.Errorf("wall badge sample")
	}
	wB, wG, wR, wX, err := gfx.SampleBGRX(d.Destination(), cfg, wx, wy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "attract wall=1 badge=(%d,%d) bgrx=%d,%d,%d,%d tiles=%d\n", wx, wy, wB, wG, wR, wX, len(wallView.Wall))
	if wB != 0 || wG != 220 || wR != 255 || wX != 0 {
		return b.String(), fmt.Errorf("wall badge bgrx %d,%d,%d,%d", wB, wG, wR, wX)
	}

	press(&m, "dpad-right")
	if m.AttractActive {
		return b.String(), fmt.Errorf("input did not dismiss attract")
	}
	if m.Shelf != keepShelf || m.Focus != keepFocus || m.Games[m.Focus].ID != keepID {
		return b.String(), fmt.Errorf("dismiss moved focus shelf=%s focus=%d id=%s", m.Shelf, m.Focus, m.Games[m.Focus].ID)
	}
	g = paintModel(d, m, th)
	hx, hy, ok = g.HighlightSample()
	if !ok {
		return b.String(), fmt.Errorf("grid highlight after dismiss")
	}
	hlB, hlG, hlR, _, err = gfx.SampleBGRX(d.Destination(), d.Config(), hx, hy)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "post-dismiss shelf=%s focus=%d highlight=(%d,%d) bgrx=%d,%d,%d\n", m.Shelf, m.Focus, hx, hy, hlB, hlG, hlR)
	if hlB != th.Highlight.B || hlG != th.Highlight.G || hlR != th.Highlight.R {
		return b.String(), fmt.Errorf("post-dismiss highlight")
	}

	cover, err := exerciseCoverGrid(d, th)
	b.WriteString(cover)
	if err != nil {
		return b.String(), err
	}
	fmt.Fprintf(&b, "selftest-attract PASS idle=1 dismiss=1 still=1 empty-panel=1 motion=1 stills-fallback=1 wall=1 nested-cover=1\n")
	return b.String(), nil
}

func attractFrameFromStills(view kitlauncher.AttractView, stills map[string]*image.RGBA, th theme.Theme, width, height int) fbgrid.AttractFrame {
	frame := attractFrame(view, tenfoot.NewStillCache(), th, width, height)
	if stills != nil {
		frame.Image = stills[view.Handle]
		frame.Next = stills[view.NextHandle]
		if view.Marquee != "" {
			frame.Marquee = stills[view.Marquee]
		}
		if len(view.Wall) >= 4 {
			wall := make([]fbgrid.AttractWallTile, 4)
			for i := 0; i < 4; i++ {
				wall[i].Image = stills[view.Wall[i].Handle]
				wall[i].Video = view.Wall[i].Motion
			}
			frame.Wall = wall
		}
	}
	frame.Empty = view.Empty
	frame.VideoBadge = view.Motion
	frame.Caption = view.Caption
	return frame
}

func paintAttractModel(d *gfx.LinuxFB, view kitlauncher.AttractView, stills map[string]*image.RGBA, th theme.Theme) {
	cfg := d.Config()
	fbgrid.PaintAttract(d, attractFrameFromStills(view, stills, th, cfg.Width, cfg.Height))
	d.Present()
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
