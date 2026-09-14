package tenfoot

import (
	"strings"
	"testing"
	"time"
)

func TestAffinityTrackerLastUsedAndHotplug(t *testing.T) {
	t.Parallel()
	var tr affinityTracker
	tr.Seed(InputKeyboard, 1)
	tr.Seed(InputMouse, 2)
	tr.preferStartup()
	if tr.current != (Affinity{Kind: InputKeyboard, ID: 1}) {
		t.Fatalf("startup = %#v", tr.current)
	}

	tr.Note(InputMouse, 2)
	if tr.current.Kind != InputMouse {
		t.Fatalf("last-used mouse = %#v", tr.current)
	}
	tr.Note(InputKeyboard, 1)
	if tr.current.Kind != InputKeyboard {
		t.Fatalf("last-used keyboard = %#v", tr.current)
	}

	tr.Attach(InputMouse, 3)
	if tr.current != (Affinity{Kind: InputMouse, ID: 3}) {
		t.Fatalf("newly plugged mouse = %#v", tr.current)
	}
	tr.Detach(InputMouse, 3)
	if tr.current != (Affinity{Kind: InputKeyboard, ID: 1}) {
		t.Fatalf("unplug mouse restored keyboard = %#v", tr.current)
	}
}

func TestAffinityTrackerUnplugReturnsRemaining(t *testing.T) {
	t.Parallel()
	var tr affinityTracker
	tr.Seed(InputKeyboard, 1)
	tr.Attach(InputGamepad, 9)
	if tr.current.Kind != InputGamepad {
		t.Fatalf("plug gamepad = %#v", tr.current)
	}
	tr.Detach(InputGamepad, 9)
	if tr.current != (Affinity{Kind: InputKeyboard, ID: 1}) {
		t.Fatalf("unplug gamepad = %#v", tr.current)
	}

	tr.Note(InputKeyboard, 1)
	tr.Attach(InputMouse, 4)
	tr.Detach(InputKeyboard, 1)
	if tr.current != (Affinity{Kind: InputMouse, ID: 4}) {
		t.Fatalf("unplug keyboard with mouse remaining = %#v", tr.current)
	}
	tr.Detach(InputMouse, 4)
	if tr.current.Kind != InputNone {
		t.Fatalf("unplug last device = %#v", tr.current)
	}
}

func TestAffinityTrackerSeedDoesNotClaim(t *testing.T) {
	t.Parallel()
	var tr affinityTracker
	tr.Seed(InputMouse, 2)
	tr.Seed(InputKeyboard, 1)
	if tr.current.Kind != InputNone {
		t.Fatalf("seed claimed %#v", tr.current)
	}
	tr.preferStartup()
	if tr.current.Kind != InputKeyboard {
		t.Fatalf("prefer keyboard without pad = %#v", tr.current)
	}

	var pads affinityTracker
	pads.Seed(InputKeyboard, 1)
	pads.Seed(InputGamepad, 8)
	pads.preferStartup()
	if pads.current.Kind != InputGamepad {
		t.Fatalf("prefer pad when present = %#v", pads.current)
	}
}

func TestAffinityLastUsedSwitchesHints(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(8)
	if got := app.Snapshot().HeaderHint(); !strings.Contains(got, "LB/RB") {
		t.Fatalf("default hint = %q", got)
	}
	if app.Snapshot().AffinityBadge() != "KB" {
		t.Fatalf("default badge = %q", app.Snapshot().AffinityBadge())
	}

	app.NoteInput(InputKeyboard, 1)
	snap := app.Snapshot()
	if snap.Affinity != InputKeyboard || snap.AffinityID != 1 {
		t.Fatalf("keyboard affinity = %s/%d", snap.Affinity, snap.AffinityID)
	}
	if !strings.Contains(snap.HeaderHint(), "Enter launch") || strings.Contains(snap.HeaderHint(), "LB/RB") {
		t.Fatalf("keyboard hint = %q", snap.HeaderHint())
	}
	if snap.AffinityBadge() != "KB" {
		t.Fatalf("keyboard badge = %q", snap.AffinityBadge())
	}

	now := time.Now()
	x, y := cellCenter(t, app, 2)
	app.PointerMoveFrom(7, x, y, now)
	snap = app.Snapshot()
	if snap.Affinity != InputMouse || snap.AffinityID != 7 {
		t.Fatalf("mouse affinity = %s/%d", snap.Affinity, snap.AffinityID)
	}
	if snap.Grid.Focus != 2 {
		t.Fatalf("mouse focus = %d", snap.Grid.Focus)
	}
	if snap.HeaderHint() != "move focus  click launch" {
		t.Fatalf("mouse hint = %q", snap.HeaderHint())
	}
	if snap.AffinityBadge() != "MOUSE" {
		t.Fatalf("mouse badge = %q", snap.AffinityBadge())
	}

	app.NoteInput(InputGamepad, 9)
	snap = app.Snapshot()
	if snap.Affinity != InputGamepad {
		t.Fatalf("gamepad affinity = %s", snap.Affinity)
	}
	if !strings.Contains(snap.HeaderHint(), "LB/RB") {
		t.Fatalf("gamepad hint = %q", snap.HeaderHint())
	}
	if snap.AffinityBadge() != "PAD 1" {
		t.Fatalf("gamepad badge = %q", snap.AffinityBadge())
	}
}

func TestAffinityHotplugClaimsWithoutMovingFocus(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(8)
	now := time.Now()
	app.NoteInput(InputKeyboard, 1)
	if app.Press(CmdRight, now) != CmdRight {
		t.Fatal("keyboard right")
	}
	focus := app.Snapshot().Grid.Focus
	if focus != 1 {
		t.Fatalf("focus = %d", focus)
	}

	app.AttachInput(InputMouse, 4)
	snap := app.Snapshot()
	if snap.Affinity != InputMouse || snap.AffinityID != 4 {
		t.Fatalf("plug mouse = %s/%d", snap.Affinity, snap.AffinityID)
	}
	if snap.Grid.Focus != focus {
		t.Fatalf("plug moved focus %d -> %d", focus, snap.Grid.Focus)
	}
	if snap.HeaderHint() != "move focus  click launch" {
		t.Fatalf("plug hint = %q", snap.HeaderHint())
	}

	app.DetachInput(InputMouse, 4)
	snap = app.Snapshot()
	if snap.Affinity != InputKeyboard || snap.AffinityID != 1 {
		t.Fatalf("unplug = %s/%d", snap.Affinity, snap.AffinityID)
	}
	if snap.Grid.Focus != focus {
		t.Fatalf("unplug moved focus %d -> %d", focus, snap.Grid.Focus)
	}
	if !strings.Contains(snap.HeaderHint(), "Enter launch") {
		t.Fatalf("restored hint = %q", snap.HeaderHint())
	}
}

func TestAffinityUnplugReturnsRemainingDevice(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(4)
	app.SeedInput(InputKeyboard, 1)
	app.SeedInput(InputGamepad, 9)
	app.FinishInputSeed()
	if got := app.Affinity(); got.Kind != InputGamepad {
		t.Fatalf("startup pad = %#v", got)
	}
	app.AttachInput(InputMouse, 2)
	if got := app.Affinity(); got.Kind != InputMouse {
		t.Fatalf("plug mouse = %#v", got)
	}
	app.DetachInput(InputMouse, 2)
	if got := app.Affinity(); got.Kind != InputGamepad || got.ID != 9 {
		t.Fatalf("unplug restored pad = %#v", got)
	}
	app.DetachInput(InputGamepad, 9)
	if got := app.Affinity(); got.Kind != InputKeyboard || got.ID != 1 {
		t.Fatalf("unplug pad restored keyboard = %#v", got)
	}
}

func TestAffinityOverlayHintsFollowDevice(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(6)
	app.detailOpen = true
	app.NoteInput(InputKeyboard, 1)
	if got := app.Snapshot().Detail.Hint; !strings.Contains(got, "Enter launch") {
		t.Fatalf("detail keyboard = %q", got)
	}
	app.NoteInput(InputMouse, 2)
	if got := app.Snapshot().Detail.Hint; got != "click launch  click empty back" {
		t.Fatalf("detail mouse = %q", got)
	}
	app.NoteInput(InputGamepad, 3)
	if got := app.Snapshot().Detail.Hint; !strings.Contains(got, "A launch") {
		t.Fatalf("detail gamepad = %q", got)
	}

	app.detailOpen = false
	app.searchOpen = true
	app.NoteInput(InputKeyboard, 1)
	if got := app.Snapshot().OSK.Hint; !strings.Contains(got, "Enter done") {
		t.Fatalf("osk keyboard = %q", got)
	}
	app.NoteInput(InputMouse, 2)
	if got := app.Snapshot().OSK.Hint; got != "click type  click empty close" {
		t.Fatalf("osk mouse = %q", got)
	}
}

func TestAffinityPlaySessionMouseDoesNotSteal(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(6)
	now := time.Now()
	app.NoteInput(InputKeyboard, 1)
	app.session = SessionResult{
		State:        "active",
		CoreKeyboard: true,
		Input:        &SessionInput{State: "attached", Ready: true},
	}
	x, y := cellCenter(t, app, 2)
	app.PointerMoveFrom(7, x, y, now)
	if got := app.Affinity(); got.Kind != InputKeyboard || got.ID != 1 {
		t.Fatalf("play-session mouse stole %#v", got)
	}
	if got := app.Snapshot().Grid.Focus; got != 0 {
		t.Fatalf("play-session mouse stole focus %d", got)
	}
}

func TestHeaderHintKeepsGamepadCopyWithoutAffinity(t *testing.T) {
	t.Parallel()
	snap := Snapshot{}
	if got := snap.HeaderHint(); got != browseHint(InputNone) {
		t.Fatalf("hint = %q", got)
	}
	if snap.AffinityBadge() != "KB" {
		t.Fatalf("badge = %q", snap.AffinityBadge())
	}
	snap.Gamepads = 2
	if snap.AffinityBadge() != "PAD 2" {
		t.Fatalf("pad badge = %q", snap.AffinityBadge())
	}
}
