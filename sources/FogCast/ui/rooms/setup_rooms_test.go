package rooms

import (
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

func TestWelcomePublishesContinueAndSkip(t *testing.T) {
	r := newRoom(t, examplePack(t, "example.welcome"), Options{Setup: Setup{Step: 1, Steps: 2}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Welcome to FES") })
	text := frameText(f)
	for _, want := range []string{"Step 1 of 2\n", "Sinclair ZX81\n", "Commodore 64\n", "Next: your controller"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Welcome is missing %q:\n%s", want, text)
		}
	}
	if d := r.Destination(); d.Kind != KindAction || d.LauncherAction != "setup_continue" {
		t.Fatalf("Welcome opens on %+v", d)
	}
	r.Input("right")
	if d := r.Destination(); d.LauncherAction != "setup_skip" {
		t.Fatalf("right %+v", d)
	}
	f = stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Skip goes straight to Home.") })
	r.Input("left")
	if d := r.Destination(); d.LauncherAction != "setup_continue" {
		t.Fatalf("left %+v", d)
	}
}

func TestWelcomeOutsideSetupHasNoTracker(t *testing.T) {
	r := newRoom(t, examplePack(t, "example.welcome"), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Welcome to FES") })
	if strings.Contains(frameText(f), "Step ") {
		t.Fatalf("tracker drawn outside setup:\n%s", frameText(f))
	}
}

func TestReadyListsFamiliesAndPublishesFinishAndBack(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{
		machineEntry("zx81", "fes.zx81", ""), titleEntry("zx81-a", "fes.zx81"),
		machineEntry("c64", "fes.c64", "rom"),
	}}}
	r := newRoom(t, examplePack(t, "example.setup-ready"), Options{Services: svc, ReleaseOnly: true, Setup: Setup{Step: 2, Steps: 2, Skipped: []string{"folders"}}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "1 game\n") })
	text := frameText(f)
	for _, want := range []string{"You can play now\n", "Step 2 of 2\n", "No games yet  ·  needs your firmware files\n", "Not on this kit\n", "Change any of this later in Settings.\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Ready is missing %q:\n%s", want, text)
		}
	}
	if d := r.Destination(); d.LauncherAction != "setup_finish" {
		t.Fatalf("Ready opens on %+v", d)
	}
	r.Input("right")
	if d := r.Destination(); d.LauncherAction != "setup_back" {
		t.Fatalf("right %+v", d)
	}
}

func TestReadyFailedRefreshClearsStaleFamilyGames(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{
		machineEntry("zx81", "fes.zx81", ""),
		titleEntry("old-title-a", "fes.zx81"),
		titleEntry("old-title-b", "fes.zx81"),
	}}}
	local := &fakeLocalCores{cores: []localcores.Core{{
		PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true,
	}}}
	r := newRoom(t, examplePack(t, "example.setup-ready"), Options{
		Services: svc, Local: local, ReleaseOnly: true,
		Setup: Setup{Step: 2, Steps: 2},
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "2 games\n") })
	if !strings.Contains(frameText(f), "2 games\n") {
		t.Fatalf("Ready did not show the initial library count:\n%s", frameText(f))
	}

	svc.setLibraryError(errors.New("host unreachable"))
	r.Resume()
	f = stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "Ready\n") && !strings.Contains(text, "2 games\n")
	})
	if strings.Contains(frameText(f), "2 games\n") {
		t.Fatalf("Ready kept the stale library count after refresh failed:\n%s", frameText(f))
	}
}

func TestHomeBannerWhileSetupIsUnfinished(t *testing.T) {
	svc := &homeServices{
		fakeServices: fakeServices{games: []hostclient.Game{machineEntry("zx81", "fes.zx81", "")}},
		recents:      []hostclient.Game{titleEntry("lunar", "fes.spectrum")},
	}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Setup: Setup{Unfinished: true, Skipped: []string{"ready"}}})
	f := stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	text := frameText(f)
	// The banner holds the Recently played slot, and Home opens on a family.
	if !strings.Contains(text, "Get started\n") || !strings.Contains(text, "Setup isn't finished\n") || strings.Contains(text, "Recently played") {
		t.Fatalf("banner slot:\n%s", text)
	}
	if d := r.Destination(); d.Kind != KindFamily {
		t.Fatalf("opening %+v", d)
	}
	if !r.Input("up") {
		t.Fatal("Up from the tiles did not reach the banner")
	}
	if d := r.Destination(); d.Kind != KindAction || d.LauncherAction != "setup_resume" {
		t.Fatalf("banner %+v", d)
	}
	if r.Input("up") {
		t.Fatal("Home consumed Up from the banner, its top row")
	}
	r.Input("down")
	if d := r.Destination(); d.Kind != KindFamily {
		t.Fatalf("down from the banner %+v", d)
	}
}
