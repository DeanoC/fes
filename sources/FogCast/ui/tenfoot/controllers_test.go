package tenfoot

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

const testPadGUID = "030000005e0400008e02000014010000"

// controllersApp is an app with one Xbox pad attached and Settings ›
// Controllers open on it.
func controllersApp(t *testing.T) *App {
	t.Helper()
	app := NewApp(nil, 1280, 720, 100)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	return openControllersApp(t, app)
}

func openControllersApp(t *testing.T, app *App) *App {
	t.Helper()
	app.SetPadInfo(7, PadInfo{Name: "Xbox 360 Controller", GUID: testPadGUID, Vendor: 0x045e, Product: 0x028e,
		Family: gfx.PromptXbox, Recognised: true, Player: 1})
	app.AttachInput(InputGamepad, 7)
	app.NoteInput(InputGamepad, 7)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	if !app.SettingsOpen() {
		t.Fatal("Settings did not open")
	}
	app.mu.Lock()
	app.focusSettingsRowLocked(settingsSectionRowPrefix + settingsSectionControllers)
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, now)
	if !app.Snapshot().Controllers.Open {
		t.Fatal("Controllers did not open")
	}
	return app
}

func controllerRow(t *testing.T, app *App, label string) ControllerRow {
	t.Helper()
	for _, row := range app.Snapshot().Controllers.Rows {
		if row.Label == label {
			return row
		}
	}
	t.Fatalf("no row %q", label)
	return ControllerRow{}
}

func savedProfile(t *testing.T, app *App) (inputmap.Profile, bool) {
	t.Helper()
	prefs, err := loadTenfootPrefs(app.prefsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return inputmap.Profile{}, false
		}
		t.Fatal(err)
	}
	profile, ok := prefs.Controllers[testPadGUID]
	return profile, ok
}

func TestControllersListedOnlyWithAPad(t *testing.T) {
	app := NewApp(nil, 1280, 720, 100)
	app.HandleCommand(CmdSettings, time.Now())
	for _, row := range app.Snapshot().Settings.Rows {
		if row.Label == "Controllers" {
			t.Fatal("Controllers is listed with no pad attached")
		}
	}
}

func TestControllersRebindSavesAndRedrawsHints(t *testing.T) {
	app := controllersApp(t)
	now := time.Now()
	// Focus opens on the bottom button. Move up to the right button and
	// send X from it.
	app.HandleCommand(CmdUp, now)
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot().Controllers
	if !snap.Picker.Open || !snap.Picker.Targets[snap.Picker.Index].Now || snap.Picker.Targets[snap.Picker.Index].Label != "B" {
		t.Fatalf("picker did not open on what the button sends now: %+v", snap.Picker)
	}
	// B is the only Back, and no other button sends C to swap with.
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	if row := controllerRow(t, app, "Right button"); row.Target != "B" || row.Changed {
		t.Fatalf("a change that leaves no Back was applied: %+v", row)
	}
	if status := app.Snapshot().Controllers.Status; !strings.Contains(status, "Not changed") {
		t.Fatalf("status = %q", status)
	}
	if _, ok := savedProfile(t, app); ok {
		t.Fatal("a refused change was saved")
	}

	// Sending A from the right button swaps it with the bottom button.
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdLeft, now)
	app.HandleCommand(CmdSelect, now)
	if row := controllerRow(t, app, "Right button"); row.Target != "A" || !row.Changed {
		t.Fatalf("right button = %+v", row)
	}
	if row := controllerRow(t, app, "Bottom button"); row.Target != "B" || !row.Changed {
		t.Fatalf("bottom button = %+v", row)
	}
	profile, ok := savedProfile(t, app)
	if !ok || profile.Bindings["a"] != "b" || profile.Bindings["b"] != "a" || len(profile.Bindings) != 2 {
		t.Fatalf("saved profile = %+v", profile)
	}
	// Hints now draw Confirm as the right-hand button.
	if got, want := app.Snapshot().padHint("A select"), string(gfx.PromptRune(gfx.PromptXbox, gfx.PromptEast))+" select"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}

	// The button's own default undoes it, swapping back.
	app.HandleCommand(CmdSortCycle, now)
	if _, ok := savedProfile(t, app); ok {
		t.Fatal("a pad back on its own layout still has a saved profile")
	}
}

func TestControllersFailedPreferenceWriteDoesNotApplyProfile(t *testing.T) {
	initial := tenfootPrefs{Controllers: map[string]inputmap.Profile{
		testPadGUID: {Bindings: map[string]string{"x": "a"}},
	}}
	data, err := encodeTenfootPrefs(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &testPreferenceStore{data: data, writeErr: errors.New("flush failed")}
	app := NewApp(nil, 1280, 720, 100)
	if err := app.SetPreferenceStore(store); err != nil {
		t.Fatal(err)
	}
	app = openControllersApp(t, app)
	beforeGen := app.PadProfileGeneration()
	beforeHint := app.Snapshot().padHint("A select")
	app.mu.Lock()
	beforeSends := app.padRemapLocked(testPadGUID)
	app.mu.Unlock()
	if beforeSends == nil {
		t.Fatal("saved profile did not load")
	}
	beforeTarget := beforeSends.Target(remoteinput.ButtonX)

	app.mu.Lock()
	app.setPadTargetLocked(remoteinput.ButtonX, remoteinput.ButtonY)
	app.mu.Unlock()

	if got := app.PadProfileGeneration(); got != beforeGen {
		t.Fatalf("profile generation advanced after failed save: got %d, want %d", got, beforeGen)
	}
	if got := app.Snapshot().padHint("A select"); got != beforeHint {
		t.Fatalf("hint changed after failed save: got %q, want %q", got, beforeHint)
	}
	app.mu.Lock()
	afterSends := app.padRemapLocked(testPadGUID)
	app.mu.Unlock()
	if got := afterSends.Target(remoteinput.ButtonX); got != beforeTarget {
		t.Fatalf("cached profile changed after failed save: X now sends %v, want %v", got, beforeTarget)
	}
	if got, err := decodeTenfootPrefs(store.data); err != nil || got.Controllers[testPadGUID].Bindings["x"] != "a" {
		t.Fatalf("failed save changed stored profile: %#v, %v", got.Controllers, err)
	}
	if status := app.Snapshot().Controllers.Status; !strings.Contains(status, "Not saved") {
		t.Fatalf("controller status = %q, want a save failure", status)
	}
}

func TestControllersRebindSwapsTakenTarget(t *testing.T) {
	app := controllersApp(t)
	app.mu.Lock()
	app.setPadTargetLocked(remoteinput.ButtonX, remoteinput.ButtonA)
	app.mu.Unlock()

	if row := controllerRow(t, app, "Left button"); row.Target != "A" {
		t.Fatalf("left button = %+v, want A", row)
	}
	if row := controllerRow(t, app, "Bottom button"); row.Target != "X" {
		t.Fatalf("bottom button = %+v, want X after A moved", row)
	}
	profile, ok := savedProfile(t, app)
	if !ok || profile.Bindings["x"] != "a" || profile.Bindings["a"] != "x" {
		t.Fatalf("saved profile = %+v", profile)
	}
}

func TestControllersRebindKeepsAControlSentByTwoButtons(t *testing.T) {
	app := controllersApp(t)
	prefs := tenfootPrefs{Controllers: map[string]inputmap.Profile{
		testPadGUID: {Bindings: map[string]string{"x": "a", "y": "a"}},
	}}
	if err := saveTenfootPrefs(app.prefsPath, prefs); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	app.padProfiles = nil // Reload the saved profile into the running page.
	app.setPadTargetLocked(remoteinput.ButtonL, remoteinput.ButtonA)
	app.mu.Unlock()

	// Three buttons sent A, so no single one takes over L's control.
	profile, ok := savedProfile(t, app)
	if !ok || profile.Bindings["l"] != "a" || profile.Bindings["x"] != "a" || profile.Bindings["y"] != "a" {
		t.Fatalf("saved profile = %+v", profile)
	}
}

func TestControllersSwapAndReset(t *testing.T) {
	app := controllersApp(t)
	now := time.Now()
	gen := app.PadProfileGeneration()
	// Down from the bottom button twice reaches Start, then Reset; Swap is
	// to its left.
	for _, cmd := range []Command{CmdDown, CmdDown, CmdDown, CmdLeft, CmdSelect} {
		app.HandleCommand(cmd, now)
	}
	snap := app.Snapshot().Controllers
	if !snap.SwapAB || controllerRow(t, app, "Bottom button").Target != "B" {
		t.Fatalf("swap did not apply: %+v", snap)
	}
	if profile, _ := savedProfile(t, app); profile.Preset != inputmap.PresetSwapAB || len(profile.Bindings) != 0 {
		t.Fatalf("saved profile = %+v", profile)
	}
	if app.PadProfileGeneration() == gen {
		t.Fatal("the input layer is not told the profile changed")
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	if _, ok := savedProfile(t, app); ok {
		t.Fatal("reset left a saved profile")
	}
	if app.Snapshot().Controllers.SwapAB {
		t.Fatal("reset left Swap A / B on")
	}
}

func TestControllersKeypadTargetAndPressedButton(t *testing.T) {
	app := controllersApp(t)
	now := time.Now()
	app.HandleCommand(CmdDown, now) // left button
	app.HandleCommand(CmdSelect, now)
	for i := 0; i < 3; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdSelect, now)
	row := controllerRow(t, app, "Left button")
	if !strings.HasPrefix(row.Target, "Keypad ") || !row.Changed {
		t.Fatalf("left button = %+v", row)
	}
	profile, _ := savedProfile(t, app)
	if !strings.HasPrefix(profile.Bindings["x"], "keypad-") {
		t.Fatalf("saved profile = %+v", profile)
	}
	if _, err := inputmap.NewRemapper(profile); err != nil {
		t.Fatalf("saved profile does not compile: %v", err)
	}

	app.NotePadButton(7, remoteinput.ButtonY, true)
	if !controllerRow(t, app, "Top button").Pressed || !app.Snapshot().Controllers.Tabs[0].Pressed {
		t.Fatal("a held button does not light up")
	}
	app.NotePadButton(7, remoteinput.ButtonY, false)
	if controllerRow(t, app, "Top button").Pressed {
		t.Fatal("a released button stays lit")
	}
}

func TestControllersPickerMovesThroughEveryTarget(t *testing.T) {
	seen := map[int]bool{0: true}
	index := 0
	for index != len(padTargets)-1 {
		next := controllerPickerStep(index, CmdRight)
		if next == index {
			t.Fatalf("Right stops at %d", index)
		}
		index = next
		seen[index] = true
	}
	if len(seen) != len(padTargets) {
		t.Fatalf("Right reaches %d of %d targets", len(seen), len(padTargets))
	}
	// Down from the first row ends on the last row; Up returns to the first.
	index = 0
	for i := 0; i < 8; i++ {
		index = controllerPickerStep(index, CmdDown)
	}
	if g, r, _ := controllerPickerCell(index); g != 2 || r != 1 {
		t.Fatalf("Down ends in group %d row %d", g, r)
	}
	for i := 0; i < 8; i++ {
		index = controllerPickerStep(index, CmdUp)
	}
	if g, r, _ := controllerPickerCell(index); g != 0 || r != 0 {
		t.Fatalf("Up ends in group %d row %d", g, r)
	}
}

func TestControllersClosesWhenItsPadUnplugs(t *testing.T) {
	app := controllersApp(t)
	app.DetachInput(InputGamepad, 7)
	if app.Snapshot().Controllers.Open {
		t.Fatal("Controllers stays open with no pad")
	}
	app.HandleCommand(CmdBack, time.Now())
}

// TestWriteControllersSheet renders the page for a look by eye:
// FES_CONTROLLERS_SHEET=dir go test -run TestWriteControllersSheet ./ui/tenfoot
func TestWriteControllersSheet(t *testing.T) {
	dir := os.Getenv("FES_CONTROLLERS_SHEET")
	if dir == "" {
		t.Skip("FES_CONTROLLERS_SHEET is not set")
	}
	write := func(name string, app *App) {
		dev, err := gfx.NewSoftware(1280, 720)
		if err != nil {
			t.Fatal(err)
		}
		drawFrame(dev, app.Snapshot(), map[string]gpuTexture{}, map[string]gpuTexture{})
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, dev.Framebuffer()); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	app := controllersApp(t)
	app.SetPadInfo(9, PadInfo{Name: "DualSense Wireless Controller", GUID: "050000004c050000e60c000000810000", Vendor: 0x054c, Product: 0x0ce6,
		Family: gfx.PromptPlayStation, Recognised: true, Player: 2})
	app.AttachInput(InputGamepad, 9)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdUp, now)
	app.NoteInput(InputGamepad, 7)
	app.NotePadButton(7, remoteinput.ButtonY, true)
	write("layout-xbox", app)
	app.NotePadButton(7, remoteinput.ButtonY, false)
	app.HandleCommand(CmdSelect, now)
	write("picker-xbox", app)
	app.HandleCommand(CmdBack, now)
	app.HandleCommand(CmdFilterNext, now)
	write("layout-playstation", app)
}
