package misterruntime_test

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

func TestSaveFailureKeepsStopRetryableAndNeverReportsIdle(t *testing.T) {
	failed := snesResponse("running_game")
	failed.Error = &misterruntime.RemoteError{Code: "save_failed", Message: "cannot save"}
	control := &recordingControl{statuses: []misterruntime.Response{failed}, stop: failed}
	runtime := newRuntimeWithNativeCoreFixtures(t, control, "", time.Millisecond, time.Second)
	if !runtime.StopReady() {
		t.Fatal("retained SNES save must permit Stop retry")
	}
	if _, err := runtime.Stop(context.Background()); err == nil || err.Code != protocol.CodeInternal {
		t.Fatalf("save failure = %v", err)
	}
	control.stop = runtimeResponse("idle", "none")
	control.stop.Error = failed.Error
	if _, err := runtime.Stop(context.Background()); err == nil {
		t.Fatal("idle with save error reported success")
	}
	control.stop.Error = nil
	if _, err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSNESPersistentIdentitySurvivesCacheRelocationAndRuntimeRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "saves", "snes")
	firstROM := snesROM(t)
	bytes, _ := os.ReadFile(firstROM)
	relocated := filepath.Join(t.TempDir(), "renamed.smc")
	if err := os.WriteFile(relocated, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	launch := func(id, rom string) string {
		t.Helper()
		control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none")}, launch: snesResponse("running_game")}
		runtime := newRuntimeWithNativeCoreFixtures(t, control, "", time.Millisecond, time.Second, misterruntime.WithSaveRoot(root))
		spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
		prepared, err := runtime.Prepare(spec, rom)
		if err != nil {
			t.Fatal(err)
		}
		prepared.GameID = id
		if _, _, err := runtime.Launch(context.Background(), prepared); err != nil {
			t.Fatal(err)
		}
		_, requests := control.launchCalls()
		path := requests[0].SavePath
		if path == "" || filepath.Ext(path) != ".srm" {
			t.Fatalf("missing persistent path: %q", path)
		}
		rel, err2 := filepath.Rel(root, path)
		if err2 != nil || filepath.Dir(filepath.Dir(rel)) != "." {
			t.Fatalf("save outside game directory: %q", path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("agent created save bytes before successful runtime launch: %v", err)
		}
		return path
	}
	first := launch("super-mario-world", firstROM)
	if got := launch("super-mario-world", relocated); got != first {
		t.Fatalf("cache location changed save: %q != %q", got, first)
	}
	if got := launch("different-game", firstROM); got == first {
		t.Fatal("game identity collision")
	}
	if err := os.WriteFile(relocated, []byte("different cartridge"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := launch("super-mario-world", relocated); got == first {
		t.Fatal("ROM revision collision")
	}
}

func TestSNESUnusableSaveRootFailsBeforeControlMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none")}, launch: snesResponse("running_game")}
	runtime := newRuntimeWithNativeCoreFixtures(t, control, "", time.Millisecond, time.Second, misterruntime.WithSaveRoot(root))
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	_, attempted, err := runtime.Launch(context.Background(), mister.PreparedLaunch{Spec: spec, AbsoluteROM: snesROM(t), GameID: "test"})
	if err == nil || attempted {
		t.Fatalf("unusable save root dispatched: %v %v", attempted, err)
	}
	if n, _ := control.launchCalls(); n != 0 {
		t.Fatal("hardware mutation")
	}
}

func TestSNESFailedSaveBlocksLeaseReleaseUntilStopRetry(t *testing.T) {
	idle := runtimeResponse("idle", "none")
	failed := snesResponse("running_game")
	failed.OK = false
	failed.Error = &misterruntime.RemoteError{Code: "save_failed", Message: "disk full"}
	control := &recordingControl{statuses: []misterruntime.Response{idle}, launch: snesResponse("running_game"), stop: failed}
	runtime := newRuntimeWithNativeCoreFixtures(t, control, "", time.Millisecond, time.Second, misterruntime.WithSaveRoot(t.TempDir()))
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	lease := kitlease.New(time.Minute, func(ctx context.Context) error {
		status, err := coordinator.Stop(ctx)
		if err != nil || status.State != protocol.StateIdle {
			return errors.New("runtime did not become idle")
		}
		return nil
	})
	defer lease.Close()
	waitState := func(want string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if lease.Status().State == want {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("lease: %+v want %s", lease.Status(), want)
	}
	waitState("free")
	grant, err := lease.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "save-test", Purpose: "game save"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-save-test", System: protocol.SystemSNES, ROMPath: snesROM(t)}); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	control.statuses = []misterruntime.Response{failed}
	control.mu.Unlock()
	if status, err := coordinator.Stop(context.Background()); err == nil || status.State == protocol.StateIdle {
		t.Fatalf("failed save claimed idle: %+v %v", status, err)
	}
	if lease.Status().State != "held" {
		t.Fatal("failed Stop released owner")
	}
	control.mu.Lock()
	control.stop = idle
	control.mu.Unlock()
	if status, err := coordinator.Stop(context.Background()); err != nil || status.State != protocol.StateIdle {
		t.Fatalf("same-owner Stop retry: %+v %v", status, err)
	}
	if _, err := lease.Renew(grant.Token); err != nil || lease.Status().State != "held" {
		t.Fatalf("same owner lost its lease after successful retry: %v", err)
	}
	// A second session exercises an explicit premature release separately.
	control.mu.Lock()
	control.statuses = []misterruntime.Response{idle}
	control.mu.Unlock()
	if _, err := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-save-test", System: protocol.SystemSNES, ROMPath: snesROM(t)}); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	control.statuses = []misterruntime.Response{failed}
	control.stop = failed
	control.mu.Unlock()
	if _, err := lease.Release(grant.Token); err != nil {
		t.Fatal(err)
	}
	waitState("blocked")
	if _, err := lease.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("b", 32), Owner: "other", Purpose: "play"}); !errors.Is(err, kitlease.ErrBlocked) {
		t.Fatalf("admitted replacement: %v", err)
	}
	control.mu.Lock()
	control.stop = idle
	control.mu.Unlock()
	request := kitlease.TakeoverRequest{ClaimRequest: kitlease.ClaimRequest{RequestID: strings.Repeat("c", 32), Owner: "operator", Purpose: "retry save"}, ExpectedGeneration: lease.Status().Generation, Reason: "storage repaired"}
	if _, err := lease.Takeover(request); !errors.Is(err, kitlease.ErrBusy) {
		t.Fatalf("retry cleanup: %v", err)
	}
	waitState("free")
	if coordinator.Status().State != protocol.StateIdle {
		t.Fatal("retry did not finish Stop")
	}
}

func TestConfiguredSavesLeaveMegaDriveAndPongRequestsUnchanged(t *testing.T) {
	// An unusable root demonstrates other profiles never touch save storage.
	root := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemPong} {
		t.Run(string(system), func(t *testing.T) {
			spec, _ := core.DefaultRegistry().Lookup(system)
			response := runtimeResponse("running_game", "game")
			name := string(system)
			response.System = &name
			response.Core = &spec.ExpectedCore
			control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none")}, launch: response}
			runtime := newRuntimeWithNativeCoreFixtures(t, control, "", time.Millisecond, time.Second, misterruntime.WithSaveRoot(root))
			rom := ""
			if system == protocol.SystemMegaDrive {
				rom = writeNativeROM(t, ".md")
			}
			if _, _, err := runtime.Launch(context.Background(), mister.PreparedLaunch{Spec: spec, AbsoluteROM: rom, GameID: "test"}); err != nil {
				t.Fatal(err)
			}
			_, requests := control.launchCalls()
			if requests[0].SavePath != "" {
				t.Fatalf("%s save enabled", system)
			}
		})
	}
}

func TestSNESPersistentLostLaunchResponseDoesNotReplay(t *testing.T) {
	control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none"), snesResponse("running_game")}, launchErr: errors.New("response lost")}
	runtime := newRuntimeWithNativeCoreFixtures(t, control, "", time.Millisecond, time.Second, misterruntime.WithSaveRoot(t.TempDir()))
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	if _, attempted, err := runtime.LaunchOwned(context.Background(), context.Background(), context.Background(), mister.PreparedLaunch{Spec: spec, AbsoluteROM: snesROM(t), GameID: "snes-test"}); err != nil || !attempted {
		t.Fatalf("lost response: %v %v", attempted, err)
	}
	n, requests := control.launchCalls()
	if n != 1 || requests[0].SavePath == "" {
		t.Fatalf("persistent launch replayed or path lost: %d %+v", n, requests)
	}
}
