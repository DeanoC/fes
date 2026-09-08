// fogcast-kit runs the controller/session adapter on the native target.
package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/host/tenfoot"
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

const gridPageSize = 12

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fogcast-kit:", err)
		os.Exit(1)
	}
}
func run() error {
	configPath := flag.String("config", "/media/fat/fogcast/launcher.json", "provisioned launcher configuration")
	inputProfile := flag.String("input-profile", "", "identity, swap-ab, or JSON profile path (default identity)")
	themeSpec := flag.String("theme", "", "default, arcade, night, or JSON/TOML path (default default)")
	selftestNav := flag.Bool("selftest-nav", false, "paint 4x3 catalog navigation on the framebuffer and exit")
	selftestPads := flag.Bool("selftest-pads", false, "open eligible USB pads, print them, and exit")
	selftestTheme := flag.Bool("selftest-theme", false, "paint default and arcade, prove type-role sizes, and sample pixels, then exit")
	selftestFPGA := flag.Bool("selftest-fpga", false, "record FC2D attract still/anim on the FPGA software-replay backend and exit")
	selftestShelf := flag.Bool("selftest-shelf", false, "paint system shelves, cycle L/R, sample header, and exit")
	selftestText := flag.Bool("selftest-text", false, "paint UI-face chrome and prove it is not DebugText, then exit")
	selftestBold := flag.Bool("selftest-bold", false, "prove title chrome uses gobold, nest detail/text/nav, then exit")
	selftestCover := flag.Bool("selftest-cover", false, "paint cover decode, placeholder, and chrome polish, then exit")
	selftestAttract := flag.Bool("selftest-attract", false, "arm short idle stills attract, paint a still, dismiss, then exit")
	selftestDetail := flag.Bool("selftest-detail", false, "open/close title detail, paint cover and title ink, then exit")
	selftestMotion := flag.Bool("selftest-motion", false, "prove focus pop and confirm pulse over ticks, then exit")
	selftestWheel := flag.Bool("selftest-wheel", false, "paint platform wheel and hero, enter a system grid, then exit")
	flag.Parse()
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
	if *selftestNav || *selftestShelf || *selftestText || *selftestBold || *selftestCover || *selftestAttract || *selftestDetail || *selftestMotion || *selftestWheel {
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
		if *selftestMotion {
			return runMotionSelftest(fb, th)
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
	detailWas := false
	var detailAt time.Time
	present := func(m kitlauncher.Model) {
		now := time.Now()
		if m.AttractActive && !m.Busy {
			lastFocus = -1
			detailWas = false
			handles := m.AttractPrefetchHandles()
			stills.Keep(handles)
			stills.Request(ctx, client.Library, handles)
			view := m.AttractView(now)
			key := modelRenderKey(m, covers.Generation(), 0)
			key.Attract = true
			key.AttractIndex = view.Index
			key.AttractHandle = view.Handle
			key.AttractFade = int(view.FadeT * 10)
			key.Stills = stills.Generation()
			if now.Sub(last) < 100*time.Millisecond && key == lastKey {
				return
			}
			last = now
			lastKey = key
			cfg := d.Config()
			fbgrid.PaintAttract(d, attractFrame(view, stills, th, cfg.Width, cfg.Height))
			d.Present()
			return
		}
		if m.WheelOpen && !m.Busy {
			detailWas = false
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
			frame := modelWheelFrame(m, covers, stills, presentations, th, w, h)
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
			if !frame.MotionActive() && now.Sub(last) < 100*time.Millisecond && key == lastKey {
				return
			}
			last = now
			lastKey = key
			fbgrid.PaintWheel(d, frame)
			d.Present()
			return
		}
		start, end := catalogPage(m.Focus, len(m.Games))
		prefetch := end + gridPageSize
		if prefetch > len(m.Games) {
			prefetch = len(m.Games)
		}
		ids := tenfoot.PageIDs(m.Games, start, prefetch)
		presentations.Keep(ids)
		presentations.Request(ctx, client.Library, ids)
		handles := tenfoot.CollectCoverHandles(m.Games, start, prefetch, presentations.Get)
		handles = append(handles, tenfoot.CollectLogoHandles(m.Games, start, prefetch, presentations.Get)...)
		if m.DetailOpen {
			handles = append(handles, m.DetailPrefetchHandles()...)
			if game, ok := focusedGame(m); ok {
				pres := presentations.Get(game.ID)
				if handle := tenfoot.CoverHandle(game, pres); handle != "" {
					handles = append(handles, handle)
				}
				if handle := tenfoot.LogoHandle(pres); handle != "" {
					handles = append(handles, handle)
				}
			}
		}
		covers.Keep(handles)
		covers.Request(ctx, client.Library, handles)
		cfg := d.Config()
		w, h := cfg.Width, cfg.Height
		grid := modelGrid(m, w, h, covers, presentations, th)
		if lastFocus >= 0 && grid.Focus != lastFocus {
			popAt = now
		}
		lastFocus = grid.Focus
		fbgrid.ArmPop(&grid, popAt, now)
		fade := 0.0
		if m.DetailOpen {
			if !detailWas {
				detailAt = now
			}
			detailWas = true
			fade = fbgrid.DetailFadeFromBlack(now.Sub(detailAt))
		} else {
			detailWas = false
		}
		key := modelRenderKey(m, covers.Generation(), presentations.Generation())
		if !grid.MotionActive() && fade <= 0 && now.Sub(last) < 100*time.Millisecond && key == lastKey {
			return
		}
		last = now
		lastKey = key
		if m.DetailOpen {
			frame := modelDetailFrame(m, covers, presentations, th, w, h)
			frame.FadeFromBlack = fade
			fbgrid.PaintDetail(d, frame)
			d.Present()
			return
		}
		fbgrid.Paint(d, grid)
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
	Focus, GameCount, AttractIndex, AttractFade, Shot int
	FocusID, Message, Shelf, AttractHandle            string
	SessionState, Execution, GameID                   string
	Busy, Connected, TargetReady, ControllerConnected bool
	Attract, Detail, Wheel                            bool
	Covers, Stills, Presentations                     uint64
}

func modelRenderKey(m kitlauncher.Model, covers, presentations uint64) renderKey {
	focusID := ""
	if m.Focus >= 0 && m.Focus < len(m.Games) {
		focusID = m.Games[m.Focus].ID
	}
	return renderKey{
		Focus: m.Focus, GameCount: len(m.Games), FocusID: focusID,
		Message: m.Message, Shelf: m.Shelf, SessionState: m.Session.State, Execution: m.Session.Execution,
		GameID: m.Session.GameID, Busy: m.Busy, Connected: m.Connected,
		TargetReady: m.TargetReady, ControllerConnected: m.ControllerConnected,
		Detail: m.DetailOpen, Wheel: m.WheelOpen, Shot: m.ShotIndex(),
		Covers: covers, Presentations: presentations,
	}
}

func catalogPage(focus, n int) (start, end int) {
	if n <= 0 {
		return 0, 0
	}
	if focus > 0 {
		start = (focus / gridPageSize) * gridPageSize
	}
	if start >= n {
		start = 0
	}
	end = start + gridPageSize
	if end > n {
		end = n
	}
	return start, end
}

// modelGrid maps the live catalog to one visible 4×3 page. Model.Focus remains
// an index into the complete catalog; the grid focus is page-local.
func modelGrid(m kitlauncher.Model, width, height int, covers *tenfoot.CoverCache, presentations *tenfoot.PresentationCache, th theme.Theme) fbgrid.Grid {
	start, end := catalogPage(m.Focus, len(m.Games))
	tiles := make([]fbgrid.Tile, 0, end-start)
	for _, game := range m.Games[start:end] {
		pres := tenfoot.Presentation{}
		if presentations != nil {
			pres = presentations.Get(game.ID)
		}
		tiles = append(tiles, gameTile(game, covers, pres, th))
	}
	g := fbgrid.NewWithTiles(width, height, tiles)
	fbgrid.ApplyTheme(&g, th)
	g.Header = truncateLabel(asciiLabel(m.HeaderChrome()), 36)
	if m.Focus >= start && m.Focus < end {
		g.Focus = m.Focus - start
	}
	g.Footer = modelFooter(m)
	return g
}

func modelDetailFrame(m kitlauncher.Model, covers *tenfoot.CoverCache, presentations *tenfoot.PresentationCache, th theme.Theme, width, height int) fbgrid.DetailFrame {
	detail := m.FocusDetail()
	title := asciiLabel(detail.Title)
	if title == "" {
		title = "UNTITLED"
	}
	frame := fbgrid.DetailFrame{
		Width:       width,
		Height:      height,
		Header:      truncateLabel(asciiLabel(m.HeaderChrome()), 36),
		Title:       title,
		Meta:        kitMetaLine(detail.MetaFacts()),
		Description: asciiLabel(detail.Summary),
		Hint:        asciiLabel(detailFooter(m)),
		Theme:       th,
		Color:       th.SystemColor(detail.Platform),
	}
	if game, ok := focusedGame(m); ok {
		frame.Color = th.SystemColor(game.System)
		pres := tenfoot.Presentation{}
		if presentations != nil {
			pres = presentations.Get(game.ID)
		}
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
	if shot := m.ShotHandle(); shot != "" {
		n := len(detail.ScreenshotIDs)
		frame.ShotCaption = asciiLabel(shotCaption(m.ShotIndex(), n))
		if covers != nil {
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

func focusedGame(m kitlauncher.Model) (tenfoot.Game, bool) {
	if m.Focus < 0 || m.Focus >= len(m.Games) {
		return tenfoot.Game{}, false
	}
	return m.Games[m.Focus], true
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

func attractFrame(view kitlauncher.AttractView, stills *tenfoot.CoverCache, th theme.Theme, width, height int) fbgrid.AttractFrame {
	frame := fbgrid.AttractFrame{
		Width:  width,
		Height: height,
		Title:  asciiLabel(view.Title),
		Empty:  view.Empty,
		FadeT:  view.FadeT,
		Theme:  th,
	}
	if view.Empty {
		frame.Hint = "any back"
		if frame.Title == "" {
			frame.Title = "FOGCAST"
		}
		return frame
	}
	frame.Hint = "A play | any back"
	if stills != nil {
		frame.Image = stills.Image(view.Handle)
		if view.NextHandle != "" && view.FadeT > 0 {
			frame.Next = stills.Image(view.NextHandle)
		}
	}
	if frame.Title == "" {
		frame.Title = "FOGCAST"
	}
	return frame
}

func gameTile(game tenfoot.Game, covers *tenfoot.CoverCache, pres tenfoot.Presentation, th theme.Theme) fbgrid.Tile {
	name := asciiLabel(game.Title)
	if name == "" {
		name = asciiLabel(game.System)
	}
	if name == "" {
		name = "UNTITLED"
	}
	tile := fbgrid.Tile{Name: truncateLabel(name, 18), Color: th.SystemColor(game.System), CoverKind: fbgrid.CoverMissing}
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
	return truncateLabel(asciiLabel(status), 36)
}

func modelWheelFrame(m kitlauncher.Model, covers, stills *tenfoot.CoverCache, presentations *tenfoot.PresentationCache, th theme.Theme, width, height int) fbgrid.WheelFrame {
	th = th.Complete()
	items := m.WheelItems()
	frame := fbgrid.WheelFrame{
		Width:    width,
		Height:   height,
		Header:   truncateLabel(asciiLabel(m.HeaderChrome()), 36),
		Footer:   modelFooter(m),
		Title:    asciiLabel(m.ShelfLabel()),
		Stats:    asciiLabel(m.WheelStats()),
		Featured: asciiLabel(m.WheelFeaturedTitle()),
		Color:    th.SystemColor(m.Shelf),
		Focus:    m.WheelIndex(),
		Theme:    th,
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
