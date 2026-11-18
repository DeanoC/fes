package tenfoot

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
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
