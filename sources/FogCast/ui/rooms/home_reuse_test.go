package rooms

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	lua "github.com/yuin/gopher-lua"
)

func TestHomeInitialPictureAddsOneFamilyPerStep(t *testing.T) {
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	previous := 0
	for step := 0; step < 20; step++ {
		frame := r.Step(time.Now())
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, hit := range frame.Hits {
			if strings.HasPrefix(hit.ID, "family:") {
				n++
			}
		}
		if n > previous+1 {
			t.Fatalf("one draw added %d family tiles; want at most one", n-previous)
		}
		if n == 4 {
			return
		}
		previous = n
	}
	t.Fatal("initial Home picture did not finish")
}

func TestHomeRestartsPartialPictureOnChanges(t *testing.T) {
	held := blockingLocalCores{fakeLocalCores: &fakeLocalCores{cores: []localcores.Core{{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true}}}, release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-held.release:
		default:
			close(held.release)
		}
	})
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{Local: held, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Now())
	r.Hover("family:2")
	r.Resize(800, 600)
	r.Step(time.Now())
	r.presentFrame(false)
	r.Step(time.Now())
	close(held.release)
	f := stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	if got := r.Destination().Label; got != "ZX Spectrum" {
		t.Fatalf("focus changed to %q", got)
	}
	hits := map[string]int{}
	for _, h := range f.Hits {
		if strings.HasPrefix(h.ID, "family:") {
			hits[h.ID]++
			if h.X+h.W > 800 {
				t.Fatalf("stale hit after resize: %+v", h)
			}
		}
	}
	if len(hits) != 4 {
		t.Fatalf("family hits = %v", hits)
	}
	for id, count := range hits {
		if count != 1 {
			t.Fatalf("duplicate hit %s: %d", id, count)
		}
	}
	same := r.Step(time.Now())
	if &f.Ops[0] != &same.Ops[0] {
		t.Fatal("completed picture was not reused")
	}
}

func TestHomeReusesFrameAndRedrawsChanges(t *testing.T) {
	for _, size := range [][2]int{{1280, 720}, {470, 350}, {300, 350}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			cores := []localcores.Core{
				{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
				{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true, Inputs: []string{"keyboard"}},
			}
			r := newRoom(t, examplePack(t, "example.fes-cores"), Options{Local: &fakeLocalCores{cores: cores}, Width: size[0], Height: size[1]})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			first := stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
			same := r.Step(time.Now())
			if len(first.Ops) == 0 || len(same.Ops) == 0 || &first.Ops[0] != &same.Ops[0] {
				t.Fatal("unchanged Home rebuilt its drawing")
			}
			// Pointer focus must invalidate the picture in the compact layout too.
			r.Hover("family:2")
			moved := r.Step(time.Now())
			if r.Destination().Label != "ZX Spectrum" || &same.Ops[0] == &moved.Ops[0] {
				t.Fatal("pointer focus did not redraw the selected family")
			}
			r.Input("header")
			menu := r.Step(time.Now())
			if r.Destination().Kind == KindFamily || &moved.Ops[0] == &menu.Ops[0] {
				t.Fatal("route navigation did not redraw")
			}
			r.Resize(800, 600)
			resized := r.Step(time.Now())
			if &menu.Ops[0] == &resized.Ops[0] {
				t.Fatal("resize reused old geometry")
			}
			if _, ok := resized.HitAt(0, 0); ok {
				t.Fatal("unexpected hit outside content")
			}
			// A discarded soft-overrun picture must not be mistaken for a presentation.
			r.presentFrame(false)
			retried := r.Step(time.Now())
			if &resized.Ops[0] == &retried.Ops[0] {
				t.Fatal("unpresented picture was reused")
			}
			r.Activate("menu:2")
			settings := r.Step(time.Now())
			if r.Destination().Kind != KindRoom || &retried.Ops[0] == &settings.Ops[0] {
				t.Fatal("pointer activation did not redraw Rooms")
			}
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHomeReplacesReusedCheckingFrame(t *testing.T) {
	held := blockingLocalCores{fakeLocalCores: &fakeLocalCores{cores: []localcores.Core{{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true}}}, release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-held.release:
		default:
			close(held.release)
		}
	})
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{Local: held, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	checking := stepUntil(t, r, func(f Frame) bool {
		for _, h := range f.Hits {
			if h.ID == "family:4" {
				return true
			}
		}
		return false
	})
	same := r.Step(time.Now())
	if &checking.Ops[0] != &same.Ops[0] {
		t.Fatal("unchanged Checking frame was rebuilt")
	}
	close(held.release)
	loaded := stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	if &same.Ops[0] == &loaded.Ops[0] {
		t.Fatal("core-list arrival reused Checking frame")
	}
}

func TestHomeReusesCompletedSlowPaint(t *testing.T) {
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{Width: 1280, Height: 720, Budget: Budget{Frame: 100 * time.Millisecond}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	slow := true
	r.L.SetField(r.L.GetGlobal("gfx"), "paint", r.L.NewFunction(func(L *lua.LState) int {
		n := r.gfxPaint(L)
		if slow {
			slow = false
			time.Sleep(150 * time.Millisecond)
		}
		return n
	}))
	first := r.Step(time.Now())
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if !r.frameReady || r.frameOverruns != 1 || len(first.Ops) == 0 {
		t.Fatal("completed slow paint was not presented as one soft overrun")
	}
	completed := stepUntil(t, r, func(f Frame) bool {
		for _, h := range f.Hits {
			if h.ID == "family:4" {
				return true
			}
		}
		return false
	})
	next := r.Step(time.Now())
	if r.frameOverruns != 0 || &completed.Ops[0] != &next.Ops[0] {
		t.Fatal("completed Home did not reuse its picture")
	}
}
