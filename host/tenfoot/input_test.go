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
