package main

import (
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
	"github.com/DeanoC/FogCast/kitlauncher"
	"github.com/DeanoC/FogCast/kitlauncher/controller"
	"github.com/DeanoC/FogCast/remoteinput"
	"time"
)

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
