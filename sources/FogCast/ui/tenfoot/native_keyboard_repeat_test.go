package tenfoot

import (
	"testing"
	"time"
)

func repeatID(n byte) (id [16]byte) { id[0] = n; return }

func TestNativeRepeatSingleArbiterAndSourceRetirement(t *testing.T) {
	var r Repeater
	start := time.Unix(100, 0)
	if got := r.Down(CmdLeft, start); got != CmdLeft {
		t.Fatal(got)
	}
	if got, err := r.DownNative(repeatID(1), 30, CmdRight, start.Add(time.Millisecond)); err != nil || got != CmdRight {
		t.Fatalf("native down %v %v", got, err)
	}
	if r.Held() != CmdRight || r.LegacyHeld() != CmdLeft {
		t.Fatal("new native source did not win")
	}
	if got := r.Tick(start.Add(repeatDelay + 2*time.Millisecond)); got != CmdRight {
		t.Fatalf("repeat %v", got)
	}
	r.RetireNative(repeatID(1))
	if r.Held() != CmdLeft {
		t.Fatal("surviving legacy source did not resume")
	}
	// Retirement itself emits no click; a surviving held source can repeat on
	// a later Tick using its original hold timing.
	if got := r.Tick(start.Add(repeatDelay + 3*time.Millisecond)); got != CmdLeft {
		t.Fatalf("legacy repeat %v", got)
	}
}

func TestNativeRepeatSameCommandAndTwoKeyboards(t *testing.T) {
	var r Repeater
	now := time.Unix(100, 0)
	r.Down(CmdLeft, now)
	_, _ = r.DownNative(repeatID(1), 30, CmdLeft, now.Add(time.Millisecond))
	_, _ = r.DownNative(repeatID(2), 31, CmdRight, now.Add(2*time.Millisecond))
	r.RetireNative(repeatID(1))
	if r.Held() != CmdRight {
		t.Fatal("removing one keyboard changed the other")
	}
	r.UpNative(repeatID(2), 31)
	if r.Held() != CmdLeft {
		t.Fatal("legacy same-direction contributor was lost")
	}
	r.Up(CmdLeft)
	if r.Held() != CmdNone {
		t.Fatal("released repeat remained")
	}
}

func TestNativeRepeatClearAndOverflowFailClosed(t *testing.T) {
	var r Repeater
	now := time.Unix(100, 0)
	_, _ = r.DownNative(repeatID(1), 30, CmdLeft, now)
	r.Clear()
	if r.Held() != CmdNone || r.Tick(now.Add(time.Second)) != CmdNone {
		t.Fatal("Clear resurrected a native hold")
	}
	r.nextOrder = ^uint64(0)
	if _, err := r.DownNative(repeatID(2), 31, CmdRight, now); err == nil {
		t.Fatal("wrapped native order accepted")
	}
	if got := r.Down(CmdLeft, now); got != CmdLeft || r.Held() != CmdLeft {
		t.Fatal("overflow broke ordinary input")
	}
	if _, err := r.DownNative(repeatID(2), 31, CmdSelect, now); err == nil {
		t.Fatal("exhausted native route accepted a nonmovement command")
	}
}

func TestNativeRepeatDoesNotOverridePadRearmOrHoldGate(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	defer a.Stop()
	now := time.Unix(100, 0)
	a.repeat.Down(CmdLeft, now)
	_, _ = a.repeat.DownNative(repeatID(1), 30, CmdRight, now.Add(time.Millisecond))
	pressed, held := map[Command]bool{CmdLeft: true}, map[Command]bool{CmdLeft: true}
	applyPressed(a, pressed, held, now.Add(2*time.Millisecond))
	if a.repeat.Held() != CmdRight {
		t.Fatal("pad rearm overrode newer native source")
	}
	if !a.hold.Begin(CmdSelect, now, true) {
		t.Fatal("hold gate did not arm")
	}
	a.repeat.RetireNative(repeatID(1))
	if got := a.hold.Tick(now.Add(longPressMin)); got != CmdViewPicker {
		t.Fatalf("native retirement disturbed pad hold: %v", got)
	}
}
