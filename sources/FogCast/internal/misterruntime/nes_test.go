package misterruntime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

func nesROM(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "game.nes")
	if err := os.WriteFile(path, []byte("NES\x1a test cartridge"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func nesResponse(state string) misterruntime.Response {
	response := runtimeResponse(state, "game")
	system, name := "nes", "NES"
	response.System, response.Core = &system, &name
	return response
}

func TestNESNativeCartridgeLaunchUsesIndexZeroAndRuntimeOwnedRBF(t *testing.T) {
	spec, ok := core.DefaultRegistry().Lookup(protocol.SystemNES)
	if !ok || spec.ExpectedCore != "NES" || spec.FileIndex != 0 || len(spec.Extensions) != 1 {
		t.Fatalf("NES registry entry is wrong: %+v", spec)
	}
	rom := nesROM(t)
	prepared, err := newRuntimeWithNativeCoreFixtures(t, &recordingControl{}, "", time.Millisecond, 20*time.Millisecond).Prepare(spec, rom)
	if err != nil {
		t.Fatal(err)
	}
	control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none")}, launch: nesResponse("running_game"), stop: runtimeResponse("idle", "none")}
	runtime := newRuntimeWithNativeCoreFixtures(t, control, "", time.Millisecond, 20*time.Millisecond)
	observed, attempted, err := runtime.Launch(context.Background(), prepared)
	if err != nil || !attempted || observed != "NES" {
		t.Fatalf("launch: %q %v %v", observed, attempted, err)
	}
	n, requests := control.launchCalls()
	if n != 1 || requests[0].System != "nes" || requests[0].RBF != "/usr/share/mister-runtime/cores/nes.rbf" ||
		len(requests[0].Media) != 1 || requests[0].Media["cartridge"] != rom || requests[0].Settings == nil || len(requests[0].Settings) != 0 {
		t.Fatalf("requests: %+v", requests)
	}
}

func TestNESNativeRejectsNonNESMediaBeforeDispatch(t *testing.T) {
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemNES)
	runtime := newRuntimeWithNativeCoreFixtures(t, &recordingControl{}, "", time.Millisecond, 20*time.Millisecond)
	for _, path := range []string{"", writeNativeROM(t, ".sfc"), writeNativeROM(t, ".unf"), "relative.nes"} {
		if _, err := runtime.Prepare(spec, path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: nesROM(t), RelativeROM: "game.nes"}
	if _, attempted, err := runtime.Launch(context.Background(), prepared); err == nil || attempted {
		t.Fatal("invalid NES launch reached native runtime")
	}
}
