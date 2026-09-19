package misterruntime_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

func nativeCoreFixtureRoot(t *testing.T, systems ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "usr/share/mister-runtime/cores")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, system := range systems {
		if err := os.WriteFile(filepath.Join(dir, system+".rbf"), []byte("fixture RBF"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newRuntimeWithNativeCoreFixtures(t *testing.T, control misterruntime.Control, bootID string, poll, health time.Duration, options ...misterruntime.RuntimeOption) *misterruntime.Runtime {
	t.Helper()
	fixture := misterruntime.WithNativeCoreFS(os.DirFS(nativeCoreFixtureRoot(t, "megadrive", "nes", "pong", "snes")))
	return misterruntime.NewRuntime(control, bootID, poll, health, append([]misterruntime.RuntimeOption{fixture}, options...)...)
}

func TestNativeCoreAvailabilityAndPreflight(t *testing.T) {
	root := nativeCoreFixtureRoot(t, "pong")
	control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none")}}
	r := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithNativeCoreFS(os.DirFS(root)))
	got := r.Health("test").NativeCores
	if got == nil || got.Version != 1 || !reflect.DeepEqual(got.Systems, []protocol.System{protocol.SystemPong}) {
		t.Fatalf("availability=%+v", got)
	}
	spec, _ := core.DefaultRegistry().Lookup("pong")
	prepared, err := r.Prepare(spec, "")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := control.calls()
	if err := os.Remove(filepath.Join(root, "usr/share/mister-runtime/cores/pong.rbf")); err != nil {
		t.Fatal(err)
	}
	if _, attempted, err := r.Launch(context.Background(), prepared); err == nil || attempted || err.Code != protocol.CodeUnsupportedOperation || err.Phase != "admission" {
		t.Fatalf("removed core: attempted=%v err=%v", attempted, err)
	}
	if _, err := r.Prepare(spec, ""); err == nil || err.Code != protocol.CodeUnsupportedOperation {
		t.Fatalf("missing core admitted: %v", err)
	}
	after, _ := control.calls()
	launches, _ := control.launchCalls()
	if after != before || launches != 0 {
		t.Fatal("missing core reached runtime control")
	}
	got = r.Health("test").NativeCores
	if got == nil || got.Systems == nil || len(got.Systems) != 0 {
		t.Fatalf("missing explicit empty availability: %+v", got)
	}
}

func TestNativeCoreAvailabilityRejectsNonFilesAndEmptyFiles(t *testing.T) {
	root := nativeCoreFixtureRoot(t)
	dir := filepath.Join(root, "usr/share/mister-runtime/cores")
	if err := os.Mkdir(filepath.Join(dir, "pong.rbf"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nes.rbf"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	r := misterruntime.NewRuntime(&recordingControl{}, "", time.Millisecond, time.Second, misterruntime.WithNativeCoreFS(os.DirFS(root)))
	if got := r.Health("test").NativeCores; got == nil || len(got.Systems) != 0 {
		t.Fatalf("invalid files advertised: %+v", got)
	}
}
