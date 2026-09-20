// Command tenfoot-linuxfb-grid paints a fake cover-grid onto a Linux
// framebuffer. D-pad/stick moves the highlight, South/Enter confirms
// (tile flash + label), Start/ESC/Q quits. It is CGO-free and does not
// use SDL, the host API, or the target agent.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/ui/fbgrid"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/linuxinput"
	"github.com/DeanoC/FogCast/ui/theme"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tenfoot-linuxfb-grid", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	device := fs.String("fb", "/dev/fb0", "framebuffer device")
	hold := fs.Duration("hold", 30*time.Second, "leave the grid on screen")
	inputFlag := fs.String("input", "auto", "comma-separated input nodes, auto, or none")
	selftest := fs.Bool("selftest", false, "inject js right+confirm+quit through a pipe (no live pads)")
	themeSpec := fs.String("theme", "", "default, arcade, night, or JSON/TOML path (default default)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	d, err := gfx.OpenLinuxFB(*device)
	if err != nil {
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: %v\n", err)
		return 1
	}
	defer d.Close()
	cfg := d.Config()
	fmt.Fprintf(stdout, "tenfoot-linuxfb-grid %s/%s cgo=0 backend=%s fb=%s %s\n",
		runtime.GOOS, runtime.GOARCH, d.BackendName(), *device, cfg)

	reader, paths := openInputs(*inputFlag, *selftest, stdout, stderr)
	if reader != nil {
		defer reader.Close()
	}

	th, err := theme.Resolve(*themeSpec)
	if err != nil {
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: %v\n", err)
		return 1
	}
	g := fbgrid.New(cfg.Width, cfg.Height)
	fbgrid.ApplyTheme(&g, th)
	paint := func() {
		fbgrid.Paint(d, g)
		d.Present()
	}

	if *selftest {
		return runSelftest(stdout, stderr, d, reader, &g, paint)
	}

	paint()
	if err := reportSamples(stdout, d, g, "focus"); err != nil {
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: %v\n", err)
		return 1
	}
	if *hold <= 0 {
		return 0
	}
	if reader == nil {
		fmt.Fprintf(stdout, "presented fake cover-grid; holding %s (input none)\n", hold)
		ctxWait(*hold)
		return 0
	}
	fmt.Fprintf(stdout, "presented fake cover-grid; input %s hold %s (A/Enter confirm, START/ESC/Q or JS 7/9 quit)\n",
		strings.Join(paths, ","), hold)
	return runInputLoop(stdout, reader, &g, paint, *hold)
}

func openInputs(flag string, selftest bool, stdout, stderr io.Writer) (*linuxinput.Reader, []string) {
	if selftest {
		if found, err := linuxinput.Discover(); err == nil {
			for _, p := range found {
				fmt.Fprintf(stdout, "input present %s (selftest uses injected pipe)\n", p)
			}
		}
		return linuxinput.NewReader(), nil
	}
	paths := resolveInputPaths(flag)
	var reader *linuxinput.Reader
	if len(paths) > 0 {
		r, err := linuxinput.OpenPaths(paths)
		if err != nil {
			fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: input: %v\n", err)
		} else {
			reader = r
			for _, info := range r.Info() {
				fmt.Fprintf(stdout, "input opened %s name=%q kind=%s\n", info.Path, info.Name, info.Kind)
			}
		}
	}
	return reader, paths
}

func resolveInputPaths(flag string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(flag, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "none" {
			continue
		}
		if part == "auto" {
			found, err := linuxinput.Discover()
			if err != nil {
				continue
			}
			for _, p := range found {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
			continue
		}
		if !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

func runInputLoop(stdout io.Writer, reader *linuxinput.Reader, g *fbgrid.Grid, paint func(), hold time.Duration) int {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(16 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(hold)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			fmt.Fprintf(stdout, "hold elapsed focus=%d selected=%q last=%s\n", g.Focus, g.Selected, g.Last)
			return 0
		case <-c:
			fmt.Fprintf(stdout, "signal focus=%d selected=%q\n", g.Focus, g.Selected)
			return 0
		case <-tick.C:
			for _, m := range reader.Poll() {
				g.Apply(m)
			}
			if g.Quit {
				fmt.Fprintf(stdout, "quit from %s focus=%d selected=%q\n", g.Last, g.Focus, g.Selected)
				return 0
			}
			g.Tick()
			paint()
		}
	}
}

func runSelftest(stdout, stderr io.Writer, d *gfx.LinuxFB, reader *linuxinput.Reader, g *fbgrid.Grid, paint func()) int {
	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: pipe: %v\n", err)
		return 1
	}
	if err := reader.Add(pr, linuxinput.KindJoystick, "selftest.js", "selftest"); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: %v\n", err)
		return 1
	}
	start := g.Focus
	paint()
	if err := reportSamples(stdout, d, *g, "before"); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: %v\n", err)
		return 1
	}
	if _, err := pw.Write(linuxinput.EncodeJS(32767, linuxinput.JSEventAxis, 0)); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: write axis: %v\n", err)
		return 1
	}
	if !waitFocus(reader, g, start+1, 2*time.Second) {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: no move from injected axis\n")
		return 1
	}
	paint()
	if err := reportSamples(stdout, d, *g, "after-move"); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "selftest focus %d -> %d\n", start, g.Focus)
	if _, err := pw.Write(linuxinput.EncodeJS(1, linuxinput.JSEventButton, 0)); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: write confirm: %v\n", err)
		return 1
	}
	if !waitSelected(reader, g, 2*time.Second) {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: no confirm from injected button 0\n")
		return 1
	}
	paint()
	if err := reportSamples(stdout, d, *g, "after-confirm"); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "selftest selected %q focus=%d\n", g.Selected, g.Focus)
	if _, err := pw.Write(linuxinput.EncodeJS(1, linuxinput.JSEventButton, 7)); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: write quit: %v\n", err)
		return 1
	}
	if !waitQuit(reader, g, 2*time.Second) {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-grid: no quit from injected button 7\n")
		return 1
	}
	_ = pw.Close()
	fmt.Fprintf(stdout, "selftest quit from %s focus=%d selected=%q\n", g.Last, g.Focus, g.Selected)
	return 0
}

func waitFocus(reader *linuxinput.Reader, g *fbgrid.Grid, want int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, m := range reader.Poll() {
			g.Apply(m)
		}
		if g.Focus == want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func waitSelected(reader *linuxinput.Reader, g *fbgrid.Grid, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, m := range reader.Poll() {
			g.Apply(m)
		}
		if g.Selected != "" && g.ConfirmLeft > 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func waitQuit(reader *linuxinput.Reader, g *fbgrid.Grid, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, m := range reader.Poll() {
			g.Apply(m)
		}
		if g.Quit {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func reportSamples(w io.Writer, d *gfx.LinuxFB, g fbgrid.Grid, tag string) error {
	hx, hy, ok := g.HighlightSample()
	if !ok {
		return fmt.Errorf("no highlight sample")
	}
	b, gv, r, xx, err := gfx.SampleBGRX(d.Destination(), d.Config(), hx, hy)
	if err != nil {
		return err
	}
	th := g.Theme.Complete()
	okHL := b == th.Highlight.B && gv == th.Highlight.G && r == th.Highlight.R && xx == 0
	fmt.Fprintf(w, "sample highlight-%s (%d,%d) bgrx=%d,%d,%d,%d want=%d,%d,%d,0 ok=%v theme=%s\n",
		tag, hx, hy, b, gv, r, xx, th.Highlight.B, th.Highlight.G, th.Highlight.R, okHL, th.Name)
	if !okHL {
		return fmt.Errorf("sample highlight-%s mismatch", tag)
	}
	ix, iy, ok := g.InteriorSample()
	if !ok {
		return fmt.Errorf("no interior sample")
	}
	b, gv, r, xx, err = gfx.SampleBGRX(d.Destination(), d.Config(), ix, iy)
	if err != nil {
		return err
	}
	want := g.Tiles[g.Focus].Color
	if g.ConfirmLeft > 0 {
		want = th.Flash
	}
	okIn := b == want.B && gv == want.G && r == want.R && xx == 0
	fmt.Fprintf(w, "sample interior-%s (%d,%d) bgrx=%d,%d,%d,%d want=%d,%d,%d,0 ok=%v focus=%d selected=%q\n",
		tag, ix, iy, b, gv, r, xx, want.B, want.G, want.R, okIn, g.Focus, g.Selected)
	if !okIn {
		return fmt.Errorf("sample interior-%s mismatch", tag)
	}
	return nil
}

func ctxWait(d time.Duration) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-c:
	}
}
