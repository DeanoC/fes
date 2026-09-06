package misterruntime_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

func snesResponse(state string) misterruntime.Response {
	response := runtimeResponse(state, "game")
	system, name := "snes", "SNES"
	response.System, response.Core = &system, &name
	return response
}
func snesROM(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "game.sfc")
	if err := os.WriteFile(path, []byte("runtime owns cartridge validation and prefix"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSNESNativeCartridgeLaunchKeepsMainIndex(t *testing.T) {
	spec, ok := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	if !ok || spec.ExpectedCore != "SNES" || spec.FileIndex != 0 {
		t.Fatalf("legacy Main SNES selector changed: %+v", spec)
	}
	control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none"), snesResponse("running_game")}, launch: snesResponse("running_game"), stop: runtimeResponse("idle", "none")}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond)
	rom := snesROM(t)
	prepared, err := runtime.Prepare(spec, rom)
	if err != nil {
		t.Fatal(err)
	}
	observed, attempted, err := runtime.Launch(context.Background(), prepared)
	if err != nil || !attempted || observed != "SNES" {
		t.Fatalf("launch: %q %v %v", observed, attempted, err)
	}
	n, requests := control.launchCalls()
	if n != 1 || requests[0].System != "snes" || requests[0].RBF != "/usr/share/mister-runtime/cores/snes.rbf" || len(requests[0].Media) != 1 || requests[0].Media["cartridge"] != rom || requests[0].Settings == nil || len(requests[0].Settings) != 0 {
		t.Fatalf("requests: %+v", requests)
	}
	if !runtime.StopReady() {
		t.Fatal("SNES stop not ready")
	}
	if _, err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestSNESNativeRejectsWrongMediaBeforeDispatch(t *testing.T) {
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	control := &recordingControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond)
	md := writeNativeROM(t, ".md")
	for _, path := range []string{"", "game.sfc", md} {
		if _, err := runtime.Prepare(spec, path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	for _, prepared := range []mister.PreparedLaunch{{Spec: spec, AbsoluteROM: snesROM(t), RelativeROM: "game.sfc"}, {Spec: spec, AbsoluteROM: snesROM(t), MGL: []byte("mgl")}} {
		if _, attempted, err := runtime.Launch(context.Background(), prepared); err == nil || attempted {
			t.Fatal("legacy MGL reached native")
		}
	}
	n, _ := control.launchCalls()
	statuses, _ := control.calls()
	if n != 0 || statuses != 0 {
		t.Fatal("invalid request reached runtime")
	}
}
func TestSNESLostLaunchMatchesRequestedIdentity(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, wrong := range []bool{false, true} {
			terminal := snesResponse("running_game")
			if wrong {
				terminal = runtimeResponse("running_game", "game")
			}
			control := &recordingControl{launchErr: errors.New("lost response"), statuses: []misterruntime.Response{runtimeResponse("idle", "none"), snesResponse("starting"), terminal}}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond)
			prepared := mister.PreparedLaunch{Spec: core.Spec{System: protocol.SystemSNES, ExpectedCore: "SNES"}, AbsoluteROM: snesROM(t)}
			var observed string
			var attempted bool
			var err *protocol.APIError
			if owned {
				observed, attempted, err = runtime.LaunchOwned(context.Background(), context.Background(), context.Background(), prepared)
			} else {
				observed, attempted, err = runtime.Launch(context.Background(), prepared)
			}
			if !attempted || (err != nil) != wrong || (!wrong && observed != "SNES") {
				t.Fatalf("owned%v wrong%v: %q %v %v", owned, wrong, observed, attempted, err)
			}
			if n, _ := control.launchCalls(); n != 1 {
				t.Fatal("replayed launch")
			}
		}
	}
}
