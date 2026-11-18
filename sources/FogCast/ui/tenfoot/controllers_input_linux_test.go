package tenfoot

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

func TestProfileEditReachesAnAttachedPadOnceItsButtonsAreUp(t *testing.T) {
	app := controllersApp(t)
	in := &nativeInput{fd: 7, kind: InputGamepad, player: 0, info: PadInfo{GUID: testPadGUID}}
	ins := &nativeInputs{devices: []*nativeInput{in}, padRemap: app.padRemap}
	press := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonA}

	// Swap A / B from the page while the pad still holds the button that
	// chose it.
	now := time.Now()
	for _, cmd := range []Command{CmdDown, CmdDown, CmdDown, CmdLeft} {
		app.HandleCommand(cmd, now)
	}
	in.setButton(uint16(ButtonSouth), CmdSelect, true)
	app.HandleCommand(CmdSelect, now)
	ins.syncProfiles(app)
	if in.remap != nil {
		t.Fatal("the profile changed under a held button")
	}
	in.setButton(uint16(ButtonSouth), CmdSelect, false)
	ins.syncProfiles(app)
	if in.remap == nil || in.remap.Apply(press).Code != remoteinput.ButtonB {
		t.Fatal("the saved profile did not reach the pad")
	}

	// Reset takes it off again.
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	ins.syncProfiles(app)
	if in.remap != nil {
		t.Fatal("reset left the pad remapped")
	}
}

// A profile may send one control from several buttons, including the d-pad.
// The control stays held until the last of them comes up.
func TestButtonsSharingAControlHoldItUntilTheLastIsUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	prefs := tenfootPrefs{Controllers: map[string]inputmap.Profile{
		testPadGUID: {Bindings: map[string]string{"x": "a", "dpad-up": "a"}},
	}}
	if err := saveTenfootPrefs(path, prefs); err != nil {
		t.Fatal(err)
	}
	feed := &localPadRecorder{}
	app := runningLocalCoreApp(feed)
	app.SetPrefsPath(path)
	remap, err := app.padRemap(testPadGUID)
	if err != nil || remap == nil {
		t.Fatalf("profile sharing a control was refused: %v", err)
	}
	in := &nativeInput{fd: 7, player: 0, info: PadInfo{GUID: testPadGUID}, remap: remap}
	now := time.Now()
	want := func(step string, events ...remoteinput.Action) {
		t.Helper()
		if len(feed.events) != len(events) {
			t.Fatalf("%s: events = %+v, want %d", step, feed.events, len(events))
		}
		for i, action := range events {
			if e := feed.events[i]; e.Code != remoteinput.ButtonA || e.Action != action {
				t.Fatalf("%s: event %d = %+v", step, i, e)
			}
		}
		_, held := app.playPadHeld[padKey{0, remoteinput.ButtonA}]
		if want := len(events)%2 == 1; held != want {
			t.Fatalf("%s: A held = %v, want %v", step, held, want)
		}
	}
	in.button(app, ButtonWest, 1, now) // X sends A.
	want("X down", remoteinput.ActionPress)
	in.button(app, ButtonSouth, 1, now)
	in.localHat(app, 17, -1, now) // Up sends A.
	want("A and up down", remoteinput.ActionPress)
	in.button(app, ButtonWest, 0, now)
	in.button(app, ButtonSouth, 0, now)
	want("X and A up, up still held", remoteinput.ActionPress)
	in.localHat(app, 17, 0, now)
	want("all up", remoteinput.ActionPress, remoteinput.ActionRelease)

	// A port release lets everything go, so nothing is left counted.
	in.button(app, ButtonWest, 1, now)
	in.button(app, ButtonSouth, 1, now)
	app.mu.Lock()
	releasePortLocked(app, in)
	app.mu.Unlock()
	if len(in.padSent) != 0 || len(in.padHeld) != 0 {
		t.Fatalf("port release left holds: sent=%v held=%v", in.padSent, in.padHeld)
	}
}

// A release comes up as what the button pressed, even if the pad's profile
// changed while it was held.
func TestPadReleaseFollowsWhatTheButtonPressed(t *testing.T) {
	feed := &localPadRecorder{}
	app := runningLocalCoreApp(feed)
	swap, err := inputmap.NewRemapper(inputmap.Profile{Preset: "swap-ab"})
	if err != nil {
		t.Fatal(err)
	}
	in := &nativeInput{fd: 7, player: 0, remap: swap}
	now := time.Now()
	in.button(app, ButtonSouth, 1, now)
	in.remap = nil
	in.button(app, ButtonSouth, 0, now)
	if len(feed.events) != 2 || feed.events[0].Code != remoteinput.ButtonB || feed.events[1].Code != remoteinput.ButtonB ||
		feed.events[1].Action != remoteinput.ActionRelease {
		t.Fatalf("events = %+v, want B down then B up", feed.events)
	}
}
