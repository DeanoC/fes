package tenfoot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/controller"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

func TestProfileEditReachesAnAttachedPadOnceItsButtonsAreUp(t *testing.T) {
	app := controllersApp(t)
	in := &nativeInput{fd: 7, kind: InputGamepad, player: 0, info: PadInfo{GUID: testPadGUID}}
	ins := &nativeInputs{devices: []*nativeInput{in}, padProfile: app.padProfile}
	press := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonA}

	// Swap A / B from the page while the pad still holds the button that
	// chose it.
	now := time.Now()
	for _, cmd := range []Command{CmdDown, CmdDown, CmdDown, CmdLeft, CmdLeft} {
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
	remap, err := savedRemap(app, testPadGUID)
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

// While the page maps a pad, its raw controls go to the mapping, not to
// menus. Reading starts once the press that chose Map step by step is up,
// and a key still down when mapping ends comes up unseen.
func TestLearningPadIsReadRaw(t *testing.T) {
	app := controllersApp(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); writer.Close() })
	fd := int(reader.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	pad := superFamicomPad(map[uint16]controller.Range{0: {Min: 0, Max: 255}, 1: {Min: 0, Max: 255}})
	in := &nativeInput{fd: fd, kind: InputGamepad, player: 0, device: pad, mapper: controller.NewMapper(pad),
		info: PadInfo{GUID: testPadGUID}}
	ins := &nativeInputs{devices: []*nativeInput{in}, padProfile: app.padProfile}
	app.SetPadInfo(fd, PadInfo{Name: "USB Gamepad", GUID: testPadGUID, Player: 2})
	now := time.Now()
	app.mu.Lock()
	app.showControllerPadLocked(fd)
	app.startLearnLocked(now)
	app.mu.Unlock()
	poll := func(events ...[]byte) {
		t.Helper()
		for _, e := range events {
			writer.Write(e)
		}
		if _, err := ins.poll(app, now); err != nil {
			t.Fatal(err)
		}
	}
	in.setButton(uint16(ButtonSouth), CmdSelect, true)
	poll()
	if in.learner != nil {
		t.Fatal("read raw while the pad still held a menu button")
	}
	in.setButton(uint16(ButtonSouth), CmdSelect, false)
	poll()
	if in.learner == nil {
		t.Fatal("not read raw once the pad was idle")
	}
	// Up on the d-pad axis, then the DB's B key (290 is b2).
	poll(nativeTestEvent(3, 1, 0), nativeTestEvent(3, 1, 128), nativeTestEvent(1, 290, 1))
	learn := app.Snapshot().Controllers.Learn
	if learn.Rows[0].Source != "Axis 2 −" || learn.Rows[1].Source != "Button 3" || learn.Step != 2 {
		t.Fatalf("learn: %+v", learn)
	}
	if len(in.controls) != 0 || len(in.padHeld) != 0 {
		t.Fatalf("raw input reached menus: %v %v", in.controls, in.padHeld)
	}
	app.HandleCommand(CmdBack, now)
	poll()
	if in.learner != nil {
		t.Fatal("still read raw after mapping ended")
	}
	if _, ok := in.mapper.Map(1, 290, 0); ok {
		t.Fatal("the key held through the end of mapping came up")
	}
}

// A saved step-by-step mapping replaces the pad's own once it is idle.
func TestSavedMappingReachesAnAttachedPad(t *testing.T) {
	app := controllersApp(t)
	pad := superFamicomPad(map[uint16]controller.Range{0: {Min: 0, Max: 255}, 1: {Min: 0, Max: 255}})
	in := &nativeInput{fd: 7, kind: InputGamepad, player: 0, device: pad, mapper: controller.NewMapper(pad),
		info: PadInfo{GUID: testPadGUID}}
	ins := &nativeInputs{devices: []*nativeInput{in}, padProfile: app.padProfile}
	app.mu.Lock()
	ok := app.savePadProfileLocked(testPadGUID, inputmap.Profile{Mapping: "dpup:b0,dpdown:b1,dpleft:b2,dpright:b3,a:b9,b:b8"})
	app.mu.Unlock()
	if !ok {
		t.Fatal("not saved")
	}
	ins.syncProfiles(app)
	if !in.mapper.Learned() || in.remap != nil {
		t.Fatalf("mapper learned %v remap %v", in.mapper.Learned(), in.remap)
	}
	if e, ok := in.mapper.Map(1, 297, 1); !ok || e.Code != remoteinput.ButtonA {
		t.Fatalf("b9: %+v %v", e, ok)
	}
	app.mu.Lock()
	app.savePadProfileLocked(testPadGUID, inputmap.Profile{})
	app.mu.Unlock()
	ins.syncProfiles(app)
	if in.mapper.Learned() {
		t.Fatal("reset left the mapping")
	}
}
