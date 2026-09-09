// fogcast-kit runs the controller/session adapter on the native target.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
	"github.com/DeanoC/FogCast/kitlauncher"
	"github.com/DeanoC/FogCast/kitlauncher/controller"
	"os"
	"os/signal"
	"syscall"
)

const (
	gridPageSize   = fbgrid.DefaultPageSize
	chromeLabelMax = 40
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fogcast-kit:", err)
		os.Exit(1)
	}
}
func run() error {
	configPath := flag.String("config", "/media/fat/fogcast/launcher.json", "provisioned launcher configuration")
	inputProfile := flag.String("input-profile", "", "identity, swap-ab, or JSON profile path (default identity)")
	themeSpec := flag.String("theme", "", "classic/default, neon/arcade, sofa-dim/night, or JSON/TOML path (default classic)")
	selftestNav := flag.Bool("selftest-nav", false, "paint 4x3 catalog navigation on the framebuffer and exit")
	selftestPads := flag.Bool("selftest-pads", false, "open eligible USB pads, print them, and exit")
	selftestTheme := flag.Bool("selftest-theme", false, "paint default and arcade, prove type-role sizes, and sample pixels, then exit")
	selftestFPGA := flag.Bool("selftest-fpga", false, "record FC2D attract still/anim on the FPGA software-replay backend and exit")
	selftestShelf := flag.Bool("selftest-shelf", false, "paint system shelves, cycle L/R, sample header, and exit")
	selftestText := flag.Bool("selftest-text", false, "paint UI-face chrome and prove it is not DebugText, then exit")
	selftestBold := flag.Bool("selftest-bold", false, "prove title chrome uses gobold, nest detail/text/nav, then exit")
	selftestCover := flag.Bool("selftest-cover", false, "paint cover decode, placeholder, and chrome polish, then exit")
	selftestAttract := flag.Bool("selftest-attract", false, "arm short idle attract, paint stills and kit-safe motion, dismiss, then exit")
	selftestDetail := flag.Bool("selftest-detail", false, "open/close title detail, paint cover and title ink, then exit")
	selftestSeries := flag.Bool("selftest-series", false, "paint series mates on detail and split, jump, hide empty, then exit")
	selftestMotion := flag.Bool("selftest-motion", false, "prove focus pop and confirm pulse over ticks, then exit")
	selftestWheel := flag.Bool("selftest-wheel", false, "paint platform wheel and hero, enter a system grid, then exit")
	selftestStrip := flag.Bool("selftest-strip", false, "paint recent/favorites strip, hand off from grid, then exit")
	selftestAtmosphere := flag.Bool("selftest-atmosphere", false, "paint dimmed fanart/cover-wall behind chrome, then exit")
	selftestLayouts := flag.Bool("selftest-layouts", false, "paint coverflow, cover-wall, and split browse, cycle Y, then exit")
	selftestPacks := flag.Bool("selftest-packs", false, "paint Classic/Neon/Sofa Dim packs, cycle X, then exit")
	selftestBadges := flag.Bool("selftest-badges", false, "paint tile/detail badges and wheel play-stats, then exit")
	selftestTransition := flag.Bool("selftest-transition", false, "paint curtain, wipe, and glitch scene overlays, then exit")
	selftestMarquee := flag.Bool("selftest-marquee", false, "paint attract and detail marquee/banner strips, then exit")
	selftestSearch := flag.Bool("selftest-search", false, "paint catalog search OSK, filter, empty, restore, then exit")
	selftestBezel := flag.Bool("selftest-bezel", false, "paint soft vignette, optional bezel, and pause chrome, then exit")
	noTransition := flag.Bool("no-transition", false, "disable kit scene transition overlays")
	flag.Parse()
	if !*noTransition {
		switch strings.ToLower(strings.TrimSpace(os.Getenv("FOGCAST_NO_TRANSITION"))) {
		case "1", "true", "yes":
			*noTransition = true
		}
	}
	if *selftestFPGA {
		fb := "/dev/fb0"
		if c, err := kitlauncher.LoadConfig(*configPath); err == nil && c.Framebuffer != "" {
			fb = c.Framebuffer
		}
		return runFPGASelftest(fb)
	}
	if *selftestPads {
		remap, err := loadKitRemapper(*inputProfile, "")
		if err != nil {
			return err
		}
		return runPadsSelftest(remap)
	}
	if *selftestTheme {
		fb := "/dev/fb0"
		if c, err := kitlauncher.LoadConfig(*configPath); err == nil && c.Framebuffer != "" {
			fb = c.Framebuffer
		}
		return runThemeSelftest(fb)
	}
	if *selftestNav || *selftestShelf || *selftestText || *selftestBold || *selftestCover || *selftestAttract || *selftestDetail || *selftestSeries || *selftestMotion || *selftestWheel || *selftestStrip || *selftestAtmosphere || *selftestLayouts || *selftestPacks || *selftestBadges || *selftestTransition || *selftestMarquee || *selftestSearch || *selftestBezel {
		fb := "/dev/fb0"
		configTheme := ""
		if c, err := kitlauncher.LoadConfig(*configPath); err == nil {
			if c.Framebuffer != "" {
				fb = c.Framebuffer
			}
			configTheme = c.Theme
		}
		th, err := loadKitTheme(*themeSpec, configTheme)
		if err != nil {
			return err
		}
		if *selftestBezel {
			return runBezelSelftest(fb, th)
		}
		if *selftestMarquee {
			return runMarqueeSelftest(fb, th)
		}
		if *selftestMotion {
			return runMotionSelftest(fb, th)
		}
		if *selftestSeries {
			return runSeriesSelftest(fb, th)
		}
		if *selftestSearch {
			return runSearchSelftest(fb, th)
		}
		if *selftestBadges {
			return runBadgesSelftest(fb, th)
		}
		if *selftestTransition {
			return runTransitionSelftest(fb, th)
		}
		if *selftestPacks {
			return runPacksSelftest(fb, th)
		}
		if *selftestLayouts {
			return runLayoutsSelftest(fb, th)
		}
		if *selftestAtmosphere {
			return runAtmosphereSelftest(fb, th)
		}
		if *selftestStrip {
			return runStripSelftest(fb, th)
		}
		if *selftestWheel {
			return runWheelSelftest(fb, th)
		}
		if *selftestBold {
			return runBoldSelftest(fb, th)
		}
		if *selftestDetail {
			return runDetailSelftest(fb, th)
		}
		if *selftestAttract {
			return runAttractSelftest(fb, th)
		}
		if *selftestCover {
			return runCoverSelftest(fb, th)
		}
		if *selftestText {
			return runTextSelftest(fb, th)
		}
		if *selftestShelf {
			return runShelfSelftest(fb, th)
		}
		return runNavSelftest(fb, th)
	}
	c, err := kitlauncher.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	th, err := loadKitTheme(*themeSpec, c.Theme)
	if err != nil {
		return err
	}
	if id := theme.NormalizePack(*themeSpec); id != "" {
		c.Theme = id
	} else if id := theme.NormalizePack(th.Name); id != "" && strings.TrimSpace(*themeSpec) == "" {
		c.Theme = id
	}
	d, err := gfx.OpenLinuxFB(c.Framebuffer)
	if err != nil {
		return err
	}
	defer d.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := kitlauncher.NewClient(c)
	covers := tenfoot.NewCoverCache()
	stills := tenfoot.NewStillCache()
	presentations := tenfoot.NewPresentationCache()
	last := time.Time{}
	var lastKey renderKey
	lastFocus := -1
	var popAt time.Time
	var fx sceneFX
	fxOff := *noTransition
	present := func(m kitlauncher.Model) {
		now := time.Now()
		look := kitLook(m, th)
		fx.observe(modelScene(m), kitTransitionStyle(look, fxOff), now)
		if m.AttractActive && !m.Busy {
			lastFocus = -1
			handles := m.AttractPrefetchHandles()
			stills.Keep(handles)
			stills.Request(ctx, client.Library, handles)
			view := m.AttractView(now)
			key := modelRenderKey(m, covers.Generation(), 0)
			key.Attract = true
			key.AttractIndex = view.Index
			key.AttractHandle = view.Handle
			key.AttractFade = int(view.FadeT * 10)
			key.Shot = view.ShotIndex
			key.Preview = view.Caption
			key.Video = view.Motion
			key.Stills = stills.Generation()
			if !fx.active(now) && now.Sub(last) < 100*time.Millisecond && key == lastKey {
				return
			}
			last = now
			lastKey = key
			cfg := d.Config()
			fbgrid.PaintAttract(d, attractFrame(view, stills, look, cfg.Width, cfg.Height))
			paintSceneFX(d, cfg.Width, cfg.Height, fx, now, look)
			d.Present()
			return
		}
		if m.WheelOpen && !m.Busy {
			ids := m.WheelPrefetchIDs()
			presentations.Keep(ids)
			presentations.Request(ctx, client.Library, ids)
			heroHandles := make([]string, 0, 2)
			logoHandles := make([]string, 0, len(ids))
			for _, id := range ids {
				pres := presentations.Get(id)
				if handle := tenfoot.LogoHandle(pres); handle != "" {
					logoHandles = append(logoHandles, handle)
				}
			}
			focusedPres := tenfoot.Presentation{}
			if game, ok := m.WheelGame(m.Shelf); ok {
				focusedPres = presentations.Get(game.ID)
			}
			if handle := m.WheelHeroHandle(focusedPres); handle != "" {
				heroHandles = append(heroHandles, handle)
			}
			if handle := m.WheelLogoHandle(focusedPres); handle != "" {
				logoHandles = append(logoHandles, handle)
			}
			stills.Keep(heroHandles)
			stills.Request(ctx, client.Library, heroHandles)
			covers.Keep(logoHandles)
			covers.Request(ctx, client.Library, logoHandles)
			cfg := d.Config()
			w, h := cfg.Width, cfg.Height
			frame := modelWheelFrame(m, covers, stills, presentations, look, w, h)
			wheelFocus := m.WheelIndex()
			if lastFocus >= 0 && wheelFocus != lastFocus {
				popAt = now
			}
			lastFocus = wheelFocus
			frame.Now = now
			frame.PopAt = popAt
			key := modelRenderKey(m, covers.Generation(), presentations.Generation())
			key.Wheel = true
			key.Stills = stills.Generation()
			if !frame.MotionActive() && !fx.active(now) && now.Sub(last) < 100*time.Millisecond && key == lastKey {
				return
			}
			last = now
			lastKey = key
			fbgrid.PaintWheel(d, frame)
			paintSceneFX(d, w, h, fx, now, look)
			d.Present()
			return
		}
		start, end := catalogPage(m.Focus, len(m.Games), m.Browse)
		prefetch := end + fbgrid.BrowsePageSize(m.Browse)
		if m.Browse == fbgrid.BrowseCoverflow || m.Browse == fbgrid.BrowseSplit {
			prefetch = end + 2
		}
		if prefetch > len(m.Games) {
			prefetch = len(m.Games)
		}
		ids := tenfoot.PageIDs(m.Games, start, prefetch)
		ids = append(ids, tenfoot.PageIDs(m.Strip, 0, len(m.Strip))...)
		ids = append(ids, tenfoot.PageIDs(m.Series, 0, len(m.Series))...)
		presentations.Keep(ids)
		presentations.Request(ctx, client.Library, ids)
		handles := tenfoot.CollectCoverHandles(m.Games, start, prefetch, presentations.Get)
		handles = append(handles, tenfoot.CollectLogoHandles(m.Games, start, prefetch, presentations.Get)...)
		handles = append(handles, tenfoot.CollectCoverHandles(m.Strip, 0, len(m.Strip), presentations.Get)...)
		handles = append(handles, tenfoot.CollectLogoHandles(m.Strip, 0, len(m.Strip), presentations.Get)...)
		handles = append(handles, tenfoot.CollectCoverHandles(m.Series, 0, len(m.Series), presentations.Get)...)
		handles = append(handles, tenfoot.CollectLogoHandles(m.Series, 0, len(m.Series), presentations.Get)...)
		backdropHandles := atmospherePrefetchHandles(m, presentations, start, prefetch)
		if m.DetailOpen {
			handles = append(handles, m.DetailPrefetchHandles()...)
			if game, ok := m.FocusedGame(); ok {
				pres := presentations.Get(game.ID)
				if handle := tenfoot.CoverHandle(game, pres); handle != "" {
					handles = append(handles, handle)
				}
				if handle := tenfoot.LogoHandle(pres); handle != "" {
					handles = append(handles, handle)
				}
				if handle := tenfoot.MarqueeHandle(pres); handle != "" {
					backdropHandles = append(backdropHandles, handle)
				}
			}
			if handle := m.FocusMarqueeHandle(); handle != "" {
				backdropHandles = append(backdropHandles, handle)
			}
		}
		covers.Keep(handles)
		covers.Request(ctx, client.Library, handles)
		stills.Keep(backdropHandles)
		stills.Request(ctx, client.Library, backdropHandles)
		cfg := d.Config()
		w, h := cfg.Width, cfg.Height
		grid := modelGrid(m, w, h, covers, presentations, look)
		grid.Atmosphere = stillImage(stills, atmosphereHandle(m, presentations))
		if lastFocus >= 0 && grid.Focus != lastFocus {
			popAt = now
		}
		lastFocus = grid.Focus
		fbgrid.ArmPop(&grid, popAt, now)
		key := modelRenderKey(m, covers.Generation(), presentations.Generation())
		key.Stills = stills.Generation()
		if !grid.MotionActive() && !fx.active(now) && now.Sub(last) < 100*time.Millisecond && key == lastKey {
			return
		}
		last = now
		lastKey = key
		if m.DetailOpen {
			frame := modelDetailFrame(m, covers, presentations, look, w, h)
			frame.Atmosphere = stillImage(stills, atmosphereHandle(m, presentations))
			mq := m.FocusMarqueeHandle()
			if mq == "" {
				if game, ok := m.FocusedGame(); ok {
					mq = tenfoot.MarqueeHandle(presentations.Get(game.ID))
				}
			}
			frame.Marquee = stillImage(stills, mq)
			fbgrid.PaintDetail(d, frame)
			paintSceneFX(d, w, h, fx, now, look)
			d.Present()
			return
		}
		fbgrid.Paint(d, grid)
		if m.SearchOpen {
			fbgrid.PaintOSK(d, modelOSKFrame(m, w, h, look, grid))
		}
		paintSceneFX(d, w, h, fx, now, look)
		d.Present()
	}
	remap, err := loadKitRemapper(*inputProfile, c.InputProfile)
	if err != nil {
		return err
	}
	return kitlauncher.Run(ctx, client, present, func() (kitlauncher.Pad, error) { return controller.OpenWith(remap) })
}

func loadKitTheme(flagSpec, configSpec string) (theme.Theme, error) {
	spec := strings.TrimSpace(flagSpec)
	if spec == "" {
		spec = strings.TrimSpace(configSpec)
	}
	if spec == "" {
		spec = strings.TrimSpace(os.Getenv("FOGCAST_THEME"))
	}
	return theme.Resolve(spec)
}

func kitLook(m kitlauncher.Model, fallback theme.Theme) theme.Theme {
	if strings.TrimSpace(m.Pack) == "" {
		return fallback.Complete()
	}
	if look, ok := theme.PackTheme(m.Pack); ok {
		return look
	}
	return fallback.Complete()
}

func packHeader(m kitlauncher.Model) string {
	header := m.HeaderChrome()
	if tag := m.SearchTag(); tag != "" && !m.WheelOpen && !m.DetailOpen {
		header = header + "  " + tag
	}
	if tag := m.Browse.HeaderTag(); tag != "" && !m.WheelOpen && !m.DetailOpen {
		header = header + "  " + tag
	}
	if tag := m.PackTag(); tag != "" {
		header = header + "  " + tag
	}
	return header
}

func loadKitRemapper(flagSpec, configSpec string) (*inputmap.Remapper, error) {
	spec := strings.TrimSpace(flagSpec)
	if spec == "" {
		spec = strings.TrimSpace(configSpec)
	}
	if spec == "" {
		spec = strings.TrimSpace(os.Getenv("FOGCAST_INPUT_PROFILE"))
	}
	profile, err := inputmap.Resolve(spec)
	if err != nil {
		return nil, err
	}
	return inputmap.NewRemapper(profile)
}

type renderKey struct {
	Focus, GameCount, AttractIndex, AttractFade, Shot, StripFocus, SeriesFocus int
	FocusID, Message, Shelf, AttractHandle, Preview, StripID, SeriesID         string
	SessionState, Execution, GameID, Query, OSKFocus                           string
	Busy, Connected, TargetReady, ControllerConnected                          bool
	Attract, Detail, Wheel, Video, Strip, Series, Search                       bool
	Covers, Stills, Presentations                                              uint64
	Browse                                                                     fbgrid.BrowseKind
	Pack                                                                       string
}

func modelRenderKey(m kitlauncher.Model, covers, presentations uint64) renderKey {
	focusID := ""
	if game, ok := m.FocusedGame(); ok {
		focusID = game.ID
	}
	stripID := ""
	if m.StripFocus >= 0 && m.StripFocus < len(m.Strip) {
		stripID = m.Strip[m.StripFocus].ID
	}
	seriesID := ""
	if m.SeriesFocus >= 0 && m.SeriesFocus < len(m.Series) {
		seriesID = m.Series[m.SeriesFocus].ID
	}
	return renderKey{
		Focus: m.Focus, GameCount: len(m.Games), FocusID: focusID,
		Message: m.Message, Shelf: m.Shelf, SessionState: m.Session.State, Execution: m.Session.Execution,
		GameID: m.Session.GameID, Busy: m.Busy, Connected: m.Connected,
		TargetReady: m.TargetReady, ControllerConnected: m.ControllerConnected,
		Detail: m.DetailOpen, Wheel: m.WheelOpen, Shot: m.ShotIndex(),
		Preview: m.ShotHandle(), Video: m.HasVideoPreview(),
		Strip: m.StripActive, StripFocus: m.StripFocus, StripID: stripID,
		Series: m.SeriesActive, SeriesFocus: m.SeriesFocus, SeriesID: seriesID,
		Covers: covers, Presentations: presentations, Browse: m.Browse,
		Pack: m.Pack, Search: m.SearchOpen, Query: m.SearchQuery,
		OSKFocus: m.SearchSnapshot().FocusID,
	}
}

func atmosphereHandle(m kitlauncher.Model, presentations *tenfoot.PresentationCache) string {
	pres := tenfoot.Presentation{}
	if presentations != nil {
		if game, ok := m.FocusedGame(); ok {
			pres = presentations.Get(game.ID)
		} else if game, ok := m.WheelGame(m.Shelf); ok {
			pres = presentations.Get(game.ID)
		}
	}
	return m.AtmosphereHandle(pres)
}

func atmospherePrefetchHandles(m kitlauncher.Model, presentations *tenfoot.PresentationCache, start, prefetch int) []string {
	lookup := func(string) tenfoot.Presentation { return tenfoot.Presentation{} }
	if presentations != nil {
		lookup = presentations.Get
	}
	handles := tenfoot.CollectBackdropHandles(m.Games, start, prefetch, lookup)
	handles = append(handles, tenfoot.CollectBackdropHandles(m.Strip, 0, len(m.Strip), lookup)...)
	if handle := atmosphereHandle(m, presentations); handle != "" {
		out := make([]string, 0, len(handles)+1)
		out = append(out, handle)
		seen := map[string]struct{}{handle: {}}
		for _, h := range handles {
			if _, ok := seen[h]; ok {
				continue
			}
			seen[h] = struct{}{}
			out = append(out, h)
		}
		return out
	}
	return handles
}

func stillImage(stills *tenfoot.CoverCache, handle string) *image.RGBA {
	if stills == nil || handle == "" {
		return nil
	}
	if stills.Status(handle) != tenfoot.CoverReady {
		return nil
	}
	return stills.Image(handle)
}

func catalogPage(focus, n int, kind fbgrid.BrowseKind) (start, end int) {
	return fbgrid.CatalogPage(focus, n, kind)
}

type sceneSig struct {
	attract bool
	detail  bool
	wheel   bool
	search  bool
	browse  fbgrid.BrowseKind
	pack    string
}

func modelScene(m kitlauncher.Model) sceneSig {
	return sceneSig{
		attract: m.AttractActive,
		detail:  m.DetailOpen,
		wheel:   m.WheelOpen,
		search:  m.SearchOpen,
		browse:  m.Browse,
		pack:    m.Pack,
	}
}

type sceneFX struct {
	sig   sceneSig
	style anim.Style
	at    time.Time
	armed bool
}

func (fx *sceneFX) observe(sig sceneSig, style anim.Style, now time.Time) {
	if fx == nil {
		return
	}
	if !fx.armed {
		fx.sig = sig
		fx.armed = true
		return
	}
	if sig == fx.sig {
		return
	}
	fx.sig = sig
	if style == anim.StyleNone {
		fx.style = anim.StyleNone
		fx.at = time.Time{}
		return
	}
	fx.style = style
	fx.at = now
}

func (fx sceneFX) active(now time.Time) bool {
	if fx.style == anim.StyleNone || fx.at.IsZero() {
		return false
	}
	return now.Sub(fx.at) < fx.style.Duration()
}

func (fx sceneFX) progress(now time.Time) float64 {
	if fx.style == anim.StyleNone || fx.at.IsZero() {
		return 1
	}
	return anim.Progress(fx.style, now.Sub(fx.at))
}

func kitTransitionStyle(th theme.Theme, disabled bool) anim.Style {
	if disabled {
		return anim.StyleNone
	}
	return anim.ParseStyle(th.Complete().Transition)
}

func transitionColors(th theme.Theme) anim.Colors {
	th = th.Complete()
	return anim.Colors{Fill: th.HeaderBar, Edge: th.Highlight, Flash: th.Flash}
}

func paintSceneFX(d gfx.Device, w, h int, fx sceneFX, now time.Time, th theme.Theme) {
	if d == nil || !fx.active(now) || w < 1 || h < 1 {
		return
	}
	anim.PaintTransition(d, gfx.Rect{X: 0, Y: 0, W: float32(w), H: float32(h)}, fx.style, fx.progress(now), transitionColors(th))
}

// modelGrid maps the live catalog to one visible page. Model.Focus remains
// an index into the complete catalog; the grid focus is page-local.
func modelGrid(m kitlauncher.Model, width, height int, covers *tenfoot.CoverCache, presentations *tenfoot.PresentationCache, th theme.Theme) fbgrid.Grid {
	start, end := catalogPage(m.Focus, len(m.Games), m.Browse)
	tiles := make([]fbgrid.Tile, 0, end-start)
	nameMax := 18
	if m.Browse == fbgrid.BrowseSplit {
		nameMax = 28
	}
	for _, game := range m.Games[start:end] {
		pres := tenfoot.Presentation{}
		if presentations != nil {
			pres = presentations.Get(game.ID)
		}
		tiles = append(tiles, gameTile(game, covers, pres, th, nameMax))
	}
	g := fbgrid.NewWithTiles(width, height, tiles)
	g.Kind = m.Browse
	fbgrid.ApplyTheme(&g, th)
	g.Header = truncateLabel(asciiLabel(packHeader(m)), chromeLabelMax)
	if m.Focus >= start && m.Focus < end {
		g.Focus = m.Focus - start
	}
	g.Footer = modelFooter(m)
	if strings.TrimSpace(m.SearchQuery) != "" && len(m.Games) == 0 {
		g.EmptyLabel = "No matches"
	}
	if len(m.Strip) > 0 && m.SearchTag() == "" {
		strip := make([]fbgrid.Tile, 0, len(m.Strip))
		for _, game := range m.Strip {
			pres := tenfoot.Presentation{}
			if presentations != nil {
				pres = presentations.Get(game.ID)
			}
			strip = append(strip, gameTile(game, covers, pres, th, 18))
		}
		g.SetStrip(strip, asciiLabel(m.StripLabel), m.StripFocus, m.StripActive)
	}
	if len(m.Series) > 0 {
		series := make([]fbgrid.Tile, 0, len(m.Series))
		for _, game := range m.Series {
			pres := tenfoot.Presentation{}
			if presentations != nil {
				pres = presentations.Get(game.ID)
			}
			series = append(series, gameTile(game, covers, pres, th, 18))
		}
		g.Series = series
		g.SeriesLabel = asciiLabel(m.SeriesLabel)
		g.SeriesFocus = m.SeriesFocus
		g.SeriesActive = m.SeriesActive
	}
	g.Session = m.SessionChrome()
	return g
}

func modelOSKFrame(m kitlauncher.Model, width, height int, th theme.Theme, g fbgrid.Grid) fbgrid.OSKFrame {
	return fbgrid.OSKFrame{
		Width:   width,
		Height:  height,
		HeaderH: g.HeaderH,
		FooterH: g.FooterH,
		OSK:     m.SearchSnapshot(),
		Theme:   th,
	}
}

func modelDetailFrame(m kitlauncher.Model, covers *tenfoot.CoverCache, presentations *tenfoot.PresentationCache, th theme.Theme, width, height int) fbgrid.DetailFrame {
	detail := m.FocusDetail()
	title := asciiLabel(detail.Title)
	if title == "" {
		title = "UNTITLED"
	}
	frame := fbgrid.DetailFrame{
		Width:        width,
		Height:       height,
		Header:       truncateLabel(asciiLabel(packHeader(m)), chromeLabelMax),
		Title:        title,
		Meta:         kitMetaLine(detail.MetaFacts()),
		Description:  asciiLabel(detail.Summary),
		Hint:         asciiLabel(detailFooter(m)),
		Theme:        th,
		Color:        th.SystemColor(detail.Platform),
		SeriesLabel:  asciiLabel(m.SeriesLabel),
		SeriesFocus:  m.SeriesFocus,
		SeriesActive: m.SeriesActive,
		Session:      m.SessionChrome(),
	}
	if len(m.Series) > 0 {
		frame.Series = make([]fbgrid.Tile, 0, len(m.Series))
		for _, game := range m.Series {
			pres := tenfoot.Presentation{}
			if presentations != nil {
				pres = presentations.Get(game.ID)
			}
			frame.Series = append(frame.Series, gameTile(game, covers, pres, th, 18))
		}
	}
	if game, ok := m.FocusedGame(); ok {
		frame.Color = th.SystemColor(game.System)
		pres := tenfoot.Presentation{}
		if presentations != nil {
			pres = presentations.Get(game.ID)
		}
		if pres.Presentation == nil {
			pres = m.FocusPresentation()
		}
		frame.Badges = kitlauncher.TitleBadges(game, pres)
		handle := m.FocusCoverHandle()
		if handle == "" {
			handle = tenfoot.CoverHandle(game, pres)
		}
		if handle != "" && covers != nil {
			frame.Cover = covers.Image(handle)
			switch covers.Status(handle) {
			case tenfoot.CoverReady:
				frame.CoverKind = fbgrid.CoverPresent
			case tenfoot.CoverLoading:
				frame.CoverKind = fbgrid.CoverLoading
			default:
				frame.CoverKind = fbgrid.CoverMissing
			}
		} else {
			frame.CoverKind = fbgrid.CoverMissing
		}
		logo := m.FocusLogoHandle()
		if logo == "" {
			logo = tenfoot.LogoHandle(pres)
		}
		if logo != "" && covers != nil && covers.Status(logo) == tenfoot.CoverReady {
			frame.Logo = covers.Image(logo)
		}
	}
	ids := m.PreviewHandles()
	if shot := m.ShotHandle(); shot != "" || m.HasVideoPreview() {
		n := len(ids)
		if m.HasVideoPreview() {
			frame.VideoBadge = true
			frame.ShotCaption = asciiLabel(previewCaption(m.ShotIndex(), n))
		} else {
			frame.ShotCaption = asciiLabel(shotCaption(m.ShotIndex(), n))
		}
		if covers != nil && shot != "" {
			frame.Shot = covers.Image(shot)
		}
	}
	return frame
}

func detailFooter(m kitlauncher.Model) string {
	if msg := strings.TrimSpace(m.Message); msg != "" {
		return msg
	}
	return m.DetailHint()
}

func shotCaption(index, count int) string {
	if count < 1 {
		return ""
	}
	if index < 0 {
		index = 0
	}
	if index >= count {
		index = count - 1
	}
	return fmt.Sprintf("%d / %d", index+1, count)
}

func previewCaption(index, count int) string {
	if count < 2 {
		return "preview"
	}
	return "preview " + shotCaption(index, count)
}

func attractFrame(view kitlauncher.AttractView, stills *tenfoot.CoverCache, th theme.Theme, width, height int) fbgrid.AttractFrame {
	frame := fbgrid.AttractFrame{
		Width:      width,
		Height:     height,
		Title:      asciiLabel(view.Title),
		Empty:      view.Empty,
		FadeT:      view.FadeT,
		VideoBadge: view.Motion,
		Caption:    asciiLabel(view.Caption),
		Theme:      th,
	}
	if view.Empty {
		frame.Hint = "any back"
		if frame.Title == "" {
			frame.Title = "FOGCAST"
		}
		return frame
	}
	if view.Motion {
		cap := frame.Caption
		if cap == "" {
			cap = "preview"
		}
		frame.Hint = "A play | any back | " + cap
	} else {
		frame.Hint = "A play | any back"
	}
	if stills != nil {
		frame.Image = stills.Image(view.Handle)
		if view.NextHandle != "" && view.FadeT > 0 {
			frame.Next = stills.Image(view.NextHandle)
		}
		if view.Marquee != "" {
			frame.Marquee = stills.Image(view.Marquee)
		}
	}
	if len(view.Wall) >= 4 {
		wall := make([]fbgrid.AttractWallTile, 4)
		for i := 0; i < 4; i++ {
			if stills != nil {
				wall[i].Image = stills.Image(view.Wall[i].Handle)
			}
			wall[i].Video = view.Wall[i].Motion
		}
		frame.Wall = wall
	}
	if frame.Title == "" {
		frame.Title = "FOGCAST"
	}
	return frame
}

func gameTile(game tenfoot.Game, covers *tenfoot.CoverCache, pres tenfoot.Presentation, th theme.Theme, nameMax int) fbgrid.Tile {
	name := asciiLabel(game.Title)
	if name == "" {
		name = asciiLabel(game.System)
	}
	if name == "" {
		name = "UNTITLED"
	}
	if nameMax < 4 {
		nameMax = 18
	}
	tile := fbgrid.Tile{
		Name:      truncateLabel(name, nameMax),
		Color:     th.SystemColor(game.System),
		CoverKind: fbgrid.CoverMissing,
		Badges:    kitlauncher.TitleBadges(game, pres),
		Meta:      tileMeta(game, pres),
	}
	if handle := tenfoot.CoverHandle(game, pres); handle != "" {
		if covers != nil {
			tile.Cover = covers.Image(handle)
			switch covers.Status(handle) {
			case tenfoot.CoverReady:
				tile.CoverKind = fbgrid.CoverPresent
			case tenfoot.CoverLoading:
				tile.CoverKind = fbgrid.CoverLoading
			default:
				tile.CoverKind = fbgrid.CoverMissing
			}
		}
	}
	if handle := tenfoot.LogoHandle(pres); handle != "" && covers != nil && covers.Status(handle) == tenfoot.CoverReady {
		tile.Logo = covers.Image(handle)
	}
	return tile
}

func modelFooter(m kitlauncher.Model) string {
	status := strings.TrimSpace(m.Message)
	if status == "" {
		switch {
		case m.Busy:
			status = "Working"
		case !m.Connected:
			status = "Host unavailable"
		case !m.TargetReady:
			status = "Kit not ready"
		case !m.ControllerConnected:
			status = "Connect USB gamepad"
		case m.Session.State == "active":
			status = "Select+Start stop"
		case m.WheelOpen:
			status = m.WheelHint()
		default:
			status = m.GridHint()
		}
	}
	return truncateLabel(asciiLabel(status), chromeLabelMax)
}

func modelWheelFrame(m kitlauncher.Model, covers, stills *tenfoot.CoverCache, presentations *tenfoot.PresentationCache, th theme.Theme, width, height int) fbgrid.WheelFrame {
	th = th.Complete()
	items := m.WheelItems()
	frame := fbgrid.WheelFrame{
		Width:    width,
		Height:   height,
		Header:   truncateLabel(asciiLabel(packHeader(m)), chromeLabelMax),
		Footer:   modelFooter(m),
		Title:    asciiLabel(m.ShelfLabel()),
		Stats:    asciiLabel(m.WheelStats()),
		Featured: asciiLabel(m.WheelFeaturedTitle()),
		Color:    th.SystemColor(m.Shelf),
		Focus:    m.WheelIndex(),
		Theme:    th,
		Session:  m.SessionChrome(),
	}
	frame.Items = make([]fbgrid.WheelItem, 0, len(items))
	for _, item := range items {
		cell := fbgrid.WheelItem{ID: item.ID, Label: asciiLabel(item.Label), Color: th.SystemColor(item.ID)}
		if game, ok := m.WheelGame(item.ID); ok && presentations != nil && covers != nil {
			if handle := tenfoot.LogoHandle(presentations.Get(game.ID)); handle != "" && covers.Status(handle) == tenfoot.CoverReady {
				cell.Logo = covers.Image(handle)
			}
		}
		frame.Items = append(frame.Items, cell)
	}
	focusedPres := tenfoot.Presentation{}
	if game, ok := m.WheelGame(m.Shelf); ok && presentations != nil {
		focusedPres = presentations.Get(game.ID)
	}
	if handle := m.WheelHeroHandle(focusedPres); handle != "" && stills != nil {
		frame.Hero = stills.Image(handle)
		switch stills.Status(handle) {
		case tenfoot.CoverReady:
			frame.HeroKind = fbgrid.CoverPresent
		case tenfoot.CoverLoading:
			frame.HeroKind = fbgrid.CoverLoading
		default:
			frame.HeroKind = fbgrid.CoverMissing
		}
	} else {
		frame.HeroKind = fbgrid.CoverMissing
	}
	if handle := m.WheelLogoHandle(focusedPres); handle != "" && covers != nil && covers.Status(handle) == tenfoot.CoverReady {
		frame.Logo = covers.Image(handle)
	}
	return frame
}

func kitMetaLine(facts string) string {
	return asciiLabel(strings.ReplaceAll(facts, "  \u00b7  ", " | "))
}

func tileMeta(game tenfoot.Game, pres tenfoot.Presentation) string {
	d := tenfoot.GameDetail(game, pres)
	if d.Platform != "" {
		d.Platform = strings.ToUpper(d.Platform)
	}
	return kitMetaLine(d.MetaFacts())
}

func asciiLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			b.WriteByte('?')
			s = s[1:]
			continue
		}
		if r < 0x20 || r > 0x7e {
			b.WriteByte('?')
		} else {
			b.WriteByte(byte(r))
		}
		s = s[size:]
	}
	return b.String()
}

func truncateLabel(s string, max int) string {
	if max < 4 {
		max = 4
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}
