package tenfoot

import (
	"testing"
	"time"
)

func TestCommandFromGamepadButtons(t *testing.T) {
	t.Parallel()
	cases := map[Button]Command{
		ButtonDPadUp:    CmdUp,
		ButtonDPadDown:  CmdDown,
		ButtonDPadLeft:  CmdLeft,
		ButtonDPadRight: CmdRight,
		ButtonSouth:     CmdSelect,
		ButtonEast:      CmdBack,
		ButtonStart:     CmdQuit,
		ButtonBack:      CmdBack,
	}
	for button, want := range cases {
		if got := CommandFromButton(button); got != want {
			t.Fatalf("button %d = %s want %s", button, got, want)
		}
	}
}

func TestCommandFromKeyAndStick(t *testing.T) {
	t.Parallel()
	if CommandFromKey("right") != CmdRight || CommandFromKey("return") != CmdSelect {
		t.Fatal("keyboard mapping")
	}
	if CommandFromStick(20000, 0) != CmdRight || CommandFromStick(0, -20000) != CmdUp {
		t.Fatal("stick mapping")
	}
	if CommandFromStick(100, 100) != CmdNone {
		t.Fatal("stick deadzone")
	}
}

func TestRepeaterFiresAfterDelay(t *testing.T) {
	t.Parallel()
	var r Repeater
	now := time.Unix(0, 0)
	if got := r.Down(CmdRight, now); got != CmdRight {
		t.Fatalf("down = %s", got)
	}
	if got := r.Tick(now.Add(100 * time.Millisecond)); got != CmdNone {
		t.Fatalf("early tick = %s", got)
	}
	if got := r.Tick(now.Add(repeatDelay + time.Millisecond)); got != CmdRight {
		t.Fatalf("repeat = %s", got)
	}
	r.Up(CmdRight)
	if got := r.Tick(now.Add(2 * time.Second)); got != CmdNone {
		t.Fatalf("after up = %s", got)
	}
}

func TestApplyPressedRearmsRemainingDirection(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)

	// Hold Left, also Up, release Up: Left must be re-armed so repeat continues.
	app := NewApp(nil, 1280, 720, 8)
	held := map[Command]bool{}
	if applyPressed(app, map[Command]bool{CmdLeft: true}, held, now) {
		t.Fatal("quit")
	}
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdLeft: true, CmdUp: true}, held, now) {
		t.Fatal("quit")
	}
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdLeft: true}, held, now) {
		t.Fatal("quit")
	}
	if app.repeat.held != CmdLeft {
		t.Fatalf("after release up: held = %s want left", app.repeat.held)
	}
	if got := app.Tick(now.Add(repeatDelay + time.Millisecond)); got != CmdLeft {
		t.Fatalf("left repeat = %s", got)
	}

	// Hold Left, also Up, release Left: Up is still held and must keep repeating.
	app = NewApp(nil, 1280, 720, 8)
	held = map[Command]bool{}
	now = time.Unix(0, 0)
	if applyPressed(app, map[Command]bool{CmdLeft: true}, held, now) {
		t.Fatal("quit")
	}
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdLeft: true, CmdUp: true}, held, now) {
		t.Fatal("quit")
	}
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdUp: true}, held, now) {
		t.Fatal("quit")
	}
	if app.repeat.held != CmdUp {
		t.Fatalf("after release left: held = %s want up", app.repeat.held)
	}
	if got := app.Tick(now.Add(repeatDelay + time.Millisecond)); got != CmdUp {
		t.Fatalf("up repeat = %s", got)
	}
}
