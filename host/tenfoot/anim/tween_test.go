package anim

import (
	"math"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

func TestClampAndEase(t *testing.T) {
	if Clamp01(-1) != 0 || Clamp01(2) != 1 || Clamp01(0.25) != 0.25 {
		t.Fatal("clamp")
	}
	if EaseLinear(0.25) != 0.25 {
		t.Fatal("linear")
	}
	if EaseInOut(0) != 0 || EaseInOut(1) != 1 {
		t.Fatal("ease ends")
	}
	if math.Abs(EaseInOut(0.5)-0.5) > 1e-9 {
		t.Fatal("ease mid")
	}
	if EaseInOut(0.25) >= 0.25 {
		t.Fatalf("ease-in should be below linear at 0.25, got %v", EaseInOut(0.25))
	}
}

func TestLerpAndMove(t *testing.T) {
	if Lerp(10, 20, 0) != 10 || Lerp(10, 20, 1) != 20 || Lerp(10, 20, 0.5) != 15 {
		t.Fatal("lerp")
	}
	c := LerpColor(gfx.RGB(0, 0, 0), gfx.RGB(10, 20, 30), 0.5)
	if c != gfx.RGB(5, 10, 15) {
		t.Fatalf("lerp color %+v", c)
	}
	from := gfx.Rect{X: 0, Y: 10, W: 8, H: 8}
	to := gfx.Rect{X: 20, Y: 10, W: 8, H: 8}
	mid := Move(from, to, 0.5)
	if mid.X != 10 || mid.Y != 10 || mid.W != 8 {
		t.Fatalf("move %+v", mid)
	}
}

func TestTweenProgress(t *testing.T) {
	tw := Tween{Duration: time.Second, Ease: EaseLinear}
	if tw.Progress(0) != 0 || tw.Progress(500*time.Millisecond) != 0.5 || tw.Progress(2*time.Second) != 1 {
		t.Fatal("linear tween")
	}
	if (Tween{Duration: 0}.Progress(0) != 1) {
		t.Fatal("zero duration")
	}
	eased := Tween{Duration: time.Second, Ease: EaseInOut}
	if eased.Progress(250*time.Millisecond) >= 0.25 {
		t.Fatal("eased tween should lag linear at 0.25")
	}
}

func TestCyclePhase(t *testing.T) {
	idx, fade, move := CyclePhase(0, 2)
	if idx != 0 || fade != 0 || move != 0 {
		t.Fatalf("t0 idx=%d fade=%v move=%v", idx, fade, move)
	}
	idx, fade, _ = CyclePhase(StillHold, 2)
	if idx != 0 || fade != 0 {
		t.Fatalf("hold idx=%d fade=%v", idx, fade)
	}
	idx, fade, _ = CyclePhase(StillHold+StillFade/2, 2)
	if idx != 0 || math.Abs(fade-0.5) > 1e-9 {
		t.Fatalf("mid-fade idx=%d fade=%v", idx, fade)
	}
	idx, fade, _ = CyclePhase(StillHold+StillFade, 2)
	if idx != 1 || fade != 0 {
		t.Fatalf("next idx=%d fade=%v", idx, fade)
	}
	_, _, move = CyclePhase(500*time.Millisecond, 2)
	if math.Abs(move-0.5) > 1e-9 {
		t.Fatalf("move at 0.5s %v", move)
	}
	_, _, move = CyclePhase(time.Second, 2)
	if math.Abs(move-1) > 1e-9 {
		t.Fatalf("move at 1s %v", move)
	}
	_, _, move = CyclePhase(2*time.Second, 2)
	if math.Abs(move) > 1e-9 {
		t.Fatalf("move at 2s %v", move)
	}
}
