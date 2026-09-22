package misterruntime_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type liveMediaControl struct {
	packageControl
	replace func(context.Context, string, string, uint64) (misterruntime.Protocol2Response, error)
	clear   func(context.Context, string, uint64) (misterruntime.Protocol2Response, error)
}

func (c *liveMediaControl) ReplaceLiveMedia(ctx context.Context, path, packageID string, generation uint64) (misterruntime.Protocol2Response, error) {
	return c.replace(ctx, path, packageID, generation)
}

func (c *liveMediaControl) ClearLiveMedia(ctx context.Context, packageID string, generation uint64) (misterruntime.Protocol2Response, error) {
	return c.clear(ctx, packageID, generation)
}

func TestRuntimeReplaceLiveMediaStagesAndDispatches(t *testing.T) {
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeTemp, err := filepath.Rel(working, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", relativeTemp)
	response := liveMediaResponse()
	calls := 0
	control := &liveMediaControl{packageControl: packageControl{status2: &response}}
	control.replace = func(_ context.Context, path, packageID string, generation uint64) (misterruntime.Protocol2Response, error) {
		calls++
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !filepath.IsAbs(path) {
			t.Fatalf("unsafe staged media: %v %v", info, err)
		}
		if packageID != strings.Repeat("a", 64) || generation != 9 {
			t.Fatalf("binding=%s/%d", packageID, generation)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "raw" {
			t.Fatalf("bytes=%q err=%v", data, err)
		}
		return response, nil
	}
	control.clear = func(context.Context, string, uint64) (misterruntime.Protocol2Response, error) {
		t.Fatal("clear unexpectedly called")
		return response, nil
	}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	if err := runtime.ReplaceLiveMedia(context.Background(), 3, strings.NewReader("raw"), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRuntimeClearLiveMediaDispatches(t *testing.T) {
	response := liveMediaResponse()
	calls := 0
	control := &liveMediaControl{packageControl: packageControl{status2: &response}}
	control.replace = func(context.Context, string, string, uint64) (misterruntime.Protocol2Response, error) {
		t.Fatal("replace unexpectedly called")
		return response, nil
	}
	control.clear = func(_ context.Context, packageID string, generation uint64) (misterruntime.Protocol2Response, error) {
		calls++
		if packageID != strings.Repeat("a", 64) || generation != 9 {
			t.Fatalf("binding=%s/%d", packageID, generation)
		}
		return response, nil
	}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	if err := runtime.ClearLiveMedia(context.Background(), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRuntimeReplaceLiveMediaRejectsBusyAndStaleGeneration(t *testing.T) {
	response := liveMediaResponse()
	control := &liveMediaControl{packageControl: packageControl{status2: &response}}
	control.replace = func(context.Context, string, string, uint64) (misterruntime.Protocol2Response, error) {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "busy", Message: "tape busy", Phase: "input"}}, nil
	}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	err := runtime.ReplaceLiveMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9})
	if err == nil || err.Code != protocol.CodeBusy || !strings.Contains(err.Message, "tape loader") {
		t.Fatalf("busy mapping: %v", err)
	}
	stale := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 8}
	control.replace = func(context.Context, string, string, uint64) (misterruntime.Protocol2Response, error) {
		t.Fatal("stale generation dispatched")
		return response, nil
	}
	if err := runtime.ReplaceLiveMedia(context.Background(), 1, strings.NewReader("x"), stale); err == nil || err.Phase != "admission" {
		t.Fatalf("stale: %v", err)
	}
}

func liveMediaResponse() misterruntime.Protocol2Response {
	gen := uint64(9)
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "running_development", Execution: "development", Generation: &gen,
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: strings.Repeat("a", 64), Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.simple-computer", Major: 1}}},
		Capabilities:  misterruntime.Protocol2Capabilities{ActiveInterfaces: []misterruntime.Protocol2Interface{{ID: "fes.media.blob", Major: 1}}}}
}
