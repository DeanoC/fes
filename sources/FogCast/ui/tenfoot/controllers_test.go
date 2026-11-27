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
	// two to its left, past Map step by step.
	for _, cmd := range []Command{CmdDown, CmdDown, CmdDown, CmdLeft, CmdLeft, CmdSelect} {
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

	app.SetPadInfo(11, PadInfo{Name: "USB gamepad", GUID: "0300cb231f08000001e4000010010000", Vendor: 0x081f, Product: 0xe401,
		Family: gfx.PromptGeneric, Recognised: true, Tested: true, Player: 1})
	app.AttachInput(InputGamepad, 11)
	app.HandleCommand(CmdFilterNext, now)
	write("layout-tested", app)

	app = controllersApp(t)
	app.SetPadInfo(7, PadInfo{Name: "USB Gamepad", GUID: testPadGUID, Vendor: 0x1234, Product: 0x5678, Player: 1})
	openLearn(t, app, now)
	write("learn-first", app)
	for _, source := range []string{"h0.1", "h0.4", "-a0", "+a0", "b2", "b1", "b3", "b1"} {
		app.LearnPadSource(7, source, now)
	}
	app.LearnPadSource(7, "b2", now)
	write("learn-optional", app)
	for _, source := range []string{"b4", "a5", "b8", "b9"} {
		app.LearnPadSource(7, source, now)
	}
	write("learn-stick", app)
	app.LearnPadSource(7, "+a0", now)
	write("learn-stick-refused", app)
	app.LearnPadSource(7, "+a3", now)
	app.LearnPadSource(7, "-a4", now)
	write("learn-review", app)
	app.LearnPadSource(7, "b2", now)
	write("layout-mapped", app)
}

// savedRemap compiles the profile saved for guid as an attaching pad does.
func savedRemap(app *App, guid string) (*inputmap.Remapper, error) {
	profile, err := app.padProfile(guid)
	if err != nil {
		return nil, err
	}
	return profileRemap(profile)
}

// openLearn starts step-by-step mapping from the page: Map step by step is
// left of Reset, below Start.
func openLearn(t *testing.T, app *App, now time.Time) {
	t.Helper()
	for _, cmd := range []Command{CmdDown, CmdDown, CmdDown, CmdLeft, CmdSelect} {
		app.HandleCommand(cmd, now)
	}
	if !app.Snapshot().Controllers.Learn.Open {
		t.Fatal("mapping did not start")
	}
	if !app.PadLearning(7) || app.PadLearning(9) {
		t.Fatal("the wrong pad is read raw")
	}
}

func TestControllersLearnStepByStep(t *testing.T) {
	app := controllersApp(t)
	now := time.Now()
	gen := app.PadProfileGeneration()
	openLearn(t, app, now)
	learn := func(sources ...string) {
		for _, s := range sources {
			app.LearnPadSource(7, s, now)
		}
	}
	learn("h0.1", "h0.4", "-a0", "+a0", "b1", "b2")
	// A control already mapped is refused, and another pad's press is not
	// this pad's.
	learn("b1")
	app.LearnPadSource(9, "b0", now)
	if snap := app.Snapshot().Controllers.Learn; snap.Label != "X" || !strings.Contains(snap.Message, "already A") {
		t.Fatalf("after a repeat: %+v", snap)
	}
	learn("b0")
	// B skips an optional step; so does X from the keyboard.
	if snap := app.Snapshot().Controllers.Learn; !snap.Optional || snap.Skip != "Button 3" {
		t.Fatalf("Y step: %+v", snap)
	}
	learn("b2")
	app.HandleCommand(CmdSortCycle, now)
	learn("a2", "b5", "b6")
	// The stick steps refuse the stick that already gives the directions;
	// B skips them.
	learn("+a0")
	if snap := app.Snapshot().Controllers.Learn; !snap.Stick || !strings.Contains(snap.Message, "already moves the directions") {
		t.Fatalf("stick step: %+v", snap)
	}
	learn("b2", "b2")
	snap := app.Snapshot().Controllers.Learn
	if !snap.Review || snap.Rows[7].Source != "Skipped" || snap.Rows[8].Source != "Skipped" || snap.Rows[9].Source != "Axis 3" {
		t.Fatalf("review: %+v", snap)
	}
	// The review is driven by the mapped pad: Right, Left, then A saves.
	learn("+a0")
	if app.Snapshot().Controllers.Learn.Choice != learnAgain {
		t.Fatal("right did not move")
	}
	learn("-a0", "b1")
	page := app.Snapshot().Controllers
	if page.Learn.Open || !page.Mapped || !strings.HasPrefix(page.Identity, "Mapped step by step") {
		t.Fatalf("after save: %+v", page)
	}
	profile, ok := savedProfile(t, app)
	want := "dpup:h0.1,dpdown:h0.4,dpleft:-a0,dpright:+a0,a:b1,b:b2,x:b0,rightshoulder:a2,back:b5,start:b6"
	if !ok || profile.Mapping != want {
		t.Fatalf("saved %+v, want mapping %q", profile, want)
	}
	if app.PadProfileGeneration() == gen {
		t.Fatal("the input layer is not told the profile changed")
	}
	// Swap A / B keeps the mapping; Reset takes it off.
	app.HandleCommand(CmdLeft, now)
	app.HandleCommand(CmdSelect, now)
	if profile, _ := savedProfile(t, app); profile.Mapping != want || profile.Preset != inputmap.PresetSwapAB {
		t.Fatalf("swap: %+v", profile)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	if _, ok := savedProfile(t, app); ok {
		t.Fatal("reset kept the mapping")
	}
}

func TestControllersLearnWaysOut(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		out  func(app *App)
	}{
		{"keyboard Back", func(app *App) { app.HandleCommand(CmdBack, now) }},
		{"nothing pressed", func(app *App) { app.Tick(now.Add(learnIdle + time.Second)) }},
		{"B in the review", func(app *App) {
			for _, s := range []string{"h0.1", "h0.4", "h0.8", "h0.2", "b0", "b1"} {
				app.LearnPadSource(7, s, now)
			}
			for i := 6; i < len(learnSteps); i++ {
				app.LearnPadSource(7, "b1", now)
			}
			if !app.Snapshot().Controllers.Learn.Review {
				t.Fatal("B did not skip to the review")
			}
			app.LearnPadSource(7, "b1", now)
		}},
		{"Cancel in the review", func(app *App) {
			for _, s := range []string{"h0.1", "h0.4", "h0.8", "h0.2", "b0", "b1"} {
				app.LearnPadSource(7, s, now)
			}
			for i := 6; i < len(learnSteps); i++ {
				app.HandleCommand(CmdSortCycle, now)
			}
			app.HandleCommand(CmdRight, now)
			app.HandleCommand(CmdRight, now)
			app.HandleCommand(CmdSelect, now)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := controllersApp(t)
			openLearn(t, app, now)
			tc.out(app)
			page := app.Snapshot().Controllers
			if page.Learn.Open || !page.Open || !strings.Contains(page.Status, "Nothing changed") {
				t.Fatalf("page: %+v", page)
			}
			if _, ok := savedProfile(t, app); ok {
				t.Fatal("a way out saved a profile")
			}
		})
	}
}

// A required step cannot be skipped, and the pad's tab cannot change
// under it.
func TestControllersLearnRequiredSteps(t *testing.T) {
	app := controllersApp(t)
	app.SetPadInfo(9, PadInfo{Name: "Other", GUID: "other", Player: 2})
	now := time.Now()
	openLearn(t, app, now)
	app.HandleCommand(CmdSortCycle, now)
	app.HandleCommand(CmdTab, now)
	snap := app.Snapshot().Controllers
	if snap.Learn.Step != 0 || snap.Tab != 0 {
		t.Fatalf("learn %+v tab %d", snap.Learn, snap.Tab)
	}
}

func TestDescribeSource(t *testing.T) {
	for source, want := range map[string]string{
		"b0": "Button 1", "h0.8": "Hat left", "h1.1": "Hat 2 up", "+a1": "Axis 2 +", "-a0": "Axis 1 −",
		"a4": "Axis 5", "a4~": "Axis 5 (reversed)",
	} {
		if got := describeSource(source); got != want {
			t.Errorf("%s = %q, want %q", source, got, want)
		}
	}
}

// A pad whose directions are a hat maps its stick as the left stick: one
// push right and one push down, in whichever sense the axis runs.
func TestControllersLearnStick(t *testing.T) {
	app := controllersApp(t)
	now := time.Now()
	openLearn(t, app, now)
	for _, s := range []string{"h0.1", "h0.4", "h0.8", "h0.2", "b0", "b1"} {
		app.LearnPadSource(7, s, now)
	}
	for i := 6; i < 12; i++ {
		app.LearnPadSource(7, "b1", now)
	}
	snap := app.Snapshot().Controllers.Learn
	if !snap.Stick || snap.Label != "Stick right" || snap.Skip != "Button 2" {
		t.Fatalf("stick right step: %+v", snap)
	}
	// A button is not a stick.
	app.LearnPadSource(7, "b4", now)
	if snap := app.Snapshot().Controllers.Learn; snap.Label != "Stick right" || !strings.Contains(snap.Message, "Push the stick") {
		t.Fatalf("button on the stick step: %+v", snap)
	}
	app.LearnPadSource(7, "+a0", now)
	// The same axis again is refused; the other one, reading low when
	// pushed down, is reversed.
	app.LearnPadSource(7, "-a0", now)
	if snap := app.Snapshot().Controllers.Learn; snap.Label != "Stick down" || !strings.Contains(snap.Message, "already Stick right") {
		t.Fatalf("same axis twice: %+v", snap)
	}
	app.LearnPadSource(7, "-a1", now)
	snap = app.Snapshot().Controllers.Learn
	if !snap.Review || snap.Rows[12].Source != "Axis 1" || snap.Rows[13].Source != "Axis 2 (reversed)" {
		t.Fatalf("review: %+v", snap.Rows)
	}
	app.LearnPadSource(7, "b0", now)
	profile, _ := savedProfile(t, app)
	if want := "dpup:h0.1,dpdown:h0.4,dpleft:h0.8,dpright:h0.2,a:b0,b:b1,leftx:a0,lefty:a1~"; profile.Mapping != want {
		t.Fatalf("saved %q, want %q", profile.Mapping, want)
	}
}

func TestControllersTestedBadgeAndSettingsSummary(t *testing.T) {
	app := NewApp(nil, 1280, 720, 100)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetPadInfo(7, PadInfo{
		Name: "USB gamepad", GUID: "0300cb231f08000001e4000010010000",
		Vendor: 0x081f, Product: 0xe401, Family: gfx.PromptGeneric, Recognised: true, Tested: true, Player: 1,
	})
	app.AttachInput(InputGamepad, 7)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	if row := settingsRowByID(t, app, settingsSectionRowPrefix+settingsSectionControllers); row.Value != "USB gamepad  ·  Tested" {
		t.Fatalf("tested controllers summary = %q, want %q", row.Value, "USB gamepad  ·  Tested")
	}

	app.mu.Lock()
	app.focusSettingsRowLocked(settingsSectionRowPrefix + settingsSectionControllers)
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, now)

	snap := app.Snapshot()
	if !snap.Controllers.Open || !snap.Controllers.Tested {
		t.Fatalf("tested Controllers snapshot = %+v", snap.Controllers)
	}
	dev, err := gfx.NewSoftware(1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	drawFrame(dev, snap, map[string]gpuTexture{}, map[string]gpuTexture{})

	// Switching to an untested pad clears Controllers.Tested, and when it is
	// the only pad attached the Settings summary omits "Tested".
	app.SetPadInfo(9, PadInfo{
		Name: "Xbox 360 Controller", GUID: testPadGUID,
		Vendor: 0x045e, Product: 0x028e, Family: gfx.PromptXbox, Recognised: true, Player: 2,
	})
	app.AttachInput(InputGamepad, 9)
	app.HandleCommand(CmdFilterNext, now)
	snap = app.Snapshot()
	if snap.Controllers.Tested {
		t.Fatalf("untested pad Controllers.Tested = true: %+v", snap.Controllers)
	}
	drawFrame(dev, snap, map[string]gpuTexture{}, map[string]gpuTexture{})

	app.DetachInput(InputGamepad, 7)
	app.HandleCommand(CmdBack, now)
	if row := settingsRowByID(t, app, settingsSectionRowPrefix+settingsSectionControllers); row.Value != "Xbox 360 Controller" {
		t.Fatalf("untested controllers summary = %q, want %q", row.Value, "Xbox 360 Controller")
	}
}

func TestControllersHidesLearnWhenPadSetsNoLearn(t *testing.T) {
	app := NewApp(nil, 1280, 720, 100)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetPadInfo(7, PadInfo{
		Name: "Xbox 360 Controller", GUID: testPadGUID,
		Vendor: 0x045e, Product: 0x028e, Family: gfx.PromptXbox, Recognised: true, Player: 1,
	})
	app.AttachInput(InputGamepad, 7)
	app.SetPadInfo(9, PadInfo{
		Name: "DualSense Wireless Controller", GUID: "050000004c050000e60c000000810000",
		Vendor: 0x054c, Product: 0x0ce6, Family: gfx.PromptPlayStation, Recognised: true, NoLearn: true, Player: 2,
	})
	app.AttachInput(InputGamepad, 9)
	app.NoteInput(InputGamepad, 7)

	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	app.mu.Lock()
	app.focusSettingsRowLocked(settingsSectionRowPrefix + settingsSectionControllers)
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, now)

	// Start on learnable pad 7 and move focus onto "Map step by step".
	for _, cmd := range []Command{CmdDown, CmdDown, CmdDown, CmdLeft, CmdLeft, CmdRight} {
		app.HandleCommand(cmd, now)
	}
	if snap := app.Snapshot().Controllers; !snap.CanLearn || snap.Focus != controllersFocusLearn {
		t.Fatalf("pad 7 should focus learn (CanLearn=%v focus=%d)", snap.CanLearn, snap.Focus)
	}

	// Switching to pad 9 (NoLearn=true) must clamp focus off Learn and hide CanLearn.
	app.HandleCommand(CmdFilterNext, now)
	snap := app.Snapshot()
	if snap.Controllers.CanLearn || snap.Controllers.Focus != controllersFocusSwap {
		t.Fatalf("pad 9 should hide learn and clamp focus to Swap: %+v", snap.Controllers)
	}

	// Moving Right from Swap skips Learn directly to Reset; Left goes back to Swap.
	app.HandleCommand(CmdRight, now)
	if got := app.Snapshot().Controllers.Focus; got != controllersFocusReset {
		t.Fatalf("Right from Swap focus = %d, want Reset (%d)", got, controllersFocusReset)
	}
	app.HandleCommand(CmdLeft, now)
	if got := app.Snapshot().Controllers.Focus; got != controllersFocusSwap {
		t.Fatalf("Left from Reset focus = %d, want Swap (%d)", got, controllersFocusSwap)
	}
	if app.PadLearning(9) {
		t.Fatal("pad 9 with NoLearn must not enter PadLearning")
	}

	dev, err := gfx.NewSoftware(1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	drawFrame(dev, app.Snapshot(), map[string]gpuTexture{}, map[string]gpuTexture{})
}

func TestControllersSettingsChordDefaultToggleAndPrompts(t *testing.T) {
	app := NewApp(nil, 1280, 720, 100)
	prefsPath := filepath.Join(t.TempDir(), "tenfoot.json")
	app.SetPrefsPath(prefsPath)
	const snesGUID = "0300cb231f08000001e4000010010000"
	app.SetPadInfo(7, PadInfo{
		Name: "USB gamepad", GUID: snesGUID,
		Vendor: 0x081f, Product: 0xe401, Family: gfx.PromptGeneric, Recognised: true, Tested: true, NoGuide: true, Player: 1,
	})
	app.AttachInput(InputGamepad, 7)

	// Pads without a Guide button default SettingsCombo to true and advertise
	// SELECT+START in place of GUIDE in footer hints.
	if got := app.Snapshot().Prompts.apply("A select  GUIDE settings"); got != "bottom button select  SELECT+START settings" {
		t.Fatalf("guide-less footer hint = %q, want SELECT+START settings", got)
	}

	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	app.mu.Lock()
	app.focusSettingsRowLocked(settingsSectionRowPrefix + settingsSectionControllers)
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, now)

	if snap := app.Snapshot().Controllers; !snap.SettingsCombo {
		t.Fatal("expected NoGuide pad to default SettingsCombo=true")
	}

	// Move down to Reset then right to the Settings shortcut toggle, and toggle off.
	for _, cmd := range []Command{CmdDown, CmdDown, CmdDown, CmdRight} {
		app.HandleCommand(cmd, now)
	}
	if got := app.Snapshot().Controllers.Focus; got != controllersFocusSettingsChord {
		t.Fatalf("focus = %d, want Settings chord (%d)", got, controllersFocusSettingsChord)
	}
	app.HandleCommand(CmdSelect, now)
	if snap := app.Snapshot().Controllers; snap.SettingsCombo {
		t.Fatal("expected SettingsCombo=false after toggling")
	}
	if got := app.Snapshot().Prompts.apply("GUIDE settings"); got != "GUIDE settings" {
		t.Fatalf("prompt after disabling SettingsCombo = %q, want GUIDE settings", got)
	}
	if p, ok := savedProfile(t, app); !ok || p.SettingsCombo != inputmap.SettingsComboOff {
		t.Fatalf("saved SettingsCombo = (%+v, %v), want %q", p, ok, inputmap.SettingsComboOff)
	}

	// Resetting the pad removes the override and restores the NoGuide default.
	app.HandleCommand(CmdLeft, now)
	app.HandleCommand(CmdSelect, now)
	if snap := app.Snapshot().Controllers; !snap.SettingsCombo {
		t.Fatal("expected reset to restore SettingsCombo=true on NoGuide pad")
	}
	if _, ok := savedProfile(t, app); ok {
		t.Fatal("expected reset of SettingsCombo-only override to clear saved profile")
	}

	dev, err := gfx.NewSoftware(1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	drawFrame(dev, app.Snapshot(), map[string]gpuTexture{}, map[string]gpuTexture{})
}
