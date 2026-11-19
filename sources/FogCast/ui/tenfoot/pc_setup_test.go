package tenfoot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestFirstRunIncludesPCFamiliesAtSupportedSizes(t *testing.T) {
	labels := []string{"Sinclair ZX81", "ZX Spectrum", "Atari ST", "Commodore 64", "PC/XT", "PC 386"}
	for _, size := range []struct{ w, h int }{{1280, 720}, {1152, 648}, {1024, 600}, {854, 480}, {768, 432}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			app := startFirstRun(t, t.TempDir(), size.w, size.h)
			welcome := waitFor(t, app, "Welcome with PC families", func(s Snapshot) bool {
				return s.Room.ID == "example.welcome" && roomTextHas(s, "PC/XT") && roomTextHas(s, "PC 386")
			})
			assertSetupFamilyLabels(t, welcome, labels)
			assertSetupTextClearOfButtons(t, welcome)
			if !roomTextHas(welcome, "In development") || roomTextHas(welcome, "Four are ready") {
				t.Fatal("Welcome does not describe the PC 386's current status")
			}
			writeHomeScreenshot(t, welcome, "pc-welcome")
			app.HandleCommand(CmdSelect, time.Now())
			ready := waitFor(t, app, "Ready with PC families", func(s Snapshot) bool {
				return s.Room.ID == "example.setup-ready" && roomTextHas(s, "PC/XT") && roomTextHas(s, "PC 386") && !roomTextHas(s, "Checking…")
			})
			assertSetupFamilyLabels(t, ready, labels)
			assertSetupTextClearOfButtons(t, ready)
			writeHomeScreenshot(t, ready, "pc-ready")
		})
	}
}

func TestWelcomeKeepsIntroductionFocusedWhenAllCoresShown(t *testing.T) {
	app := startFirstRun(t, t.TempDir(), 1280, 720)
	waitRoom(t, app, "example.welcome")
	app.SetAllCores(true)
	app.mu.Lock()
	app.openSetupStepLocked(0)
	app.mu.Unlock()
	snap := waitFor(t, app, "Welcome with all cores enabled", func(s Snapshot) bool {
		return s.Room.ID == "example.welcome" && roomTextHas(s, "PC 386")
	})
	for _, label := range []string{"Apple II", "ColecoVision", "Sega Master System", "Pong"} {
		if roomTextHas(snap, label) {
			t.Fatalf("Welcome introduced held-back core %q", label)
		}
	}
}

func assertSetupFamilyLabels(t *testing.T, snap Snapshot, labels []string) {
	t.Helper()
	for _, label := range labels {
		var found bool
		for _, op := range snap.Room.Frame.Ops {
			if op.Kind != rooms.OpText || op.Text != label {
				continue
			}
			found = true
			weight := gfx.WeightRegular
			if op.Bold {
				weight = gfx.WeightBold
			}
			w := gfx.MeasureTextWeight(op.Text, op.Size, weight)
			if op.MaxW > 0 && w > op.MaxW {
				t.Errorf("%s clips family label %q: %d > %d", snap.Room.ID, label, w, op.MaxW)
			}
			if op.X < 0 || op.Y < 0 || op.X+float32(w) > float32(snap.Room.Width) || op.Y+float32(gfx.TextHeight(op.Size)) > float32(snap.Room.Height) {
				t.Errorf("%s puts family label %q outside its frame", snap.Room.ID, label)
			}
		}
		if !found {
			t.Errorf("%s is missing family label %q:\n%s", snap.Room.ID, label, strings.TrimSpace(snap.Room.Title))
		}
	}
}
