//go:build sdl3

// These SDL integration tests run in the host-sdl3 CI job.

package tenfoot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestSDLPadFamilyAndBusFromGUID(t *testing.T) {
	if got := sdlBusFromGUID("050000004c050000e60c000000810000"); got != 5 {
		t.Fatalf("bluetooth GUID bus = %d, want 5", got)
	}
	if got := sdlBusFromGUID("030000005e0400008e02000014010000"); got != 3 {
		t.Fatalf("usb GUID bus = %d, want 3", got)
	}
	for _, tc := range sdlPadFamilyCases() {
		if tc.got != tc.want {
			t.Fatalf("%s family = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestSDLControllersVirtualGamepadUIAndSwapAB(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if !initSDLGamepadSubsystem() {
		if os.Getenv("FOGCAST_REQUIRE_SDL3") == "1" {
			t.Fatalf("SDL gamepad init unavailable in required SDL3 test run: %s", sdlError())
		}
		t.Skipf("SDL gamepad init unavailable: %s", sdlError())
	}
	defer quitSDLGamepadSubsystem()

	app := NewApp(nil, 1280, 720, 100)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))

	pads := newSDLPadSet()
	defer pads.close()

	id, cleanupPad, err := attachVirtualSDLPadForTest(app, pads)
	if err != nil {
		t.Fatalf("attach virtual gamepad: %v", err)
	}
	defer cleanupPad()

	if got := pads.len(); got != 1 {
		t.Fatalf("pads.len() = %d, want 1", got)
	}

	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	app.mu.Lock()
	app.focusSettingsRowLocked(settingsSectionRowPrefix + settingsSectionControllers)
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if !snap.Controllers.Open || len(snap.Controllers.Tabs) != 1 || snap.Controllers.Tabs[0].Label != "P1" {
		t.Fatalf("Settings › Controllers did not open for virtual SDL pad: %+v", snap.Controllers)
	}
	if snap.Controllers.CanLearn {
		t.Fatal("expected SDL pad in Settings › Controllers to report CanLearn=false")
	}

	// Pressing Y while Controllers is open highlights both the tab and row via NotePadButton.
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonY, true); err != nil {
		t.Fatal(err)
	}
	held := map[Command]bool{}
	var stick stickTracker
	pollGamepads(app, pads, held, &stick, now)
	app.Tick(now)

	snap = app.Snapshot()
	if !snap.Controllers.Tabs[0].Pressed {
		t.Fatal("expected P1 controller tab to highlight while North is held")
	}
	foundNorthPressed := false
	for _, row := range snap.Controllers.Rows {
		if row.Control == gfx.PromptNorth && row.Pressed {
			foundNorthPressed = true
		}
	}
	if !foundNorthPressed {
		t.Fatalf("expected North row pressed in Controllers snapshot: %+v", snap.Controllers.Rows)
	}

	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonY, false); err != nil {
		t.Fatal(err)
	}
	pollGamepads(app, pads, held, &stick, now.Add(20*time.Millisecond))

	// Hold South across Swap A / B: the held release stays ButtonA, then the
	// reloaded profile takes effect once released.
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonA, true); err != nil {
		t.Fatal(err)
	}
	pollGamepads(app, pads, held, &stick, now.Add(40*time.Millisecond))
	if got := sdlPadSentCode(pads, id, remoteinput.ButtonA); got != remoteinput.ButtonA {
		t.Fatalf("held South code before swap = %v, want A", got)
	}

	app.mu.Lock()
	_, ok := app.togglePadSwapLocked(int(id))
	app.mu.Unlock()
	if !ok {
		t.Fatal("togglePadSwapLocked failed")
	}

	pollGamepads(app, pads, held, &stick, now.Add(60*time.Millisecond))
	if pads.profileGen == app.PadProfileGeneration() {
		t.Fatal("expected SDL pad profile reload to wait until held button is released")
	}

	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonA, false); err != nil {
		t.Fatal(err)
	}
	pollGamepads(app, pads, held, &stick, now.Add(80*time.Millisecond))
	pollGamepads(app, pads, held, &stick, now.Add(100*time.Millisecond))
	if pads.profileGen != app.PadProfileGeneration() {
		t.Fatalf("expected SDL profileGen %d to catch up to App %d after release", pads.profileGen, app.PadProfileGeneration())
	}

	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonA, true); err != nil {
		t.Fatal(err)
	}
	pollGamepads(app, pads, held, &stick, now.Add(120*time.Millisecond))
	if got := sdlPadSentCode(pads, id, remoteinput.ButtonA); got != remoteinput.ButtonB {
		t.Fatalf("swapped South sent %v, want ButtonB", got)
	}
	_ = setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonA, false)
	pollGamepads(app, pads, held, &stick, now.Add(140*time.Millisecond))

	// Enabling the Settings chord in Controllers lets Select+Start open Settings
	// from any menu without quitting.
	app.mu.Lock()
	if _, ok := app.togglePadSettingsComboLocked(int(id)); !ok {
		app.mu.Unlock()
		t.Fatal("togglePadSettingsComboLocked failed")
	}
	app.closeSettingsLocked()
	app.mu.Unlock()

	pollGamepads(app, pads, held, &stick, now.Add(160*time.Millisecond))
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonStart, true); err != nil {
		t.Fatal(err)
	}
	if pollGamepads(app, pads, held, &stick, now.Add(180*time.Millisecond)) {
		t.Fatal("lone Start quit with Settings chord enabled")
	}
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonSelect, true); err != nil {
		t.Fatal(err)
	}
	if pollGamepads(app, pads, held, &stick, now.Add(200*time.Millisecond)) {
		t.Fatal("Select+Start quit instead of opening Settings")
	}
	if !app.SettingsOpen() {
		t.Fatal("Select+Start did not open Settings on SDL pad")
	}
	_ = setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonStart, false)
	_ = setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonSelect, false)
	pollGamepads(app, pads, held, &stick, now.Add(220*time.Millisecond))

	// Select then Start closes Settings without cycling layout.
	layoutBefore := app.Snapshot().Grid.Mode
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonSelect, true); err != nil {
		t.Fatal(err)
	}
	pollGamepads(app, pads, held, &stick, now.Add(240*time.Millisecond))
	if got := app.Snapshot().Grid.Mode; got != layoutBefore {
		t.Fatalf("pressing Select with Settings chord enabled cycled layout eagerly: got %v, want %v", got, layoutBefore)
	}
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonStart, true); err != nil {
		t.Fatal(err)
	}
	if pollGamepads(app, pads, held, &stick, now.Add(260*time.Millisecond)) {
		t.Fatal("Select -> Start quit instead of closing Settings")
	}
	if app.SettingsOpen() {
		t.Fatal("Select -> Start did not close Settings on SDL pad")
	}
	if got := app.Snapshot().Grid.Mode; got != layoutBefore {
		t.Fatalf("Select -> Start chord changed layout from %v to %v", layoutBefore, got)
	}
	_ = setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonSelect, false)
	_ = setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonStart, false)
	pollGamepads(app, pads, held, &stick, now.Add(280*time.Millisecond))
	if got := app.Snapshot().Grid.Mode; got != layoutBefore {
		t.Fatalf("unwinding Select -> Start changed layout from %v to %v", layoutBefore, got)
	}

	// A solo Select tap waits until release, then cycles layout normally.
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonSelect, true); err != nil {
		t.Fatal(err)
	}
	pollGamepads(app, pads, held, &stick, now.Add(300*time.Millisecond))
	if got := app.Snapshot().Grid.Mode; got != layoutBefore {
		t.Fatalf("solo Select cycled layout before release: got %v, want %v", got, layoutBefore)
	}
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonSelect, false); err != nil {
		t.Fatal(err)
	}
	pollGamepads(app, pads, held, &stick, now.Add(320*time.Millisecond))
	if got := app.Snapshot().Grid.Mode; got == layoutBefore {
		t.Fatalf("solo Select release did not cycle layout from %v", layoutBefore)
	}

	// A solo Start tap waits until release, then produces CmdQuit on desktop.
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonStart, true); err != nil {
		t.Fatal(err)
	}
	if pollGamepads(app, pads, held, &stick, now.Add(340*time.Millisecond)) {
		t.Fatal("solo Start quit before release")
	}
	if err := setVirtualSDLButtonForTest(pads, id, remoteinput.ButtonStart, false); err != nil {
		t.Fatal(err)
	}
	if !pollGamepads(app, pads, held, &stick, now.Add(360*time.Millisecond)) {
		t.Fatal("solo Start release did not quit on desktop SDL pad")
	}

	// Detach clears Settings › Controllers.
	cleanupPad()
	if pads.len() != 0 {
		t.Fatalf("expected 0 SDL pads after detach, got %d", pads.len())
	}
	if app.Snapshot().Controllers.Open {
		t.Fatal("expected Controllers page to close when last SDL pad detaches")
	}
}
