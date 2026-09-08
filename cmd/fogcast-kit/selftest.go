package main

import (
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/kitlauncher"
	"github.com/DeanoC/FogCast/remoteinput"
	"time"
)

func runNavSelftest(fbPath string) error {
	d, err := gfx.OpenLinuxFB(fbPath)
	if err != nil {
		return err
	}
	defer d.Close()
	report, err := exerciseNavGrid(d)
	fmt.Print(report)
	return err
}

func exerciseNavGrid(d *gfx.LinuxFB) (string, error) {
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
		g := paintModel(d, m)
		start, end := catalogPage(m.Focus, len(m.Games))
		hx, hy, ok := g.HighlightSample()
		if !ok {
			return fmt.Errorf("%s: no highlight", name)
		}
		gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(d.Destination(), d.Config(), hx, hy)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s focus=%d page=%d:%d highlight=(%d,%d) bgrx=%d,%d,%d,%d local=%d\n",
			name, m.Focus, start, end, hx, hy, gotB, gotG, gotR, gotX, g.Focus)
		if m.Focus != wantFocus {
			return fmt.Errorf("%s: focus %d want %d", name, m.Focus, wantFocus)
		}
		if start != wantPage {
			return fmt.Errorf("%s: page start %d want %d", name, start, wantPage)
		}
		if gotB != 0 || gotG != 220 || gotR != 255 || gotX != 0 {
			return fmt.Errorf("%s: highlight bgrx %d,%d,%d,%d", name, gotB, gotG, gotR, gotX)
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

func paintModel(d *gfx.LinuxFB, m kitlauncher.Model) fbgrid.Grid {
	cfg := d.Config()
	g := modelGrid(m, cfg.Width, cfg.Height, nil)
	fbgrid.Paint(d, g)
	d.Present()
	return g
}

func press(m *kitlauncher.Model, name string) {
	e, _ := remoteinput.NormalizeGamepad(name, true)
	m.Input(e, time.Now())
}
