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

func TestDuplicatePersistedProfileCannotShareHeldSessionTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	prefs := tenfootPrefs{Controllers: map[string]inputmap.Profile{
		testPadGUID: {Bindings: map[string]string{"x": "a", "y": "a"}},
	}}
	if err := saveTenfootPrefs(path, prefs); err != nil {
		t.Fatal(err)
	}
	feed := &localPadRecorder{}
	app := runningLocalCoreApp(feed)
	app.SetPrefsPath(path)
	remap, err := app.padRemap(testPadGUID)
	if err == nil {
		t.Fatal("duplicate persisted targets were accepted")
	}
	if remap != nil {
		t.Fatal("invalid profile returned an active remapper")
	}
	in := &nativeInput{fd: 7, player: 0, info: PadInfo{GUID: testPadGUID}}
	now := time.Now()
	in.button(app, ButtonWest, 1, now)  // X
	in.button(app, ButtonNorth, 1, now) // Y
	in.button(app, ButtonNorth, 0, now)
	if len(feed.events) != 3 ||
		feed.events[0].Code != remoteinput.ButtonX || feed.events[0].Action != remoteinput.ActionPress ||
		feed.events[1].Code != remoteinput.ButtonY || feed.events[1].Action != remoteinput.ActionPress ||
		feed.events[2].Code != remoteinput.ButtonY || feed.events[2].Action != remoteinput.ActionRelease {
		t.Fatalf("releasing Y affected X's held input: %+v", feed.events)
	}
	if _, ok := app.playPadHeld[padKey{0, remoteinput.ButtonX}]; !ok {
		t.Fatal("X's hold was released with Y")
	}
	in.button(app, ButtonWest, 0, now)
	if len(feed.events) != 4 || feed.events[3].Code != remoteinput.ButtonX || feed.events[3].Action != remoteinput.ActionRelease {
		t.Fatalf("X release was not kept independent: %+v", feed.events)
	}
}
