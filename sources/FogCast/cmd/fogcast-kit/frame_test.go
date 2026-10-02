package main

import (
	"context"
	"image"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/fbgrid"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
	"github.com/DeanoC/FogCast/ui/theme"
)

type idleMenuClient struct {
	generation atomic.Uint64
	calls      atomic.Int32
}

func (c *idleMenuClient) Status(context.Context) (menudisplay.Status, error) {
	return menudisplay.Status{Available: true, Generation: c.generation.Load()}, nil
}

func (c *idleMenuClient) Present(_ context.Context, _ uint64, _ []byte) (menudisplay.Result, error) {
	c.calls.Add(1)
	return menudisplay.Result{Generation: c.generation.Load()}, nil
}

func TestKitIdleMenuGenerationAndResume(t *testing.T) {
	client := &idleMenuClient{}
	client.generation.Store(1)
	d, err := gfx.NewMenuDisplayWithPresenter(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p := newKitFramePainter(d)
	now := time.Now()
	key := renderKey{}
	p.shouldPaint(now, key, false)
	d.Clear(gfx.RGB(1, 2, 3))
	p.Present()
	wait := func(calls int32) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for client.calls.Load() < calls || d.FramePending() {
			if time.Now().After(deadline) {
				t.Fatalf("present calls %d, want %d", client.calls.Load(), calls)
			}
			time.Sleep(time.Millisecond)
		}
	}
	wait(1)
	for i := 1; i < 30; i++ {
		p.shouldPaint(now.Add(time.Duration(i)*time.Millisecond), key, false)
	}
	if d.FramePending() || client.calls.Load() != 1 {
		t.Fatal("unchanged revision resubmitted")
	}
	client.generation.Store(2)
	// The menu probe clock is real; hold the scene clock within its idle gate
	// so recovery here must use the retained frame, not a fallback redraw.
	time.Sleep(1050 * time.Millisecond)
	if p.shouldPaint(now.Add(30*time.Millisecond), key, false) {
		t.Fatal("generation probe rasterized")
	}
	wait(2)
	if err := d.Pause(t.Context()); err != nil {
		t.Fatal(err)
	}
	d.Resume()
	if p.shouldPaint(now.Add(31*time.Millisecond), key, false) {
		t.Fatal("Resume rasterized")
	}
	wait(3)
}

type revisionRecorder struct {
	*gfx.Recorder
	revisions []uint64
}

func (r *revisionRecorder) PresentRevision(revision uint64) {
	r.revisions = append(r.revisions, revision)
}

func TestKitIdleFramesAndInvalidation(t *testing.T) {
	d := &revisionRecorder{Recorder: gfx.NewRecorder()}
	p := newKitFramePainter(d)
	now := time.Unix(1700000000, 0)
	key := renderKey{FocusID: "pong", Connected: true}
	grid := fbgrid.New(640, 360)
	paint := func(at time.Time, key renderKey, moving bool) bool {
		if !p.shouldPaint(at, key, moving) {
			return false
		}
		fbgrid.Paint(p, grid)
		presentKitFrame(p, grid.Width, theme.Default(), "KIT test")
		return true
	}
	if !paint(now, key, false) {
		t.Fatal("first frame skipped")
	}
	calls := len(d.Calls)
	for i := 1; i < 30; i++ {
		if paint(now.Add(time.Duration(i)*time.Second/30), key, false) {
			t.Fatal("idle tick rasterized")
		}
	}
	if len(d.Calls) != calls || len(d.revisions) != 30 || d.revisions[29] != 1 {
		t.Fatal("idle tick must service the same revision without draw commands")
	}
	if !paint(now.Add(kitIdleRefresh), key, false) || p.revision != 2 {
		t.Fatal("bounded fallback refresh skipped")
	}
	// Known UI and asynchronous resource changes draw on the next tick.
	for _, mutate := range []func(){
		func() { key.Focus++ },
		func() { key.Covers++ },
		func() { key.Stills++ },
		func() { key.Presentations++ },
		func() { key.CoreStatusRevision++ },
		func() { key.Message = "Starting Pong" },
		func() { key.Detail = true },
		func() { key.Search = true },
		func() { key.Pack = "neon" },
	} {
		mutate()
		if !paint(now.Add(kitIdleRefresh+time.Millisecond), key, false) {
			t.Fatal("changed scene skipped")
		}
	}
	if !paint(now.Add(kitIdleRefresh+2*time.Millisecond), key, true) ||
		!paint(now.Add(kitIdleRefresh+3*time.Millisecond), key, true) ||
		!paint(now.Add(kitIdleRefresh+4*time.Millisecond), key, false) {
		t.Fatal("animation or its final settled frame skipped")
	}
	if paint(now.Add(kitIdleRefresh+5*time.Millisecond), key, false) {
		t.Fatal("settled frame rasterized again")
	}
}

func TestKitIdleLinuxFBDoesNotPresent(t *testing.T) {
	d := gfx.NewRecorder()
	p := newKitFramePainter(d)
	now := time.Unix(1700000000, 0)
	key := renderKey{}
	p.shouldPaint(now, key, false)
	p.Present()
	if p.shouldPaint(now.Add(time.Millisecond), key, false) || len(d.Calls) != 1 || d.Calls[0].Op != "Present" {
		t.Fatalf("unchanged framebuffer was presented: %v", d.Ops())
	}
}

// Measures the real grid raster/present path at 30 ticks per simulated second,
// including the periodic refresh, rather than just timing the idle gate.
func BenchmarkKitGridIdle(b *testing.B) {
	for _, refresh := range []time.Duration{100 * time.Millisecond, kitIdleRefresh} {
		b.Run(refresh.String(), func(b *testing.B) {
			d, err := gfx.NewSoftware(1280, 720)
			if err != nil {
				b.Fatal(err)
			}
			defer d.Close()
			p := newKitFramePainter(d)
			grid := fbgrid.New(1280, 720)
			now := time.Unix(1700000000, 0)
			key := renderKey{}
			last := now.Add(-refresh)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				at := now.Add(time.Duration(i) * (time.Second / 30))
				ready := false
				if refresh == kitIdleRefresh {
					ready = p.shouldPaint(at, key, false)
				} else if at.Sub(last) >= refresh {
					last, ready = at, true
				}
				if ready {
					fbgrid.Paint(p, grid)
					presentKitFrame(p, grid.Width, theme.Default(), "KIT benchmark")
				}
			}
		})
	}
}

func TestWheelRefreshSkipsOnlyIdenticalSettledScenes(t *testing.T) {
	var cache wheelFrameCache
	frame := fbgrid.WheelFrame{Width: 1280, Height: 720, Theme: theme.Default(),
		Hero:  image.NewRGBA(image.Rect(0, 0, 8, 8)),
		Items: []fbgrid.WheelItem{{ID: "nes", Label: "NES"}}}
	if !cache.shouldPaint(frame, false) || cache.shouldPaint(frame, false) {
		t.Fatal("first paint or idle reuse failed")
	}
	frame.Footer = "changed title"
	if !cache.shouldPaint(frame, false) {
		t.Fatal("refresh hid changed footer")
	}
	frame.Hero = image.NewRGBA(image.Rect(0, 0, 9, 8))
	if !cache.shouldPaint(frame, false) {
		t.Fatal("refresh hid changed artwork")
	}
	frame.Items = []fbgrid.WheelItem{{ID: "snes", Label: "SNES"}}
	if !cache.shouldPaint(frame, false) {
		t.Fatal("refresh hid changed wheel")
	}
	if !cache.shouldPaint(frame, true) || !cache.shouldPaint(frame, true) || !cache.shouldPaint(frame, false) {
		t.Fatal("motion or final settled frame was skipped")
	}
	if cache.shouldPaint(frame, false) {
		t.Fatal("settled frame not reused")
	}
}
