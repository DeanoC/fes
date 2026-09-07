// Command tenfoot-linuxfb-spike paints a software-rasterized test pattern
// onto a Linux framebuffer. It is CGO-free and does not use SDL, the host
// API, or the target agent.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tenfoot-linuxfb-spike", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	device := fs.String("fb", "/dev/fb0", "framebuffer device")
	hold := fs.Duration("hold", 30*time.Second, "leave the pattern on screen")
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
	gfx.PaintLinuxFBSpike(d, cfg.Width, cfg.Height)
	d.Present()
	if err := reportSamples(stdout, d); err != nil {
		fmt.Fprintf(stderr, "tenfoot-linuxfb-spike: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "presented color bars + grid + FOGCAST; holding %s\n", hold)
	if *hold <= 0 {
		return 0
	}
	ctxWait(*hold)
	return 0
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
