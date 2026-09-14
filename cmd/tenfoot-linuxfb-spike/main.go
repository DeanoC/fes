// Command tenfoot-linuxfb-spike paints a software-rasterized test pattern
// onto a Linux framebuffer. With input nodes it moves a cursor from evdev
// or joystick events and quits on Start/ESC/Q (or JS button 7/9). It is
// CGO-free and does not use SDL, the host API, or the target agent.
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

	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot/linuxinput"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tenfoot-linuxfb-spike", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	device := fs.String("fb", "/dev/fb0", "framebuffer device")
	hold := fs.Duration("hold", 30*time.Second, "leave the pattern on screen")
	inputFlag := fs.String("input", "auto", "comma-separated input nodes, auto, or none")
	selftest := fs.Bool("selftest", false, "inject js right+quit through the reader and sample the cursor")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	d, err := gfx.OpenLinuxFB(*device)
	if err != nil {
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: %v\n", err)
		return 1
	}
	defer d.Close()
	cfg := d.Config()
	fmt.Fprintf(stdout, "tenfoot-linuxfb-spike %s/%s cgo=0 backend=%s fb=%s %s\n",
		runtime.GOOS, runtime.GOARCH, d.BackendName(), *device, cfg)

	reader, paths := openInputs(*inputFlag, *selftest, stdout, stderr)
	if reader != nil {
		defer reader.Close()
	}

	cur := linuxinput.NewCursor(cfg.Width, cfg.Height, gfx.LinuxFBCursorSize, 16)
	paint := func(status string) {
		gfx.PaintLinuxFBInput(d, cfg.Width, cfg.Height, cur.X, cur.Y, status)
		d.Present()
	}

	if *selftest {
		return runSelftest(stdout, stderr, d, reader, &cur, paint)
	}

	paint(cursorStatus(cur, "hold"))
	if err := reportSamples(stdout, d); err != nil {
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: %v\n", err)
		return 1
	}
	if *hold <= 0 {
		return 0
	}
	if reader == nil {
		fmt.Fprintf(stdout, "presented color bars + grid + FOGCAST; holding %s (input none)\n", hold)
		ctxWait(*hold)
		return 0
	}
	fmt.Fprintf(stdout, "presented color bars + cursor; input %s hold %s (START/ESC/Q or JS 7/9 quit)\n",
		strings.Join(paths, ","), hold)
	return runInputLoop(stdout, reader, &cur, paint, *hold)
}

func openInputs(flag string, selftest bool, stdout, stderr io.Writer) (*linuxinput.Reader, []string) {
	paths := resolveInputPaths(flag)
	var reader *linuxinput.Reader
	if len(paths) > 0 {
		r, err := linuxinput.OpenPaths(paths)
		if err != nil {
			fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: input: %v\n", err)
		} else {
			reader = r
			for _, info := range r.Info() {
				fmt.Fprintf(stdout, "input opened %s name=%q kind=%s\n", info.Path, info.Name, info.Kind)
			}
		}
	}
	if selftest {
		if reader == nil {
			reader = linuxinput.NewReader()
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

func runInputLoop(stdout io.Writer, reader *linuxinput.Reader, cur *linuxinput.Cursor, paint func(string), hold time.Duration) int {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(16 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(hold)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			fmt.Fprintf(stdout, "hold elapsed cursor=%d,%d last=%s\n", cur.X, cur.Y, cur.Last)
			return 0
		case <-c:
			fmt.Fprintf(stdout, "signal cursor=%d,%d\n", cur.X, cur.Y)
			return 0
		case <-tick.C:
			for _, m := range reader.Poll() {
				cur.Apply(m)
			}
			if cur.Quit {
				fmt.Fprintf(stdout, "quit from %s cursor=%d,%d\n", cur.Last, cur.X, cur.Y)
				return 0
			}
			cur.Tick()
			paint(cursorStatus(*cur, cur.Last))
		}
	}
}

func runSelftest(stdout, stderr io.Writer, d *gfx.LinuxFB, reader *linuxinput.Reader, cur *linuxinput.Cursor, paint func(string)) int {
	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: pipe: %v\n", err)
		return 1
	}
	if err := reader.Add(pr, linuxinput.KindJoystick, "selftest.js", "selftest"); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: %v\n", err)
		return 1
	}
	startX, startY := cur.X, cur.Y
	paint(cursorStatus(*cur, "selftest"))
	if err := reportSamples(stdout, d); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: %v\n", err)
		return 1
	}
	if err := reportCursor(stdout, d, *cur, "before"); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: %v\n", err)
		return 1
	}
	if _, err := pw.Write(linuxinput.EncodeJS(32767, linuxinput.JSEventAxis, 0)); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: write axis: %v\n", err)
		return 1
	}
	if !waitCursorMove(reader, cur, startX, 2*time.Second) {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: no move from injected axis\n")
		return 1
	}
	paint(cursorStatus(*cur, cur.Last))
	if err := reportCursor(stdout, d, *cur, "after"); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "selftest cursor (%d,%d) -> (%d,%d)\n", startX, startY, cur.X, cur.Y)
	if _, err := pw.Write(linuxinput.EncodeJS(1, linuxinput.JSEventButton, 7)); err != nil {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: write quit: %v\n", err)
		return 1
	}
	if !waitQuit(reader, cur, 2*time.Second) {
		_ = pw.Close()
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: no quit from injected button 7\n")
		return 1
	}
	_ = pw.Close()
	fmt.Fprintf(stdout, "selftest quit from %s cursor=%d,%d\n", cur.Last, cur.X, cur.Y)
	return 0
}

func waitCursorMove(reader *linuxinput.Reader, cur *linuxinput.Cursor, startX int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, m := range reader.Poll() {
			cur.Apply(m)
		}
		if cur.X != startX {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func waitQuit(reader *linuxinput.Reader, cur *linuxinput.Cursor, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, m := range reader.Poll() {
			cur.Apply(m)
		}
		if cur.Quit {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func cursorStatus(cur linuxinput.Cursor, last string) string {
	if last == "" {
		last = "idle"
	}
	return fmt.Sprintf("X=%d Y=%d %s", cur.X, cur.Y, last)
}

func reportCursor(w io.Writer, d *gfx.LinuxFB, cur linuxinput.Cursor, tag string) error {
	b, g, r, xx, err := gfx.SampleBGRX(d.Destination(), d.Config(), cur.SampleX(), cur.SampleY())
	if err != nil {
		return err
	}
	ok := b == 0 && g == 220 && r == 255 && xx == 0
	fmt.Fprintf(w, "sample cursor-%s (%d,%d) bgrx=%d,%d,%d,%d want=0,220,255,0 ok=%v\n",
		tag, cur.SampleX(), cur.SampleY(), b, g, r, xx, ok)
	if !ok {
		return fmt.Errorf("sample cursor-%s mismatch", tag)
	}
	return nil
}

func reportSamples(w io.Writer, d *gfx.LinuxFB) error {
	checks := []struct {
		x, y        int
		b, g, r, x8 byte
		name        string
	}{
		{40, 40, 255, 255, 255, 0, "white-bar"},
		{440, 40, 0, 0, 255, 0, "red-bar"},
		{520, 40, 255, 0, 0, 0, "blue-bar"},
	}
	for _, c := range checks {
		b, g, r, xx, err := gfx.SampleBGRX(d.Destination(), d.Config(), c.x, c.y)
		if err != nil {
			return err
		}
		ok := b == c.b && g == c.g && r == c.r && xx == c.x8
		fmt.Fprintf(w, "sample %s (%d,%d) bgrx=%d,%d,%d,%d want=%d,%d,%d,%d ok=%v\n",
			c.name, c.x, c.y, b, g, r, xx, c.b, c.g, c.r, c.x8, ok)
		if !ok {
			return fmt.Errorf("sample %s mismatch", c.name)
		}
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
