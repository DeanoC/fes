package tenfoot

import (
	"strconv"
	"testing"
	"time"
)

func TestCommandFromGamepadButtons(t *testing.T) {
	t.Parallel()
	cases := map[Button]Command{
		ButtonDPadUp:        CmdUp,
		ButtonDPadDown:      CmdDown,
		ButtonDPadLeft:      CmdLeft,
		ButtonDPadRight:     CmdRight,
		ButtonSouth:         CmdSelect,
		ButtonEast:          CmdBack,
		ButtonStart:         CmdQuit,
		ButtonBack:          CmdBack,
		ButtonWest:          CmdSortCycle,
		ButtonNorth:         CmdSearch,
		ButtonLeftShoulder:  CmdFilterPrev,
		ButtonRightShoulder: CmdFilterNext,
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
	if CommandFromKey("[") != CmdFilterPrev || CommandFromKey("]") != CmdFilterNext || CommandFromKey("x") != CmdSortCycle || CommandFromKey("/") != CmdSearch {
		t.Fatal("browse keyboard mapping")
	}
	if CommandFromKey("c") != CmdViewNext || CommandFromKey("v") != CmdFavorite || CommandFromKey("*") != CmdFavorite {
		t.Fatal("collection keyboard mapping")
	}
	if CommandFromStick(20000, 0) != CmdRight || CommandFromStick(0, -20000) != CmdUp {
		t.Fatal("stick mapping")
	}
	if CommandFromStick(100, 100) != CmdNone {
		t.Fatal("stick deadzone")
	}
	// Near-diagonal noise must not flip a latched axis.
	if CommandFromStickHeld(20000, 19900, CmdRight) != CmdRight {
		t.Fatal("stick hysteresis hold")
	}
	if CommandFromStickHeld(19900, -20000, CmdUp) != CmdUp {
		t.Fatal("stick hysteresis vertical hold")
	}
	if CommandFromStickHeld(20000, 20000+stickHysteresis, CmdRight) != CmdDown {
		t.Fatal("stick hysteresis switch")
	}
	if CommandFromStickHeld(100, 100, CmdRight) != CmdNone {
		t.Fatal("stick recenter")
	}
	// Latched axis below stickGate must not hand focus to the other axis.
	if CommandFromStickHeld(12000, 17000, CmdRight) != CmdRight {
		t.Fatal("stick latch holds right below gate")
	}
	if CommandFromStickHeld(-12000, 17000, CmdLeft) != CmdLeft {
		t.Fatal("stick latch holds left below gate")
	}
	if CommandFromStickHeld(17000, 12000, CmdDown) != CmdDown {
		t.Fatal("stick latch holds down below gate")
	}
	if CommandFromStickHeld(17000, -12000, CmdUp) != CmdUp {
		t.Fatal("stick latch holds up below gate")
	}
	if CommandFromStickHeld(12000, 12000+stickHysteresis, CmdRight) != CmdDown {
		t.Fatal("stick latch still switches on hysteresis")
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

	// Hold Right, also Down, release Down: Right is re-armed without an extra step.
	app := catalogApp(20)
	held := map[Command]bool{}
	if applyPressed(app, map[Command]bool{CmdRight: true}, held, now) {
		t.Fatal("quit")
	}
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdRight: true, CmdDown: true}, held, now) {
		t.Fatal("quit")
	}
	focusAfterBoth := app.Snapshot().Grid.Focus
	if focusAfterBoth <= 1 {
		t.Fatalf("expected right+down focus > 1, got %d", focusAfterBoth)
	}
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdRight: true}, held, now) {
		t.Fatal("quit")
	}
	if app.repeat.held != CmdRight {
		t.Fatalf("after release down: held = %s want right", app.repeat.held)
	}
	if got := app.Snapshot().Grid.Focus; got != focusAfterBoth {
		t.Fatalf("re-arm moved focus %d -> %d", focusAfterBoth, got)
	}
	if got := app.Tick(now.Add(repeatDelay + time.Millisecond)); got != CmdRight {
		t.Fatalf("right repeat = %s", got)
	}

	// Hold Right, also Down, release Right: Down is still held and must keep repeating.
	app = catalogApp(20)
	held = map[Command]bool{}
	now = time.Unix(0, 0)
	if applyPressed(app, map[Command]bool{CmdRight: true}, held, now) {
		t.Fatal("quit")
	}
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdRight: true, CmdDown: true}, held, now) {
		t.Fatal("quit")
	}
	focusAfterBoth = app.Snapshot().Grid.Focus
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdDown: true}, held, now) {
		t.Fatal("quit")
	}
	if app.repeat.held != CmdDown {
		t.Fatalf("after release right: held = %s want down", app.repeat.held)
	}
	if got := app.Snapshot().Grid.Focus; got != focusAfterBoth {
		t.Fatalf("re-arm moved focus %d -> %d", focusAfterBoth, got)
	}
	if got := app.Tick(now.Add(repeatDelay + time.Millisecond)); got != CmdDown {
		t.Fatalf("down repeat = %s", got)
	}
}

func TestApplyPressedSelectWhileHeldDoesNotWalkFocus(t *testing.T) {
	t.Parallel()
	app := catalogApp(20)
	held := map[Command]bool{}
	now := time.Unix(0, 0)
	if applyPressed(app, map[Command]bool{CmdRight: true}, held, now) {
		t.Fatal("quit")
	}
	focus := app.Snapshot().Grid.Focus
	if focus != 1 {
		t.Fatalf("focus after right = %d", focus)
	}
	now = now.Add(10 * time.Millisecond)
	if applyPressed(app, map[Command]bool{CmdRight: true, CmdSelect: true}, held, now) {
		t.Fatal("quit")
	}
	if got := app.Snapshot().Grid.Focus; got != focus {
		t.Fatalf("select while held moved focus %d -> %d", focus, got)
	}
	if app.repeat.held != CmdRight {
		t.Fatalf("held after select = %s want right", app.repeat.held)
	}
	if got := app.Tick(now.Add(50 * time.Millisecond)); got != CmdNone {
		t.Fatalf("select must not restart repeat immediately: %s", got)
	}
	if got := app.Tick(time.Unix(0, 0).Add(repeatDelay + time.Millisecond)); got != CmdRight {
		t.Fatalf("select restarted repeat delay: %s", got)
	}
}

func TestHoldGateSelectShortAndLongPress(t *testing.T) {
	t.Parallel()
	app := catalogApp(5)
	held := map[Command]bool{}
	now := time.Unix(0, 0)
	if applyPressed(app, map[Command]bool{CmdSelect: true}, held, now) {
		t.Fatal("quit")
	}
	if app.Snapshot().ViewPicker {
		t.Fatal("picker opened on down")
	}
	if app.Snapshot().Launch.Phase != "idle" {
		t.Fatalf("select fired on down: %#v", app.Snapshot().Launch)
	}
	now = now.Add(40 * time.Millisecond)
	if applyPressed(app, map[Command]bool{}, held, now) {
		t.Fatal("quit")
	}
	if app.Snapshot().Launch.Phase != "error" {
		t.Fatalf("short select should confirm, launch = %#v", app.Snapshot().Launch)
	}

	app = catalogApp(5)
	held = map[Command]bool{}
	now = time.Unix(0, 0)
	if applyPressed(app, map[Command]bool{CmdSelect: true}, held, now) {
		t.Fatal("quit")
	}
	if got := app.Tick(now.Add(longPressMin + time.Millisecond)); got != CmdViewPicker {
		t.Fatalf("long = %s", got)
	}
	if !app.Snapshot().ViewPicker {
		t.Fatal("picker should open")
	}
	if applyPressed(app, map[Command]bool{}, held, now.Add(longPressMin+2*time.Millisecond)) {
		t.Fatal("quit")
	}
	if app.Snapshot().Launch.Phase != "idle" {
		t.Fatalf("long select launched: %#v", app.Snapshot().Launch)
	}
	if !app.Snapshot().ViewPicker {
		t.Fatal("release should keep picker open")
	}
}

func TestHoldGateNorthLongPressFavorites(t *testing.T) {
	t.Parallel()
	var g HoldGate
	now := time.Unix(0, 0)
	if !g.Begin(CmdSearch, now, true) {
		t.Fatal("begin")
	}
	if got := g.Tick(now.Add(100 * time.Millisecond)); got != CmdNone {
		t.Fatalf("early = %s", got)
	}
	if got := g.Tick(now.Add(longPressMin + time.Millisecond)); got != CmdFavorite {
		t.Fatalf("long = %s", got)
	}
	if got := g.Release(CmdSearch); got != CmdNone {
		t.Fatalf("release after long = %s", got)
	}
	if !g.Begin(CmdSearch, now, true) {
		t.Fatal("begin short")
	}
	if got := g.Release(CmdSearch); got != CmdSearch {
		t.Fatalf("short release = %s", got)
	}
}

func catalogApp(n int) *App {
	app := NewApp(nil, 1280, 720, n)
	app.games = make([]Game, n)
	for i := 0; i < n; i++ {
		app.games[i] = Game{ID: "g" + strconv.Itoa(i), Title: "Game"}
	}
	app.grid.SetCount(n)
	return app
}
