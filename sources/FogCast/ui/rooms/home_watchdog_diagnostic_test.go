package rooms

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
)

// TestHomeWatchdogDiagnostic runs the embedded Home on a target CPU with the
// production deadlines. It does not exercise the launcher, HDMI or real cores.
// Opt in on an idle, claimed kit; stop at the first failure to retain its timing.
func TestHomeWatchdogDiagnostic(t *testing.T) {
	if os.Getenv("FES_HOME_WATCHDOG_DIAGNOSTIC") != "1" {
		t.Skip("explicit target diagnostic")
	}
	cores := make([]localcores.Core, 8)
	for i, id := range []string{"fes.apple2", "fes.atari-st", "fes.coleco", "fes.c64", "fes.pong", "fes.sms", "fes.spectrum", "fes.zx81"} {
		cores[i] = localcores.Core{PackageID: fmt.Sprintf("%064x", i+1), CoreID: id, Name: id, Needs: "none", Launchable: true}
	}
	pack := examplePack(t, "example.fes-cores")
	var maxDraw time.Duration
	for cycle := 0; cycle < 100; cycle++ {
		r := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: cores}, Width: 1280, Height: 720, Budget: DefaultBudget()})
		if err := r.Load(); err != nil {
			t.Fatalf("cycle=%d load: %v", cycle, err)
		}
		loaded := false
		for frame := 0; frame < 100; frame++ {
			if loaded && frame%10 == 0 {
				r.Input("right")
			}
			r.Step(time.Now())
			timing := r.LastStep()
			if timing.Draw > maxDraw {
				maxDraw = timing.Draw
			}
			if err := r.Err(); err != nil {
				t.Fatalf("cycle=%d frame=%d timing=%+v: %v", cycle, frame, timing, err)
			}
			if homeSettled(r) {
				loaded = true
			}
			time.Sleep(time.Second / 60)
		}
		if !loaded {
			t.Fatalf("cycle=%d never loaded eight-entry fixture", cycle)
		}
		r.Close()
		t.Logf("cycle=%d frames=100 max_draw=%s", cycle, maxDraw)
	}
}
