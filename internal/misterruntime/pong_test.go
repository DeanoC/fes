package misterruntime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

func pongResponse(state string) misterruntime.Response {
	response := runtimeResponse(state, "game")
	system, name := "pong", "Pong"
	response.System, response.Core = &system, &name
	return response
}

func TestPongNativeROMlessLaunchAndStop(t *testing.T) {
	spec, ok := core.DefaultRegistry().Lookup("pong")
	if !ok || spec.ExpectedCore != "Pong" {
		t.Fatal("Pong is not registered")
	}
	control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none"), pongResponse("running_game")}, launch: pongResponse("running_game"), stop: runtimeResponse("idle", "none")}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond)
	prepared, err := runtime.Prepare(spec, "")
	if err != nil {
		t.Fatal(err)
	}
	observed, attempted, err := runtime.Launch(context.Background(), prepared)
	if err != nil || !attempted || observed != "Pong" {
		t.Fatalf("launch = %q %v %v", observed, attempted, err)
	}
	n, requests := control.launchCalls()
	if n != 1 || requests[0].System != "pong" || requests[0].RBF != "/usr/share/mister-runtime/cores/pong.rbf" || requests[0].Media == nil || len(requests[0].Media) != 0 || requests[0].Settings == nil || len(requests[0].Settings) != 0 {
		t.Fatalf("requests = %#v", requests)
	}
	if !runtime.StopReady() {
		t.Fatal("Pong cannot stop")
	}
	if _, err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPongRejectsMediaBeforeRuntimeCalls(t *testing.T) {
	spec := core.Spec{System: "pong", ExpectedCore: "Pong"}
	control := &recordingControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond)
	if _, err := runtime.Prepare(spec, "/tmp/rom.bin"); err == nil {
		t.Fatal("Pong accepted a ROM")
	}
	for _, prepared := range []mister.PreparedLaunch{
		{Spec: spec, AbsoluteROM: "/tmp/rom.bin"}, {Spec: spec, RelativeROM: "rom.bin"}, {Spec: spec, MGL: []byte("<mistergamedescription/>")},
	} {
		if _, attempted, err := runtime.Launch(context.Background(), prepared); err == nil || attempted {
			t.Fatalf("invalid media launch = %v %v", attempted, err)
		}
	}
	n, _ := control.launchCalls()
	statuses, _ := control.calls()
	if n != 0 || statuses != 0 {
		t.Fatal("invalid Pong media reached runtime")
	}
}

func TestPongLostLaunchMatchesOnlyRequestedIdentity(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, wrong := range []bool{false, true} {
			terminal := pongResponse("running_game")
			if wrong {
				terminal = runtimeResponse("running_game", "game")
			}
			control := &recordingControl{launchErr: errors.New("lost response"), statuses: []misterruntime.Response{runtimeResponse("idle", "none"), pongResponse("starting"), terminal}}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond)
			prepared := mister.PreparedLaunch{Spec: core.Spec{System: "pong", ExpectedCore: "Pong"}}
			var observed string
			var attempted bool
			var err *protocol.APIError
			if owned {
				observed, attempted, err = runtime.LaunchOwned(context.Background(), context.Background(), context.Background(), prepared)
			} else {
				observed, attempted, err = runtime.Launch(context.Background(), prepared)
			}
			if !attempted || (err != nil) != wrong || (!wrong && observed != "Pong") {
				t.Fatalf("owned=%v wrong=%v: %q %v %v", owned, wrong, observed, attempted, err)
			}
			n, _ := control.launchCalls()
			if n != 1 {
				t.Fatal("launch replayed")
			}
		}
	}
}
