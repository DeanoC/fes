package gfx

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

// damageRuntime models a runtime that advertises damage: it rejects a stale
// generation, and a damage base that is not the frame it shows.
type damageRuntime struct {
	mu                    sync.Mutex
	generation, seq       uint64
	screen                []byte
	status, full, damaged int
	rejected              int
	staged                int
}

func (r *damageRuntime) Status(context.Context) (menudisplay.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status++
	return menudisplay.Status{Available: true, Generation: r.generation, DisplayedSequence: r.seq, Damage: true}, nil
}

func (r *damageRuntime) Present(_ context.Context, generation uint64, pix []byte) (menudisplay.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.generation {
		r.rejected++
		return menudisplay.Result{}, errors.New("stale generation")
	}
	r.full++
	r.screen = append(r.screen[:0], pix...)
	r.seq++
	return menudisplay.Result{Generation: generation, DisplayedSequence: r.seq}, nil
}

func (r *damageRuntime) PresentDamage(_ context.Context, generation uint64, pix []byte, damage menudisplay.Damage) (menudisplay.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.generation || uint64(damage.BaseSequence) != r.seq || r.screen == nil {
		r.rejected++
		return menudisplay.Result{}, errors.New("stale_revision")
	}
	r.damaged++
	for _, rect := range damage.Rects {
		r.staged += rect.W * rect.H
		for y := rect.Y; y < rect.Y+rect.H; y++ {
			at := y*menudisplay.Stride + rect.X*4
			copy(r.screen[at:at+rect.W*4], pix[at:at+rect.W*4])
		}
	}
	r.seq++
	return menudisplay.Result{Generation: generation, DisplayedSequence: r.seq}, nil
}

func (r *damageRuntime) counts() (status, full, damaged, rejected int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, r.full, r.damaged, r.rejected
}

type damageHarness struct {
	t      *testing.T
	d      *MenuDisplay
	r      *damageRuntime
	clock  *menuClock
	mu     sync.Mutex
	traces []MenuPresentTrace
}

func newDamageHarness(t *testing.T) *damageHarness {
	t.Helper()
	h := &damageHarness{t: t, r: &damageRuntime{generation: 3}, clock: newMenuClock()}
	d, err := newMenuDisplayWithClient(h.r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	d.now = h.clock.Now
	d.SetChangeDriven(true)
	d.SetTraceSink(func(p MenuPresentTrace) {
		h.mu.Lock()
		h.traces = append(h.traces, p)
		h.mu.Unlock()
	})
	h.d = d
	return h
}

// frame paints a box and presents it, waits for the worker, and checks the
// runtime now shows exactly the rendered frame.
func (h *damageHarness) frame(x, y int, c Color) MenuPresentTrace {
	h.t.Helper()
	p := h.submit(x, y, c)
	h.r.mu.Lock()
	same := bytes.Equal(h.r.screen, h.d.Software.Framebuffer().Pix)
	h.r.mu.Unlock()
	if !same {
		h.t.Fatal("runtime does not show the rendered frame")
	}
	return p
}

func (h *damageHarness) submit(x, y int, c Color) MenuPresentTrace {
	h.t.Helper()
	h.mu.Lock()
	before := len(h.traces)
	h.mu.Unlock()
	h.d.FillRect(Rect{X: float32(x), Y: float32(y), W: 20, H: 10}, c)
	h.d.Present()
	// Wait for the worker's trace, not WaitIdle. A generation change finishes
	// without presenting, and WaitIdle reports that to the launch handoff.
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.mu.Lock()
		ready := len(h.traces) > before
		h.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("present did not finish: %v", h.d.LastError())
		}
		time.Sleep(time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.traces[len(h.traces)-1]
}

func TestMenuDisplayDamagedPresentsSkipStatus(t *testing.T) {
	h := newDamageHarness(t)
	first := h.frame(0, 0, RGB(9, 9, 9))
	if first.StatusSkipped || first.DamageRects != 0 {
		t.Fatalf("first frame %+v", first)
	}
	second := h.frame(101, 50, RGB(200, 0, 0))
	if !second.StatusSkipped || second.DamageRects != 1 || second.Retried {
		t.Fatalf("second frame %+v", second)
	}
	status, full, damaged, rejected := h.r.counts()
	if status != 1 || full != 1 || damaged != 1 || rejected != 0 {
		t.Fatalf("status=%d full=%d damaged=%d rejected=%d", status, full, damaged, rejected)
	}
	// 20x10 at x=101 widens to the 8-pixel groups [96,128).
	if h.r.staged != 32*10 {
		t.Fatalf("staged %d pixels", h.r.staged)
	}
	h.clock.Advance(menuProbeInterval)
	third := h.frame(300, 300, RGB(0, 200, 0))
	if third.StatusSkipped || third.DamageRects != 1 {
		t.Fatalf("status was not re-read after the probe interval: %+v", third)
	}
}

func TestMenuDisplayDamageFallsBackToCompleteFrame(t *testing.T) {
	h := newDamageHarness(t)
	h.frame(0, 0, RGB(9, 9, 9))
	// Another frame reached the screen: the base is stale.
	h.r.mu.Lock()
	h.r.seq++
	h.r.mu.Unlock()
	stale := h.frame(40, 40, RGB(1, 2, 3))
	if !stale.Retried || !stale.StatusSkipped || stale.DamageRects != 0 || !stale.Presented {
		t.Fatalf("stale base %+v", stale)
	}
	// A new generation while status is cached is found by the retry. As
	// without the cache, a frame queued for the old generation is dropped
	// without a wait and the next one goes whole to the new generation.
	h.r.mu.Lock()
	h.r.generation = 4
	h.r.mu.Unlock()
	moved := h.submit(80, 80, RGB(4, 5, 6))
	if !moved.Retried || !moved.StatusSkipped || moved.Presented {
		t.Fatalf("new generation %+v", moved)
	}
	redrawn := h.frame(80, 80, RGB(4, 5, 6))
	if redrawn.StatusSkipped || redrawn.DamageRects != 0 || !redrawn.Presented {
		t.Fatalf("redraw at the new generation %+v", redrawn)
	}
	if err := h.d.Pause(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.d.Resume()
	resumed := h.frame(120, 120, RGB(7, 8, 9))
	if resumed.StatusSkipped || resumed.DamageRects != 0 || resumed.Retried {
		t.Fatalf("resume kept a shortcut: %+v", resumed)
	}
	if h.d.LastError() != nil {
		t.Fatal(h.d.LastError())
	}
	if again := h.frame(160, 160, RGB(1, 1, 1)); !again.StatusSkipped || again.DamageRects != 1 {
		t.Fatalf("shortcuts did not return after a complete frame: %+v", again)
	}
}
