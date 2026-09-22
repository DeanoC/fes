package misterruntime_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
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

func TestRuntimeClearLiveMediaMapsInputIOToBusyAndOtherIOToUnavailable(t *testing.T) {
	response := liveMediaResponse()
	control := &liveMediaControl{packageControl: packageControl{status2: &response}}
	control.replace = func(context.Context, string, string, uint64) (misterruntime.Protocol2Response, error) {
		t.Fatal("replace unexpectedly called")
		return response, nil
	}
	control.clear = func(context.Context, string, uint64) (misterruntime.Protocol2Response, error) {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "io_failed", Message: "FES GP exchange state is ambiguous", Phase: "input"}}, nil
	}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	binding := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}
	err := runtime.ClearLiveMedia(context.Background(), binding)
	if err == nil || err.Code != protocol.CodeBusy || err.Phase != "input" || !strings.Contains(err.Message, "tape loader") {
		t.Fatalf("transport glitch = %#v", err)
	}
	control.clear = func(context.Context, string, uint64) (misterruntime.Protocol2Response, error) {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "io_failed", Message: "FES GP exchange deadline exceeded: opcode=4 index=1 request=0 ack=unobserved", Phase: "input"}}, nil
	}
	err = runtime.ClearLiveMedia(context.Background(), binding)
	if err == nil || err.Code != protocol.CodeBusy || err.Phase != "input" {
		t.Fatalf("deadline glitch = %#v", err)
	}
	control.clear = func(context.Context, string, uint64) (misterruntime.Protocol2Response, error) {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "busy", Message: "tape busy", Phase: "input"}}, nil
	}
	err = runtime.ClearLiveMedia(context.Background(), binding)
	if err == nil || err.Code != protocol.CodeBusy || err.Phase != "input" {
		t.Fatalf("loader busy = %#v", err)
	}
	control.clear = func(context.Context, string, uint64) (misterruntime.Protocol2Response, error) {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "io_failed", Message: "MMIO read failed", Phase: "input"}}, nil
	}
	err = runtime.ClearLiveMedia(context.Background(), binding)
	if err == nil || err.Code != protocol.CodeMiSTerUnavailable || err.Phase != "input" || strings.Contains(err.Message, "tape loader") {
		t.Fatalf("hard mmio = %#v", err)
	}
	control.clear = func(context.Context, string, uint64) (misterruntime.Protocol2Response, error) {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "io_failed", Message: "FES computer media clear failed", Phase: "input"}}, nil
	}
	err = runtime.ClearLiveMedia(context.Background(), binding)
	if err == nil || err.Code != protocol.CodeMiSTerUnavailable || err.Phase != "input" || strings.Contains(err.Message, "tape loader") {
		t.Fatalf("invalid clear ack = %#v", err)
	}
	control.clear = func(context.Context, string, uint64) (misterruntime.Protocol2Response, error) {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "io_failed", Message: "MMIO read failed", Phase: "programming"}}, nil
	}
	err = runtime.ClearLiveMedia(context.Background(), binding)
	if err == nil || err.Code != protocol.CodeMiSTerUnavailable || err.Phase != "programming" {
		t.Fatalf("programming io = %#v", err)
	}
}

type clearKeyboardControl struct {
	*liveMediaControl
	reached chan struct{}
}

func (c *clearKeyboardControl) SetKeyboard(context.Context, uint64) (misterruntime.Protocol2Response, error) {
	select {
	case c.reached <- struct{}{}:
	default:
	}
	return liveMediaResponse(), nil
}

func TestClearLiveMediaSerializesKeyboard(t *testing.T) {
	response := liveMediaResponse()
	entered, release := make(chan struct{}), make(chan struct{})
	control := &clearKeyboardControl{
		liveMediaControl: &liveMediaControl{packageControl: packageControl{status2: &response}},
		reached:          make(chan struct{}, 1),
	}
	control.clear = func(context.Context, string, uint64) (misterruntime.Protocol2Response, error) {
		close(entered)
		<-release
		return response, nil
	}
	control.replace = func(context.Context, string, string, uint64) (misterruntime.Protocol2Response, error) {
		t.Fatal("replace unexpectedly called")
		return response, nil
	}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	keys := input.NewKeyboardSink()
	keys.SetPoster(func(matrix uint64) error { return runtime.SetKeyboard(context.Background(), matrix) })
	done := make(chan *protocol.APIError, 1)
	go func() {
		done <- runtime.ClearLiveMedia(context.Background(), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9})
	}()
	<-entered
	keyDone := make(chan error, 1)
	go func() {
		keyDone <- keys.Apply(protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Code: uint16(zx81keys.Letter('J')), Action: uint8(remoteinput.ActionPress)})
	}()
	select {
	case <-control.reached:
		t.Fatal("keyboard reached the runtime during clear")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-keyDone; err != nil {
		t.Fatal(err)
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
