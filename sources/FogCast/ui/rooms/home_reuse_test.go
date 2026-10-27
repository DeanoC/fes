package rooms

import (
	"fmt"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	lua "github.com/yuin/gopher-lua"
)

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
			first := stepUntil(t, r, func(Frame) bool { return r.Destination().Kind == KindCore })
			same := r.Step(time.Now())
			if len(first.Ops) == 0 || len(same.Ops) == 0 || &first.Ops[0] != &same.Ops[0] {
				t.Fatal("unchanged Home rebuilt its drawing")
			}
			// Pointer focus must invalidate the picture even in the stacked layout.
			r.Hover("core:2")
			moved := r.Step(time.Now())
			if r.Destination().PackageID != coreZXID || &same.Ops[0] == &moved.Ops[0] {
				t.Fatal("pointer focus did not redraw the selected core")
			}
			for i := 0; i < 2 && r.Destination().Kind == KindCore; i++ {
				r.Input("up")
			}
			menu := r.Step(time.Now())
			if r.Destination().Kind == KindCore || &moved.Ops[0] == &menu.Ops[0] {
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
	checking := r.Step(time.Now())
	same := r.Step(time.Now())
	if &checking.Ops[0] != &same.Ops[0] {
		t.Fatal("unchanged Checking frame was rebuilt")
	}
	close(held.release)
	loaded := stepUntil(t, r, func(Frame) bool { return r.Destination().Kind == KindCore })
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
	next := r.Step(time.Now())
	if r.frameOverruns != 0 || &first.Ops[0] != &next.Ops[0] {
		t.Fatal("unchanged Home repeated its slow paint instead of reusing it")
	}
}
