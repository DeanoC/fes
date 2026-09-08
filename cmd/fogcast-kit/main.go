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
	selftestCover := flag.Bool("selftest-cover", false, "paint cover decode, placeholder, and chrome polish, then exit")
	selftestAttract := flag.Bool("selftest-attract", false, "arm short idle stills attract, paint a still, dismiss, then exit")
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
	if *selftestNav || *selftestShelf || *selftestText || *selftestCover || *selftestAttract {
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
	last := time.Time{}
	var lastKey renderKey
	present := func(m kitlauncher.Model) {
		now := time.Now()
		if m.AttractActive && !m.Busy {
			handles := m.AttractPrefetchHandles()
			stills.Keep(handles)
			stills.Request(ctx, client.Library, handles)
			view := m.AttractView(now)
			key := modelRenderKey(m, covers.Generation())
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
		start, end := catalogPage(m.Focus, len(m.Games))
		prefetch := end + gridPageSize
		if prefetch > len(m.Games) {
			prefetch = len(m.Games)
		}
		handles := tenfoot.PageHandles(m.Games, start, prefetch)
		covers.Keep(handles)
		covers.Request(ctx, client.Library, handles)
		key := modelRenderKey(m, covers.Generation())
		if now.Sub(last) < 100*time.Millisecond && key == lastKey {
			return
		}
		last = now
		lastKey = key
		cfg := d.Config()
		w, h := cfg.Width, cfg.Height
		grid := modelGrid(m, w, h, covers, th)
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
	Focus, GameCount, AttractIndex, AttractFade       int
	FocusID, Message, Shelf, AttractHandle            string
	SessionState, Execution, GameID                   string
	Busy, Connected, TargetReady, ControllerConnected bool
	Attract                                           bool
	Covers, Stills                                    uint64
}

func modelRenderKey(m kitlauncher.Model, covers uint64) renderKey {
	focusID := ""
	if m.Focus >= 0 && m.Focus < len(m.Games) {
		focusID = m.Games[m.Focus].ID
	}
	return renderKey{
		Focus: m.Focus, GameCount: len(m.Games), FocusID: focusID,
		Message: m.Message, Shelf: m.Shelf, SessionState: m.Session.State, Execution: m.Session.Execution,
		GameID: m.Session.GameID, Busy: m.Busy, Connected: m.Connected,
		TargetReady: m.TargetReady, ControllerConnected: m.ControllerConnected,
		Covers: covers,
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
func modelGrid(m kitlauncher.Model, width, height int, covers *tenfoot.CoverCache, th theme.Theme) fbgrid.Grid {
	start, end := catalogPage(m.Focus, len(m.Games))
	tiles := make([]fbgrid.Tile, 0, end-start)
	for _, game := range m.Games[start:end] {
		tiles = append(tiles, gameTile(game, covers, th))
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

func gameTile(game tenfoot.Game, covers *tenfoot.CoverCache, th theme.Theme) fbgrid.Tile {
	name := asciiLabel(game.Title)
	if name == "" {
		name = asciiLabel(game.System)
	}
	if name == "" {
		name = "UNTITLED"
	}
	tile := fbgrid.Tile{Name: truncateLabel(name, 18), Color: th.SystemColor(game.System), CoverKind: fbgrid.CoverMissing}
	if handle := tenfoot.CoverHandle(game, tenfoot.Presentation{}); handle != "" {
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
		default:
			status = "A play | L/R shelf"
		}
	}
	return truncateLabel(asciiLabel(status), 36)
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
