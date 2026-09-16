package misterruntime_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaControl struct {
	packageControl
	loadMedia func(context.Context, string) (misterruntime.Protocol2Response, error)
}

func (c *mediaControl) LoadMedia(ctx context.Context, path string) (misterruntime.Protocol2Response, error) {
	return c.loadMedia(ctx, path)
}
func mediaResponse() misterruntime.Protocol2Response {
	gen := uint64(9)
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "running_development", Execution: "development", Generation: &gen,
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: strings.Repeat("a", 64), Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.simple-computer", Major: 1}}},
		Capabilities:  misterruntime.Protocol2Capabilities{ActiveInterfaces: []misterruntime.Protocol2Interface{{ID: "fes.media.blob", Major: 1}}}}
}

func TestDevelopmentMediaStagesPrivateBytesAndNeverReplays(t *testing.T) {
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeTemp, err := filepath.Rel(working, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", relativeTemp)
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "lost reply"}[lost], func(t *testing.T) {
			response := mediaResponse()
			calls := 0
			staged := ""
			control := &mediaControl{packageControl: packageControl{status2: &response}}
			payload := bytes.Repeat([]byte{0, 255, 7}, 5461)
			payload = append(payload, 3)
			control.loadMedia = func(_ context.Context, path string) (misterruntime.Protocol2Response, error) {
				calls++
				staged = path
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !filepath.IsAbs(path) {
					t.Fatalf("unsafe staged media: %v %v", info, err)
				}
				dir, err := os.Stat(filepath.Dir(path))
				if err != nil || dir.Mode().Perm() != 0700 {
					t.Fatalf("unsafe private directory: %v %v", dir, err)
				}
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, payload) {
					t.Fatalf("media bytes changed: %v", err)
				}
				if lost {
					return misterruntime.Protocol2Response{}, io.EOF
				}
				return response, nil
			}
			runtime := misterruntime.NewRuntime(control, "", 0, 0)
			err := runtime.LoadDevelopmentMedia(context.Background(), int64(len(payload)), bytes.NewReader(payload), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9})
			if (err != nil) != lost || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if _, err := os.Lstat(staged); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("staging retained: %v", err)
			}
		})
	}
}

func TestDevelopmentMediaRejectsBeforeStagingOrDispatch(t *testing.T) {
	for _, name := range []string{"empty", "oversized", "short", "long", "stale generation", "stale package", "inactive", "no media", "wrong ABI", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			response := mediaResponse()
			binding := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}
			size := int64(1)
			body := "x"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "empty":
				size = 0
				body = ""
			case "oversized":
				size = 16385
			case "short":
				size = 2
			case "long":
				body = "xx"
			case "stale generation":
				binding.Generation = 8
			case "stale package":
				binding.PackageID = strings.Repeat("b", 64)
			case "inactive":
				response.State = "idle"
			case "no media":
				response.Capabilities.ActiveInterfaces = nil
			case "wrong ABI":
				response.ActivePackage.Descriptor.ABI.ID = "fes.simple-game"
			case "cancelled":
				cancel()
			}
			control := &mediaControl{packageControl: packageControl{status2: &response}, loadMedia: func(context.Context, string) (misterruntime.Protocol2Response, error) {
				t.Fatal("rejected media dispatched")
				return response, nil
			}}
			runtime := misterruntime.NewRuntime(control, "", 0, 0)
			if err := runtime.LoadDevelopmentMedia(ctx, size, strings.NewReader(body), binding); err == nil {
				t.Fatal("invalid media accepted")
			}
		})
	}
}

func TestDevelopmentMediaRechecksRuntimeGenerationAfterStaging(t *testing.T) {
	before, after := mediaResponse(), mediaResponse()
	*after.Generation = 10
	control := &mediaControl{packageControl: packageControl{status2Script: []protocol2StatusResult{{response: before}, {response: after}}}, loadMedia: func(context.Context, string) (misterruntime.Protocol2Response, error) {
		t.Fatal("stale media dispatched")
		return after, nil
	}}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	err := runtime.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9})
	if err == nil || err.Phase != "admission" {
		t.Fatalf("generation changed: %v", err)
	}
}
