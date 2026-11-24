//go:build linux

package tenfoot

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestConfiguredAppOnlyDisablesKitLocalKeyboardOnSDLWindows(t *testing.T) {
	for _, tc := range []struct {
		backend string
		kit     bool
		want    bool
	}{
		{"sdl", true, true}, {"software", true, true}, {"fpga", true, true},
		{"linuxfb", true, false}, {"menu-display", true, false}, {"sdl", false, false},
	} {
		t.Run(tc.backend+map[bool]string{true: "-kit", false: "-paired"}[tc.kit], func(t *testing.T) {
			app, err := configuredApp(Options{GFX: tc.backend, KitHost: tc.kit, APIBase: "http://127.0.0.1:1", PrefsPath: filepath.Join(t.TempDir(), "prefs.json"), Width: 1280, Height: 720})
			if err != nil {
				t.Fatal(err)
			}
			defer app.Stop()
			if app.kitLocalKeyboardUnavailable != tc.want {
				t.Fatalf("unavailable=%v, want %v", app.kitLocalKeyboardUnavailable, tc.want)
			}
		})
	}
}

func TestSDLKitLocalLegacyKeyboardCannotForwardGuestInput(t *testing.T) {
	app := NewApp(NewClient("http://127.0.0.1:1", nil), 1280, 720, 20)
	defer app.Stop()
	feed := &boundKeyboardRecorder{}
	app.SetKitLocal(&fakeLocalCores{}, feed)
	app.mu.Lock()
	app.kitHost, app.kitLocalKeyboardUnavailable = true, true
	app.session = kitDisplaySession()
	app.session.CoreKeyboard, app.session.CoreKeyboardHID = true, false
	app.mu.Unlock()
	event := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Code: remoteinput.KeyA, Action: remoteinput.ActionPress}
	if app.sendPhysicalPlayKey(1, "a", 4, event) || app.SendPlayHID(event) {
		t.Fatal("legacy physical keyboard reached guest route")
	}
	if !strings.Contains(app.Snapshot().Status, kitLocalKeyboardUnavailableStatus) {
		t.Fatal("legacy keyboard unavailable status was not visible")
	}
	feed.mu.Lock()
	defer feed.mu.Unlock()
	if feed.raw != 0 || len(feed.events) != 0 {
		t.Fatal("legacy key was queued to local guest feed")
	}
}

func TestSDLKitGuestQueueDropsKeyboardButKeepsPadAndMouse(t *testing.T) {
	app := NewApp(NewClient("http://127.0.0.1:1", nil), 1280, 720, 20)
	defer app.Stop()
	feed := &fakePadFeed{}
	app.SetKitLocal(&fakeLocalCores{}, feed)
	app.mu.Lock()
	app.kitHost, app.kitLocalKeyboardUnavailable = true, true
	app.session = kitDisplaySession()
	keyboard := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Code: remoteinput.KeyA, Action: remoteinput.ActionRelease}
	disguised := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindKey, Code: remoteinput.KeyA, Action: remoteinput.ActionRelease}
	pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonA, Action: remoteinput.ActionPress}
	mouse := remoteinput.MouseEvent(1, 0, 0)
	app.queuePlayHIDLocked([]remoteinput.Event{keyboard, disguised, pad, mouse})
	tail := app.playHIDTail
	app.mu.Unlock()
	if tail == nil {
		t.Fatal("nonkeyboard input was lost")
	}
	<-tail
	got := feed.snapshot()
	if len(got) != 2 || got[0] != pad || got[1] != mouse {
		t.Fatalf("queued events %+v", got)
	}
}

func TestSDLKitLocalKeyboardUnavailableIsVisibleAndCannotBind(t *testing.T) {
	app := NewApp(NewClient("http://127.0.0.1:1", nil), 1280, 720, 20)
	defer app.Stop()
	feed := &boundKeyboardRecorder{}
	app.SetKitLocal(&fakeLocalCores{}, feed)
	app.mu.Lock()
	app.kitHost, app.kitLocalKeyboardUnavailable = true, true
	app.session = kitDisplaySession()
	app.session.CoreKeyboardHID = true
	app.session.CorePackage.PackageID, app.session.CorePackage.Generation = strings.Repeat("a", 64), 1
	app.healthHave = true
	app.health.Ready, app.health.TargetReachable = true, true
	app.health.Connection.State, app.health.Connection.LeaseOwned = "active", true
	app.health.Connection.TargetID = app.session.TargetID
	app.health.Connection.LeaseGeneration = strings.Repeat("b", 64)
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "active", Generation: strings.Repeat("b", 64)}
	app.kitLocalKeyboardUnavailable = false
	_, before := app.localKeyboardBindingLocked()
	app.kitLocalKeyboardUnavailable = true
	_, bound := app.localKeyboardBindingLocked()
	closed := app.keyboardHIDFailClosedLocked()
	app.mu.Unlock()
	if !before || bound || !closed {
		t.Fatal("SDL kit-local HID route was admitted")
	}
	// The same exclusion applies while another host owns the execution lease.
	app.mu.Lock()
	app.health.Connection.State, app.health.Connection.LeaseOwned, app.health.Connection.Owner = "busy", false, "remote"
	app.kitLease.State, app.kitLease.Owner = "held", "remote"
	app.kitLocalKeyboardUnavailable = false
	_, foreignBefore := app.localKeyboardBindingLocked()
	app.kitLocalKeyboardUnavailable = true
	_, foreignAfter := app.localKeyboardBindingLocked()
	app.mu.Unlock()
	if !foreignBefore || foreignAfter {
		t.Fatal("SDL foreign-owner binding was not gated")
	}
	if !strings.Contains(app.Snapshot().Status, kitLocalKeyboardUnavailableStatus) {
		t.Fatal("SDL kit-local keyboard unavailability was not visible")
	}
	// A paired host never receives the kit-local unavailable status.
	app.mu.Lock()
	app.client.paired = true
	app.mu.Unlock()
	if strings.Contains(app.Snapshot().Status, kitLocalKeyboardUnavailableStatus) {
		t.Fatal("paired host inherited kit-local unavailable status")
	}
}

func TestSDLUnavailableStatusReachesRoomRenderSelectors(t *testing.T) {
	snap := Snapshot{Status: strings.Repeat("older long status ", 100), Session: SessionSnapshot{KeyboardUnavailable: true}}
	snap.Room.Notice = "another status"
	snap.Health.Line = "host unreachable"
	snap.Health.HostUnreachable = true
	snap.KitLease.Line = "kit busy"
	if got := heroFooterStatus(snap); got != kitLocalKeyboardUnavailableStatus {
		t.Fatalf("hero footer hides unavailable notice: %q", got)
	}
	if got := hardwareRoomStatus(snap); got != kitLocalKeyboardUnavailableStatus {
		t.Fatalf("hardware footer hides unavailable notice: %q", got)
	}
	// A normal 1280px layout reserves at least a third of the content band.
	if width := gfx.MeasureText(kitLocalKeyboardUnavailableStatus, 16); width > (1280-64)/3-20 {
		t.Fatalf("unavailable notice does not fit footer: %dpx", width)
	}
}
